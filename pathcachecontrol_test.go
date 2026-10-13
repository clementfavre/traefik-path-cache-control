package traefik_path_cache_control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	longCache = "public, max-age=2592000"
	yearCache = "public, max-age=31536000, immutable"
)

type testCase struct {
	name   string
	method string
	path   string
	status int
	want   string
}

// backend answers with the given status and its own Cache-Control, like most apps do
func backend(status int) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Cache-Control", "no-cache")
		rw.WriteHeader(status)
		_, _ = rw.Write([]byte("body"))
	})
}

func runCases(t *testing.T, cfg *Config, tests []testCase) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}

			status := tt.status
			if status == 0 {
				status = http.StatusOK
			}

			handler, err := New(context.Background(), backend(status), cfg, "test")
			if err != nil {
				t.Fatal(err)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(method, tt.path, nil))

			if got := rec.Header().Get("Cache-Control"); got != tt.want {
				t.Errorf("Cache-Control = %q, want %q", got, tt.want)
			}

			if rec.Code != status {
				t.Errorf("status = %d, want %d", rec.Code, status)
			}
		})
	}
}

func TestShorthand(t *testing.T) {
	cfg := CreateConfig()
	cfg.PathPrefixes = []string{"/css/", "/js/", "/media/"}

	runCases(t, cfg, []testCase{
		{name: "css", path: "/css/default/main.css", want: longCache},
		{name: "js", path: "/js/main.js", want: longCache},
		{name: "media", path: "/media/abc/photo.jpg", want: longCache},
		{name: "partial content", path: "/media/abc/video.mp4", status: http.StatusPartialContent, want: longCache},
		{name: "not modified", path: "/css/main.css", status: http.StatusNotModified, want: longCache},
		{name: "head", method: http.MethodHead, path: "/css/main.css", want: longCache},
		{name: "post untouched", method: http.MethodPost, path: "/media/upload", want: "no-cache"},
		{name: "page untouched", path: "/contact", want: "no-cache"},
		{name: "prefix lookalike untouched", path: "/cssfoo/main.css", want: "no-cache"},
		{name: "case sensitive by default", path: "/Media/photo.jpg", want: "no-cache"},
		{name: "404 untouched", path: "/media/missing.jpg", status: http.StatusNotFound, want: "no-cache"},
		{name: "redirect untouched", path: "/media/old.jpg", status: http.StatusFound, want: "no-cache"},
	})
}

func TestRules(t *testing.T) {
	cfg := CreateConfig()
	cfg.CaseInsensitive = true
	cfg.Rules = []Rule{
		{PathPrefixes: []string{"/css/", "/js/"}, CacheControl: yearCache},
		{PathPrefixes: []string{"/media/"}, Extensions: []string{"jpg", ".PNG"}},
		{Extensions: []string{".woff2"}},
	}

	runCases(t, cfg, []testCase{
		{name: "own cacheControl", path: "/css/main.css", want: yearCache},
		{name: "own cacheControl js", path: "/js/main.js", want: yearCache},
		{name: "default cacheControl", path: "/media/photo.jpg", want: longCache},
		{name: "prefix and extension both required", path: "/media/doc.pdf", want: "no-cache"},
		{name: "extension outside prefix", path: "/images/photo.jpg", want: "no-cache"},
		{name: "extension only rule", path: "/fonts/site.woff2", want: longCache},
		{name: "case insensitive prefix", path: "/Media/Photo.JPG", want: longCache},
		{name: "case insensitive extension", path: "/media/logo.png", want: longCache},
		{name: "case insensitive own cacheControl", path: "/CSS/Main.css", want: yearCache},
		{name: "page untouched", path: "/contact", want: "no-cache"},
	})
}

func TestFirstRuleWins(t *testing.T) {
	cfg := CreateConfig()
	cfg.Rules = []Rule{
		{PathPrefixes: []string{"/media/"}, CacheControl: "public, max-age=60"},
		{Extensions: []string{".jpg"}, CacheControl: yearCache},
	}

	runCases(t, cfg, []testCase{
		{name: "both rules match", path: "/media/photo.jpg", want: "public, max-age=60"},
		{name: "second rule only", path: "/images/photo.jpg", want: yearCache},
	})
}

func TestImplicitStatus(t *testing.T) {
	// Backend writes the body without calling WriteHeader
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte("body"))
	})

	cfg := CreateConfig()
	cfg.PathPrefixes = []string{"/css/"}

	handler, err := New(context.Background(), next, cfg, "test")
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
	tests := []struct {
		name   string
		config func(cfg *Config)
	}{
		{name: "nothing to match", config: func(*Config) {}},
		{name: "empty cacheControl", config: func(cfg *Config) {
			cfg.PathPrefixes = []string{"/css/"}
			cfg.CacheControl = " "
		}},
		{name: "rules and shorthand", config: func(cfg *Config) {
			cfg.PathPrefixes = []string{"/css/"}
			cfg.Rules = []Rule{{PathPrefixes: []string{"/js/"}}}
		}},
		{name: "rule without prefixes or extensions", config: func(cfg *Config) {
			cfg.Rules = []Rule{{CacheControl: longCache}}
		}},
		{name: "empty prefix", config: func(cfg *Config) {
			cfg.PathPrefixes = []string{""}
		}},
		{name: "empty extension in a rule", config: func(cfg *Config) {
			cfg.Rules = []Rule{{Extensions: []string{" "}}}
		}},
		{name: "no cacheControl at all", config: func(cfg *Config) {
			cfg.CacheControl = ""
			cfg.Rules = []Rule{{PathPrefixes: []string{"/css/"}}}
		}},
	}

	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CreateConfig()
			tt.config(cfg)

			if _, err := New(context.Background(), next, cfg, "test"); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
