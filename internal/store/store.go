package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"boot.dev/linko/internal/linkoerr"
)

type Store struct {
	dir   string
	cache map[string]string
	mu    sync.RWMutex
}

func NewStore(dir string) *Store {
	return &Store{
		dir:   dir,
		cache: make(map[string]string),
	}
}

func (s *Store) Create(ctx context.Context, longURL string) (string, error) {
	b := make([]byte, 3)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	code := hex.EncodeToString(b)
	path := filepath.Join(s.dir, code)
	err = os.WriteFile(path, []byte(longURL), 0644)
	if err != nil {
		return "", err
	}
	return code, nil
}

func (s *Store) List(ctx context.Context) (map[string]string, error) {
	urls := make(map[string]string)
	var errs []error

	err := s.walk(func(entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}

		path := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			err = linkoerr.WithAttrs(err, "path", path)
			errs = append(errs, err)
			return nil
		}
		urls[entry.Name()] = string(data)
		return nil
	})

	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return urls, errors.Join(errs...)
	}

	return urls, nil
}

func (s *Store) Lookup(ctx context.Context, code string) (string, error) {
	s.mu.RLock()
	if val, ok := s.cache[code]; ok {
		s.mu.RUnlock()
		return val, nil
	}
	s.mu.RUnlock()

	path := filepath.Join(s.dir, code)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	result := string(data)

	s.mu.Lock()
	s.cache[code] = result
	s.mu.Unlock()

	return result, nil
}

func (s *Store) walk(fn func(entry fs.DirEntry) error) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := fn(e); err != nil {
			return linkoerr.WithAttrs(err, "path", filepath.Join(s.dir, e.Name()))
		}
	}
	return nil
}
