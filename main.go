package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"time"

	"boot.dev/linko/internal/build"
	"boot.dev/linko/internal/linkoerr"
	"boot.dev/linko/internal/store"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"gopkg.in/natefinch/lumberjack.v2"
)

type multiError interface {
	error
	Unwrap() []error
}

type stackTracer interface {
	StackTrace() []uintptr
}

func errorAttrs(err error) []slog.Attr {
	attrs := []slog.Attr{slog.String("msg", err.Error())}
	attrs = append(attrs, linkoerr.Attrs(err)...)
	var st stackTracer
	if errors.As(err, &st) {
		attrs = append(attrs, slog.Any("stack_trace", st.StackTrace()))
	}
	return attrs
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	sensitiveKeys := []string{"password", "key", "apikey", "secret", "pin", "creditcardno", "user"}
	if slices.Contains(sensitiveKeys, a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}

	if a.Value.Kind() == slog.KindString {
		u, err := url.Parse(a.Value.String())
		if err == nil && u.User != nil {
			if _, hasPass := u.User.Password(); hasPass {
				newURL := *u
				newURL.User = url.UserPassword(u.User.Username(), "[REDACTED]")
				return slog.String(a.Key, newURL.String())
			}
		}
	}

	if a.Key == "error" {
		if err, ok := a.Value.Any().(error); ok {
			var me multiError
			if errors.As(err, &me) {
				errs := me.Unwrap()
				errGroupArgs := make([]any, len(errs))
				for i, e := range errs {
					attrs := errorAttrs(e)
					args := make([]any, len(attrs))
					for j, attr := range attrs {
						args[j] = attr
					}
					errGroupArgs[i] = slog.Group(fmt.Sprintf("error_%d", i+1), args...)
				}
				return slog.Group("errors", errGroupArgs...)
			}
			attrs := errorAttrs(err)
			args := make([]any, len(attrs))
			for i, attr := range attrs {
				args[i] = attr
			}
			return slog.Group("error", args...)
		}
	}
	return a
}

func main() {
	var handler slog.Handler
	var logWriter io.WriteCloser

	if logFile := os.Getenv("LINKO_LOG_FILE"); logFile != "" {
		lumber := &lumberjack.Logger{
			Filename: logFile, MaxSize: 1, MaxBackups: 10, MaxAge: 28, LocalTime: false, Compress: true,
		}
		logWriter = lumber
		handler = slog.NewJSONHandler(logWriter, &slog.HandlerOptions{ReplaceAttr: replaceAttr})
	} else {
		isTTY := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
		handler = tint.NewHandler(os.Stderr, &tint.Options{NoColor: !isTTY, ReplaceAttr: replaceAttr})
	}

	logger := slog.New(handler)
	if logWriter != nil {
		defer logWriter.Close()
	}

	env := os.Getenv("ENV")
	hostname, _ := os.Hostname()
	logger = logger.With(
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
		slog.String("env", env),
		slog.String("hostname", hostname),
	)

	slog.SetDefault(logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	myStore := store.NewStore("data")
	srv := newServer(myStore, 8899, cancel, logger)

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.shutdown(shutdownCtx)
	}()

	slog.Info("Starting Linko server on :8899")
	if err := srv.start(); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
