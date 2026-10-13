package traefik_path_cache_control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const longCache = "public, max-age=2592000"

func newHandler(t *testing.T, status int) http.Handler {
	t.Helper()

	backend := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Cache-Control", "no-cache")
		rw.WriteHeader(status)
		_, _ = rw.Write([]byte("body"))
	})

	cfg := CreateConfig()
	cfg.PathPrefixes = []string{"/css/", "/js/", "/media/"}

	handler, err := New(context.Background(), backend, cfg, "test")
	if err != nil {
		t.Fatal(err)
	}

	return handler
}

func TestCacheControl(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		status int
		want   string
	}{
		{name: "css", path: "/css/default/main.css", status: http.StatusOK, want: longCache},
		{name: "js", path: "/js/main.js", status: http.StatusOK, want: longCache},
		{name: "media", path: "/media/abc/photo.jpg", status: http.StatusOK, want: longCache},
		{name: "not modified", path: "/css/main.css", status: http.StatusNotModified, want: longCache},
		{name: "page untouched", path: "/contact", status: http.StatusOK, want: "no-cache"},
		{name: "prefix lookalike untouched", path: "/cssfoo/main.css", status: http.StatusOK, want: "no-cache"},
		{name: "404 untouched", path: "/media/missing.jpg", status: http.StatusNotFound, want: "no-cache"},
		{name: "redirect untouched", path: "/media/old.jpg", status: http.StatusFound, want: "no-cache"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			newHandler(t, tt.status).ServeHTTP(rec, req)

			if got := rec.Header().Get("Cache-Control"); got != tt.want {
				t.Errorf("Cache-Control = %q, want %q", got, tt.want)
			}

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

func TestImplicitStatus(t *testing.T) {
	// Backend writes the body without calling WriteHeader
	backend := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte("body"))
	})

	cfg := CreateConfig()
	cfg.PathPrefixes = []string{"/css/"}

	handler, err := New(context.Background(), backend, cfg, "test")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/css/main.css", nil))

	if got := rec.Header().Get("Cache-Control"); got != longCache {
		t.Errorf("Cache-Control = %q, want %q", got, longCache)
	}
}

func TestInvalidConfig(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	if _, err := New(context.Background(), next, CreateConfig(), "test"); err == nil {
		t.Error("expected an error without pathPrefixes")
	}

	cfg := CreateConfig()
	cfg.PathPrefixes = []string{"/css/"}
	cfg.CacheControl = " "

	if _, err := New(context.Background(), next, cfg, "test"); err == nil {
		t.Error("expected an error with an empty cacheControl")
	}
}
