package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const LogContextKey contextKey = "logContext"

type LogContext struct {
	RequestID string
	Username  string
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := uuid.New().String()
		w.Header().Set("X-Request-ID", requestID)

		logCtx := &LogContext{
			RequestID: requestID,
		}
		ctx := context.WithValue(r.Context(), LogContextKey, logCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			// Retrieve the exact same pointer from the context
			var requestID, username string
			if logCtx, ok := r.Context().Value(LogContextKey).(*LogContext); ok {
				requestID = logCtx.RequestID
				username = logCtx.Username
			}

			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"client_ip", redactIP(r.RemoteAddr),
				"duration", time.Since(start).Nanoseconds(),
				"response_status", sw.status,
				"request_id", requestID,
			}

			if username != "" {
				attrs = append(attrs, "user", username)
			}

			logger.Info("Served request", attrs...)
		})
	}
}

func redactIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return remoteAddr
	}
	ipv4 := ip.To4()
	if ipv4 == nil {
		return remoteAddr
	}
	return fmt.Sprintf("%d.%d.%d.x", ipv4[0], ipv4[1], ipv4[2])
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}
