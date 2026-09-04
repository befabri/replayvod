package middleware_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/server/api/middleware"
)

func TestRequestLogsRedactInviteTokens(t *testing.T) {
	const token = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	for _, tc := range []struct {
		name, target, wantPath string
	}{
		{"invite", "/invite/" + token, "/invite/[redacted]"},
		{"encoded route", "/inv%69te/" + token, "/invite/[redacted]"},
		{"trailing path", "/invite/" + token + "/extra", "/invite/[redacted]"},
		{"extra slash", "//invite/" + token, "/invite/[redacted]"},
		{"oauth query", "/api/v1/auth/twitch?invite=" + token, "/api/v1/auth/twitch"},
		{"ordinary route", "/dashboard/schedules", "/dashboard/schedules"},
	} {
		for _, panics := range []bool{false, true} {
			name := tc.name
			if panics {
				name += "/panic"
			}
			t.Run(name, func(t *testing.T) {
				var logs bytes.Buffer
				log := slog.New(slog.NewJSONHandler(&logs, nil))
				req := httptest.NewRequest(http.MethodGet, tc.target, nil)
				originalURL := req.URL.String()
				handler := middleware.Logger(log)(middleware.Recoverer(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.String() != originalURL {
						t.Error("redaction changed the URL received by the handler")
					}
					if panics {
						panic("test panic")
					}
					w.WriteHeader(http.StatusOK)
				})))
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)
				if strings.Contains(logs.String(), token) {
					t.Fatal("raw invite token appeared in a request or panic log")
				}
				wantStatus, wantEntries := http.StatusOK, 1
				if panics {
					wantStatus, wantEntries = http.StatusInternalServerError, 2
				}
				if rr.Code != wantStatus {
					t.Fatalf("response status = %d, want %d", rr.Code, wantStatus)
				}
				decoder := json.NewDecoder(&logs)
				entries := 0
				for {
					var entry map[string]any
					err := decoder.Decode(&entry)
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					entries++
					if entry["path"] != tc.wantPath {
						t.Errorf("logged path = %v, want %s", entry["path"], tc.wantPath)
					}
					if entry["msg"] == "request" && (entry["method"] != http.MethodGet || entry["status"] != float64(wantStatus)) {
						t.Errorf("request metadata was lost: %v", entry)
					}
				}
				if entries != wantEntries {
					t.Errorf("logged %d entries, want %d", entries, wantEntries)
				}
			})
		}
	}
}
