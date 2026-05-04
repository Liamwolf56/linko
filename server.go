package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"

	"boot.dev/linko/internal/middleware"
	"boot.dev/linko/internal/store"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var httpRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	},
	[]string{"method", "path", "status"},
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		path := r.URL.Path
		method := r.Method
		status := strconv.Itoa(rec.status)

		httpRequestsTotal.
			WithLabelValues(method, path, status).
			Inc()
	})
}

type server struct {
	httpServer *http.Server
	store      *store.Store
	authStore  *store.Store
	cancel     context.CancelFunc
	logger     *slog.Logger
}

func httpError(ctx context.Context, w http.ResponseWriter, status int, err error) {
	message := err.Error()
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusInternalServerError {
		message = http.StatusText(status)
	}
	http.Error(w, message, status)
}

func newServer(store *store.Store, port int, cancel context.CancelFunc, logger *slog.Logger) *server {
	s := &server{
		store:     store,
		authStore: store,
		cancel:    cancel,
		logger:    logger,
	}
	mux := http.NewServeMux()

	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("GET /debug/pprof/", s.authMiddleware(http.HandlerFunc(pprof.Index)))
	mux.Handle("GET /debug/pprof/profile", s.authMiddleware(http.HandlerFunc(pprof.Profile)))

	mux.HandleFunc("GET /", s.handlerIndex)
	mux.Handle("POST /api/login", s.authMiddleware(http.HandlerFunc(s.handlerLogin)))
	mux.Handle("POST /api/shorten", s.authMiddleware(http.HandlerFunc(s.handlerShortenLink)))
	mux.Handle("GET /api/stats", s.authMiddleware(http.HandlerFunc(s.handlerStats)))
	mux.Handle("GET /api/urls", s.authMiddleware(http.HandlerFunc(s.handlerListURLs)))
	mux.HandleFunc("GET /{shortCode}", s.handlerRedirect)
	mux.HandleFunc("POST /admin/shutdown", s.handlerShutdown)

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", port),
		Handler: middleware.RequestIDMiddleware(
			middleware.RequestLogger(logger)(
				metricsMiddleware(mux),
			),
		),
	}
	s.httpServer = srv
	return s
}

func (s *server) start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	s.logger.Debug("Linko is running", "addr", ln.Addr().String())
	if err := s.httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) shutdown(ctx context.Context) error {
	s.logger.Debug("Linko is shutting down")
	return s.httpServer.Shutdown(ctx)
}

func (s *server) handlerShutdown(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ENV") == "production" {
		httpError(r.Context(), w, http.StatusNotFound, errors.New("not found"))
		return
	}
	w.WriteHeader(http.StatusOK)
	go s.cancel()
}
