package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestHealthHandler_OK(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rr := httptest.NewRecorder()
	healthHandler(fakePinger{}, log)(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status field = %q, want %q", body["status"], "ok")
	}
}

func TestHealthHandler_UnhealthyDoesNotLeakError(t *testing.T) {
	const secret = "dsn=postgres://user:hunter2@db:5432 connection refused"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rr := httptest.NewRecorder()
	healthHandler(fakePinger{err: errors.New(secret)}, log)(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(rr.Body.String(), "hunter2") || strings.Contains(rr.Body.String(), "connection refused") {
		t.Fatalf("response body leaked the raw DB error: %s", rr.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "unhealthy" {
		t.Fatalf("status field = %q, want %q", body["status"], "unhealthy")
	}
	if _, ok := body["error"]; ok {
		t.Fatalf("response body must not include an error field, got: %v", body)
	}
}
