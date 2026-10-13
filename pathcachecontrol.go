// Package traefik_path_cache_control sets a Cache-Control header on responses matching a path prefix or a file extension
package traefik_path_cache_control

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Config holds the plugin configuration
type Config struct {
	// Shorthand for a single rule, used when Rules is empty
	PathPrefixes []string `json:"pathPrefixes,omitempty"`
	Extensions   []string `json:"extensions,omitempty"`

	// Default value, also used by rules without their own cacheControl
	CacheControl string `json:"cacheControl,omitempty"`

	// Match prefixes and extensions regardless of case, e.g. for IIS backends
	CaseInsensitive bool `json:"caseInsensitive,omitempty"`

	// Checked in order, the first matching rule wins
	Rules []Rule `json:"rules,omitempty"`
}

// Rule sets its Cache-Control on paths matching its prefixes and extensions
type Rule struct {
	PathPrefixes []string `json:"pathPrefixes,omitempty"`
	Extensions   []string `json:"extensions,omitempty"`
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
	next            http.Handler
	rules           []Rule
	caseInsensitive bool
}

// New creates the middleware
func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	rules, err := buildRules(config)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return &PathCacheControl{
		next:            next,
		rules:           rules,
		caseInsensitive: config.CaseInsensitive,
	}, nil
}

// buildRules validates the config and normalizes rules once, at startup
func buildRules(config *Config) ([]Rule, error) {
	shorthand := len(config.PathPrefixes) > 0 || len(config.Extensions) > 0
	rules := config.Rules

	switch {
	case len(rules) > 0 && shorthand:
		return nil, errors.New("use either rules or top-level pathPrefixes/extensions, not both")
	case len(rules) == 0 && !shorthand:
		return nil, errors.New("pathPrefixes, extensions or rules is required")
	case len(rules) == 0:
		rules = []Rule{{PathPrefixes: config.PathPrefixes, Extensions: config.Extensions}}
	}

	built := make([]Rule, 0, len(rules))

	for i, rule := range rules {
		label := fmt.Sprintf("rules[%d]: ", i)
		if shorthand {
			label = ""
		}

		if len(rule.PathPrefixes) == 0 && len(rule.Extensions) == 0 {
			return nil, fmt.Errorf("%spathPrefixes or extensions is required", label)
		}

		cacheControl := strings.TrimSpace(rule.CacheControl)
		if cacheControl == "" {
			cacheControl = strings.TrimSpace(config.CacheControl)
		}

		if cacheControl == "" {
			return nil, fmt.Errorf("%scacheControl must not be empty", label)
		}

		prefixes, err := normalize(rule.PathPrefixes, false, config.CaseInsensitive)
		if err != nil {
			return nil, fmt.Errorf("%spathPrefixes: %w", label, err)
		}

		extensions, err := normalize(rule.Extensions, true, config.CaseInsensitive)
		if err != nil {
			return nil, fmt.Errorf("%sextensions: %w", label, err)
		}

		built = append(built, Rule{PathPrefixes: prefixes, Extensions: extensions, CacheControl: cacheControl})
	}

	return built, nil
}

// normalize trims values, adds the leading dot to extensions and lowercases if needed
func normalize(values []string, extension, lower bool) ([]string, error) {
	out := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("empty value")
		}

		if extension && !strings.HasPrefix(value, ".") {
			value = "." + value
		}

		if lower {
			value = strings.ToLower(value)
		}

		out = append(out, value)
	}

	return out, nil
}

func (s *PathCacheControl) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Other methods and paths go straight through, no wrapping
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		s.next.ServeHTTP(rw, req)
		return
	}

	cacheControl, ok := s.match(req.URL.Path)
	if !ok {
		s.next.ServeHTTP(rw, req)
		return
	}

	s.next.ServeHTTP(&responseWriter{ResponseWriter: rw, cacheControl: cacheControl}, req)
}

// match returns the Cache-Control of the first rule matching the path
func (s *PathCacheControl) match(path string) (string, bool) {
	if s.caseInsensitive {
		path = strings.ToLower(path)
	}

	for _, rule := range s.rules {
		// Both conditions apply when both are set, an empty list matches anything
		if hasPrefix(path, rule.PathPrefixes) && hasSuffix(path, rule.Extensions) {
			return rule.CacheControl, true
		}
	}

	return "", false
}

func hasPrefix(path string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

func hasSuffix(path string, suffixes []string) bool {
	if len(suffixes) == 0 {
		return true
	}

	for _, suffix := range suffixes {
		if strings.HasSuffix(path, suffix) {
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
