// Package traefik_path_cache_control sets a Cache-Control header on responses whose path matches a prefix
package traefik_path_cache_control

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Config holds the plugin configuration
type Config struct {
	PathPrefixes []string `json:"pathPrefixes,omitempty"`
	CacheControl string   `json:"cacheControl,omitempty"`
}

// CreateConfig returns the default configuration
func CreateConfig() *Config {
	// No default prefixes, they must be explicit in the middleware config
	return &Config{
		CacheControl: "public, max-age=2592000",
	}
}

// PathCacheControl is the middleware
type PathCacheControl struct {
	next         http.Handler
	pathPrefixes []string
	cacheControl string
}

// New creates the middleware
func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if len(config.PathPrefixes) == 0 {
		return nil, fmt.Errorf("%s: pathPrefixes must not be empty", name)
	}

	if strings.TrimSpace(config.CacheControl) == "" {
		return nil, fmt.Errorf("%s: cacheControl must not be empty", name)
	}

	return &PathCacheControl{
		next:         next,
		pathPrefixes: config.PathPrefixes,
		cacheControl: config.CacheControl,
	}, nil
}

func (s *PathCacheControl) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Other methods and paths go straight through, no wrapping
	if (req.Method != http.MethodGet && req.Method != http.MethodHead) || !s.matches(req.URL.Path) {
		s.next.ServeHTTP(rw, req)
		return
	}

	s.next.ServeHTTP(&responseWriter{ResponseWriter: rw, cacheControl: s.cacheControl}, req)
}

func (s *PathCacheControl) matches(path string) bool {
	for _, prefix := range s.pathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// responseWriter sets the header right before the status is sent, without buffering the body
type responseWriter struct {
	http.ResponseWriter
	cacheControl string
	wroteHeader  bool
}

func (r *responseWriter) WriteHeader(statusCode int) {
	// 1xx can be sent several times before the final status
	if statusCode >= 100 && statusCode < 200 {
		r.ResponseWriter.WriteHeader(statusCode)
		return
	}

	if r.wroteHeader {
		return
	}

	r.wroteHeader = true

	// Only successful responses, never errors or redirects
	switch statusCode {
	case http.StatusOK, http.StatusPartialContent, http.StatusNotModified:
		r.ResponseWriter.Header().Set("Cache-Control", r.cacheControl)
	}

	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseWriter) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}

	return r.ResponseWriter.Write(p)
}

func (r *responseWriter) Flush() {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}

	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("%T is not a http.Hijacker", r.ResponseWriter)
	}

	return hijacker.Hijack()
}

// Unwrap gives http.ResponseController access to the original writer
func (r *responseWriter) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
