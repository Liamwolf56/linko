package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
	pkgerr "github.com/pkg/errors"
)

// stackTracer is the interface to extract stack traces from pkg/errors
type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

// AsType is a generic helper to check if an error implements an interface
func AsType[T any](err error) (T, bool) {
	var target T
	if errors.As(err, &target) {
		return target, true
	}
	return target, false
}

// replaceAttr groups the error into a structured object with message and stack_trace
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == "error" {
		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}

		// If it's a stackTracer, output structured object with message and stack_trace
		if stackErr, ok := AsType[stackTracer](err); ok {
			return slog.GroupAttrs("error",
				slog.String("message", stackErr.Error()),
				slog.String("stack_trace", fmt.Sprintf("%+v", stackErr.StackTrace())),
			)
		}

		// Fallback for normal errors
		return slog.GroupAttrs("error",
			slog.String("message", err.Error()),
		)
	}
	return a
}

type closeFunc func() error

func initializeLogger() (*slog.Logger, closeFunc) {
	logFile := os.Getenv("LINKO_LOG_FILE")

	// Stderr: DEBUG and above (Human readable)
	stderrHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceAttr,
	})

	if logFile == "" {
		return slog.New(stderrHandler), func() error { return nil }
	}

	file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open log file: %v\n", err)
		os.Exit(1)
	}

	bufferedFile := bufio.NewWriterSize(file, 8192)

	// File: INFO and above (Machine readable JSON)
	fileHandler := slog.NewJSONHandler(bufferedFile, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: replaceAttr,
	})

	// Combine handlers
	logger := slog.New(slog.NewMultiHandler(stderrHandler, fileHandler))

	cleanup := func() error {
		if err := bufferedFile.Flush(); err != nil {
			return err
		}
		return file.Close()
	}

	return logger, cleanup
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	logger, cleanup := initializeLogger()
	defer func() {
		if err := cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to cleanup logger: %v\n", err)
		}
	}()

	st, err := store.New(*dataDir, logger)
	if err != nil {
		logger.Error("failed to create store", "error", err)
		os.Exit(1)
	}

	s := newServer(*st, *httpPort, cancel, logger)

	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown server", "error", err)
		os.Exit(1)
	}
	if serverErr != nil {
		logger.Error("server error", "error", serverErr)
		os.Exit(1)
	}
}
