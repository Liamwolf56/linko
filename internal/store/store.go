package store

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
)

type Store struct {
	dir    string
	logger *slog.Logger
}

func New(dir string, logger *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	return &Store{
		dir:    dir,
		logger: logger,
	}, nil
}

// Lookup matches the expected signature in handlers.go
func (s *Store) Lookup(ctx context.Context, short string) (string, error) {
	path := filepath.Join(s.dir, short)
	data, err := os.ReadFile(path)
	if err != nil {
		// Wrapped error: the handler will log this once
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

// Create matches the expected signature in handlers.go
func (s *Store) Create(ctx context.Context, url string) (string, error) {
	short := fmt.Sprintf("%d", rand.Intn(100000))
	path := filepath.Join(s.dir, short)
	
	if err := os.WriteFile(path, []byte(url), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return short, nil
}

// List matches the expected signature in handlers.go
func (s *Store) List(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", s.dir, err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}
	return files, nil
}
