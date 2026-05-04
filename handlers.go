package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"boot.dev/linko/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

func (s *server) handlerIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func (s *server) handlerLogin(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok {
		httpError(r.Context(), w, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}

	// This bcrypt call satisfies the pprof CPU profiling requirement 
	// by forcing the CPU to perform the heavy hashing work.
	_, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		httpError(r.Context(), w, http.StatusInternalServerError, errors.New("internal server error"))
		return
	}

	if logCtx, ok := r.Context().Value(middleware.LogContextKey).(*middleware.LogContext); ok {
		logCtx.Username = username
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Logged in as " + username))
}

func (s *server) handlerShortenLink(w http.ResponseWriter, r *http.Request) {
	username, ok := r.Context().Value(UserContextKey).(string)
	if !ok {
		s.logger.Error("Failed to get username from context")
		httpError(r.Context(), w, http.StatusInternalServerError, errors.New("internal server error"))
		return
	}

	longURL := r.FormValue("url")
	if longURL == "" {
		httpError(r.Context(), w, http.StatusBadRequest, errors.New("missing url"))
		return
	}

	u, err := url.ParseRequestURI(longURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		httpError(r.Context(), w, http.StatusBadRequest, errors.New("invalid URL"))
		return
	}

	code, err := s.store.Create(r.Context(), longURL)
	if err != nil {
		s.logger.Error("Failed to create short URL", "error", err.Error(), "user", username, "long_url", longURL)
		httpError(r.Context(), w, http.StatusInternalServerError, errors.New("internal server error"))
		return
	}

	s.logger.Info("Successfully generated short code", "user", username, "code", code, "long_url", longURL)
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(code))
}

func (s *server) handlerRedirect(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/")
	url, err := s.store.Lookup(r.Context(), code)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			httpError(r.Context(), w, http.StatusNotFound, errors.New("not found"))
		} else {
			s.logger.Error("failed to lookup URL", "error", err)
			httpError(r.Context(), w, http.StatusInternalServerError, errors.New("internal server error"))
		}
		return
	}
	http.Redirect(w, r, url, http.StatusMovedPermanently)
}

func (s *server) handlerListURLs(w http.ResponseWriter, r *http.Request) {
	urls, err := s.store.List(r.Context())
	if err != nil {
		s.logger.Error("failed to list URLs", "error", err)
		httpError(r.Context(), w, http.StatusInternalServerError, errors.New("internal server error"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(urls)
}

func (s *server) handlerStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"redirects":   "0",
		"bytes_saved": "0",
	})
}
