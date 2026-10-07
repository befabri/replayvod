package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func dashboardTestRouter(t *testing.T, dashboardDir string) http.Handler {
	t.Helper()
	router := chi.NewRouter()
	setupDashboardRoutes(router, dashboardDir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return router
}

func writeDashboardTestFile(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardRejectsPathTraversalAndEscapingSymlinks(t *testing.T) {
	root := t.TempDir()
	dashboardDir := filepath.Join(root, "dashboard")
	writeDashboardTestFile(t, filepath.Join(dashboardDir, "index.html"), "dashboard shell")
	writeDashboardTestFile(t, filepath.Join(root, "private.txt"), "private data outside dashboard")
	writeDashboardTestFile(t, filepath.Join(root, "outside", "private.txt"), "private data outside dashboard")
	for name, target := range map[string]string{
		"absolute-link.txt": filepath.Join(root, "private.txt"),
		"relative-link.txt": "../private.txt",
		"linked-directory":  "../outside",
	} {
		if err := os.Symlink(target, filepath.Join(dashboardDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	router := dashboardTestRouter(t, dashboardDir)
	for _, target := range []string{
		"/../private.txt",
		"/assets/../../private.txt",
		"/%2e%2e/private.txt",
		"/assets/%2E%2E/%2e%2e/private.txt",
		"/%2e%2e%2fprivate.txt",
		"/absolute-link.txt",
		"/relative-link.txt",
		"/linked-directory/private.txt",
	} {
		t.Run(target, func(t *testing.T) {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
			if rr.Code < 400 || rr.Code >= 500 {
				t.Errorf("status = %d, want a client error", rr.Code)
			}
			if strings.Contains(rr.Body.String(), "private data") {
				t.Fatal("response disclosed a file outside the dashboard")
			}
		})
	}
}

func TestDashboardAssetsAndSPAFallback(t *testing.T) {
	dashboardDir := t.TempDir()
	writeDashboardTestFile(t, filepath.Join(dashboardDir, "index.html"), "dashboard shell")
	writeDashboardTestFile(t, filepath.Join(dashboardDir, "assets", "app.js"), "console.log('dashboard')")
	writeDashboardTestFile(t, filepath.Join(dashboardDir, "robots.txt"), "User-agent: *")
	if err := os.Symlink("assets/app.js", filepath.Join(dashboardDir, "linked.js")); err != nil {
		t.Fatal(err)
	}
	router := dashboardTestRouter(t, dashboardDir)
	for _, tc := range []struct {
		path      string
		wantBody  string
		wantCache string
	}{
		{path: "/assets/app.js", wantBody: "console.log('dashboard')", wantCache: "public, max-age=31536000, immutable"},
		{path: "/linked.js", wantBody: "console.log('dashboard')", wantCache: "public, max-age=31536000, immutable"},
		{path: "/robots.txt", wantBody: "User-agent: *"},
		{path: "/", wantBody: "dashboard shell"},
		{path: "/videos/recording-123", wantBody: "dashboard shell"},
		{path: "/videos/recording-123/", wantBody: "dashboard shell"},
		{path: "/assets/", wantBody: "dashboard shell"},
		{path: "/missing.js", wantBody: "dashboard shell"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rr.Code != http.StatusOK || rr.Body.String() != tc.wantBody {
				t.Fatalf("status = %d, body = %q; want 200 and %q", rr.Code, rr.Body.String(), tc.wantBody)
			}
			if cache := rr.Header().Get("Cache-Control"); cache != tc.wantCache {
				t.Errorf("Cache-Control = %q, want %q", cache, tc.wantCache)
			}
		})
	}
	for _, target := range []string{"/api/missing", "/trpc/missing"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", target, rr.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	request.Header.Set("Range", "bytes=0-6")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, request)
	if rr.Code != http.StatusPartialContent || rr.Body.String() != "console" {
		t.Errorf("range request status = %d, body = %q; want 206 and console", rr.Code, rr.Body.String())
	}
}

func TestDashboardIndexCannotEscapeRoot(t *testing.T) {
	for _, replaceAfterSetup := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing symlink", true: "replaced after setup"}[replaceAfterSetup], func(t *testing.T) {
			root := t.TempDir()
			dashboardDir := filepath.Join(root, "dashboard")
			indexPath := filepath.Join(dashboardDir, "index.html")
			writeDashboardTestFile(t, indexPath, "dashboard shell")
			writeDashboardTestFile(t, filepath.Join(root, "private.txt"), "private data outside dashboard")
			var router http.Handler
			if replaceAfterSetup {
				router = dashboardTestRouter(t, dashboardDir)
			}
			if err := os.Remove(indexPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../private.txt", indexPath); err != nil {
				t.Fatal(err)
			}
			if !replaceAfterSetup {
				if dashboardBuildExists(dashboardDir) {
					t.Error("dashboardBuildExists accepted an escaping index.html symlink")
				}
				router = dashboardTestRouter(t, dashboardDir)
			}
			for _, target := range []string{"/", "/videos/recording-123"} {
				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
				if rr.Code < 400 || strings.Contains(rr.Body.String(), "private data") {
					t.Errorf("%s status = %d, body = %q; want rejected index access", target, rr.Code, rr.Body.String())
				}
			}
		})
	}
}
