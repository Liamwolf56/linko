package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"boot.dev/linko/internal/middleware"
)

func Test_requestLogger(t *testing.T) {
	logBuffer := &bytes.Buffer{}

	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				// Freeze the time for deterministic testing
				return slog.Time(slog.TimeKey, time.Date(2023, 10, 1, 12, 34, 57, 0, time.UTC))
			}
			return a
		},
	}))

	// Create a minimal server instance
	s := &server{logger: logger}

	// Create a dummy handler to pass through the middleware
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// Apply the middleware
	loggedHandler := middleware.RequestLogger(s.logger)(dummyHandler)

	req := httptest.NewRequest("GET", "http://lin.ko/api/stats", nil)
	// Set a consistent IP for testing
	req.RemoteAddr = "192.0.2.x:1234"
	
	rr := httptest.NewRecorder()
	loggedHandler.ServeHTTP(rr, req)

	logOutput := logBuffer.String()

	// Check for essential components instead of an exact string match.
	// This prevents the test from failing due to dynamic 'duration' values.
	expectedParts := []string{
		`msg="Served request"`,
		`method=GET`,
		`path=/api/stats`,
		`response_status=200`,
	}

	for _, part := range expectedParts {
		if !strings.Contains(logOutput, part) {
			t.Errorf("log missing expected part %q. Got: %q", part, logOutput)
		}
	}

	// Check the status code
	if rr.Code != http.StatusOK {
		t.Errorf("expected status code: %d, got: %d", http.StatusOK, rr.Code)
	}
}
