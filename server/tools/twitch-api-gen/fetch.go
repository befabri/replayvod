package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FetchOptions configures the reference HTML fetch.
type FetchOptions struct {
	URL       string
	CachePath string
	MaxAge    time.Duration
	Refresh   bool
	Client    *http.Client
	Log       *slog.Logger
}

// Fetch returns the Twitch reference HTML parsed into a goquery document.
// If a fresh cache exists (younger than MaxAge and Refresh is false), it is used;
// otherwise the HTML is re-fetched and the cache is written.
func Fetch(ctx context.Context, opts FetchOptions) (*goquery.Document, error) {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.MaxAge == 0 {
		opts.MaxAge = 24 * time.Hour
	}

	if !opts.Refresh && cacheFresh(opts.CachePath, opts.MaxAge) {
		opts.Log.Info("reusing cache", "path", opts.CachePath)
		return readCache(opts.CachePath)
	}

	opts.Log.Info("fetching reference html", "url", opts.URL)
	body, err := download(ctx, opts.URL, opts.Client)
	if err != nil {
		return nil, err
	}
	if err := writeCache(opts.CachePath, body); err != nil {
		return nil, fmt.Errorf("write cache: %w", err)
	}
	opts.Log.Info("cached", "path", opts.CachePath, "bytes", len(body))

	return goquery.NewDocumentFromReader(bytes.NewReader(body))
}

func cacheFresh(path string, maxAge time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < maxAge
}

func readCache(path string) (*goquery.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cache: %w", err)
	}
	defer f.Close()
	return goquery.NewDocumentFromReader(f)
}

func writeCache(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func download(ctx context.Context, url string, client *http.Client) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", "replayvod-twitch-api-gen/0.1 (+https://github.com/befabri/replayvod)")

	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s for %s", resp.Status, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}
