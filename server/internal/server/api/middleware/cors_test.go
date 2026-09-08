package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/trpc"
)

const corsTrustedOrigin = "https://dashboard.example"

var (
	testMethods = []string{http.MethodGet, http.MethodHead, http.MethodPost}
	testHeaders = trpc.RequestHeaders()
)

func trustedCORS() func(http.Handler) http.Handler {
	return CORS([]string{corsTrustedOrigin}, testMethods, testHeaders)
}

func TestCORSEmptyAllowedOriginsDoesNotAllowAll(t *testing.T) {
	handler := CORS(nil, testMethods, testHeaders)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for no trusted origins", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want empty for no trusted origins", got)
	}
}

func TestCORSEmptyAllowedOriginsDoesNotAnswerPreflight(t *testing.T) {
	handler := CORS(nil, testMethods, testHeaders)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want downstream status %d", rr.Code, http.StatusTeapot)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for no trusted origins", got)
	}
}

func TestCORSAllowsOnlyConfiguredOrigins(t *testing.T) {
	handler := trustedCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		name       string
		origin     string
		wantOrigin string
	}{
		{name: "trusted", origin: corsTrustedOrigin, wantOrigin: corsTrustedOrigin},
		{name: "untrusted", origin: "https://evil.example", wantOrigin: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", tc.origin)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if got := rr.Header().Get("Access-Control-Allow-Origin"); got != tc.wantOrigin {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, tc.wantOrigin)
			}
		})
	}
}

func assertCORSAllowance(t *testing.T, rr *httptest.ResponseRecorder, wantOrigin string) {
	t.Helper()
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != wantOrigin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, wantOrigin)
	}
	wantCredentials := ""
	if wantOrigin != "" {
		wantCredentials = "true"
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != wantCredentials {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want %q", got, wantCredentials)
	}
}

func containsFold(values []string, want string) bool {
	return slices.ContainsFunc(values, func(v string) bool { return strings.EqualFold(strings.TrimSpace(v), want) })
}

func TestCORSTrustedOriginReadsEveryBrowserMethod(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusGone, http.StatusServiceUnavailable} {
		handler := trustedCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		for _, method := range testMethods {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				req := httptest.NewRequest(method, "/api/v1/videos/7/parts/1/stream", nil)
				req.Header.Set("Origin", corsTrustedOrigin)
				rr := httptest.NewRecorder()

				handler.ServeHTTP(rr, req)

				if rr.Code != status {
					t.Fatalf("status = %d, want the handler's %d", rr.Code, status)
				}
				assertCORSAllowance(t, rr, corsTrustedOrigin)
				if !slices.Contains(rr.Header().Values("Vary"), "Origin") {
					t.Fatalf("Vary = %v, want Origin", rr.Header().Values("Vary"))
				}
			})
		}
	}
}

func TestCORSHeadProbeFollowsOriginPolicy(t *testing.T) {
	handler := trustedCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "video file missing", http.StatusNotFound)
	}))

	for _, tc := range []struct {
		name       string
		origin     string
		wantOrigin string
	}{
		{name: "trusted origin", origin: corsTrustedOrigin, wantOrigin: corsTrustedOrigin},
		{name: "untrusted origin", origin: "https://evil.example", wantOrigin: ""},
		{name: "same origin", origin: "", wantOrigin: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodHead, "/api/v1/videos/7/parts/1/stream", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 regardless of origin", rr.Code)
			}
			assertCORSAllowance(t, rr, tc.wantOrigin)
		})
	}
}

func preflight(t *testing.T, handler http.Handler, method, requestHeaders string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodOptions, "/trpc/system.events", nil)
	req.Header.Set("Origin", corsTrustedOrigin)
	req.Header.Set("Access-Control-Request-Method", method)
	if requestHeaders != "" {
		req.Header.Set("Access-Control-Request-Headers", requestHeaders)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code == http.StatusTeapot {
		t.Fatalf("preflight %s %q reached the handler, want the middleware to answer it", method, requestHeaders)
	}
	return rr
}

func TestCORSPreflightAdvertisesEveryBrowserMethod(t *testing.T) {
	handler := trustedCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	for _, method := range testMethods {
		t.Run(method, func(t *testing.T) {
			rr := preflight(t, handler, method, "")
			assertCORSAllowance(t, rr, corsTrustedOrigin)
			if got := rr.Header().Values("Access-Control-Allow-Methods"); !containsFold(strings.Split(strings.Join(got, ","), ","), method) {
				t.Fatalf("Access-Control-Allow-Methods = %q, want it to include %s", got, method)
			}
		})
	}
	t.Run("unlisted method", func(t *testing.T) {
		rr := preflight(t, handler, http.MethodPatch, "")
		assertCORSAllowance(t, rr, "")
	})
}

func TestCORSPreflightAllowsEveryBrowserRequestHeader(t *testing.T) {
	handler := trustedCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	for _, header := range testHeaders {
		t.Run(header, func(t *testing.T) {
			rr := preflight(t, handler, http.MethodGet, strings.ToLower(header))
			assertCORSAllowance(t, rr, corsTrustedOrigin)
			if got := rr.Header().Get("Access-Control-Allow-Headers"); !containsFold(strings.Split(got, ","), header) {
				t.Fatalf("Access-Control-Allow-Headers = %q, want it to include %s", got, header)
			}
		})
	}
	t.Run("unlisted header", func(t *testing.T) {
		rr := preflight(t, handler, http.MethodGet, "x-unlisted")
		assertCORSAllowance(t, rr, "")
	})
}
