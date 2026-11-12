package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"safetyagent/internal/agent"
)

// Detect missing packaged assets and a broken repository-independent sample path.
func TestApplicationServesPackagedPageAndSample(t *testing.T) {
	h := application(agent.Config{})
	for _, path := range []string{"/", "/app.js", "/style.css", "/api/sample"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 {
			t.Fatalf("%s: %d", path, r.Code)
		}
		if path == "/" && !strings.Contains(r.Body.String(), "text/html") && !strings.Contains(r.Body.String(), "<!doctype html>") {
			t.Fatal("no page")
		}
		if path == "/api/sample" {
			var p agent.Project
			if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			if err := agent.Validate(p); err != nil {
				t.Fatal(err)
			}
			if len(p.Clips) < 6 || len(p.Tracks) < 3 {
				t.Fatal("incomplete sample")
			}
		}
	}
}

// Keep the shipped page pinned to offline: configuring a model must not silently
// move the default parser away from the no-key acceptance path.
func TestPackagedPageDefaultsToOffline(t *testing.T) {
	h := application(agent.Config{})
	fetch := func(path string) string {
		t.Helper()
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 {
			t.Fatalf("%s: %d", path, r.Code)
		}
		return r.Body.String()
	}
	page := fetch("/")
	if !strings.Contains(page, `<option value="offline">离线句式</option>`) ||
		!strings.Contains(page, `<option value="model" disabled>`) {
		t.Fatal("page does not default to offline with model disabled")
	}
	if !strings.Contains(page, `type="module" src="/app.js"`) {
		t.Fatal("page does not load native modules")
	}
}

func TestExtensionPackagedMediaCover(t *testing.T) {
	h := application(agent.Config{})
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/demo-cover.svg", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), "<svg") {
		t.Fatalf("demo media unavailable: %d", r.Code)
	}
	if !strings.Contains(r.Header().Get("Content-Security-Policy"), "media-src 'self' blob:") {
		t.Fatal("local video cannot load under CSP")
	}
}
