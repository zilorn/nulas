package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSSRFrontend(t *testing.T) {
	var paths []string
	ssr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.Host != "nulas.local" || r.Header.Get("X-Forwarded-Host") != "" {
			t.Error("SSR must preserve original host and discard untrusted forwarded headers")
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<h1>SSR page</h1>"))
	}))
	defer ssr.Close()
	t.Setenv("NULAS_WEB_DIR", "")
	t.Setenv("NULAS_SSR_URL", ssr.URL)
	handler := testApp(t, "").handler()
	for _, path := range []string{"/", "/tasks", "/nodes", "/?view=profiles", "/_build/assets/app.js"} {
		r := httptest.NewRequest("GET", "http://nulas.local"+path, nil)
		r.Header.Set("X-Forwarded-Host", "evil.example")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "SSR page") {
			t.Fatalf("SSR route %s failed: %d", path, w.Code)
		}
		if paths[len(paths)-1] != path {
			t.Fatalf("SSR path/query changed: %s", paths[len(paths)-1])
		}
	}
	for _, path := range []string{"/api/health", "/api/unknown"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if strings.Contains(w.Body.String(), "SSR page") || len(paths) != 5 {
			t.Fatal("API request forwarded to SSR")
		}
	}
	ssr.Close()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/nodes", nil))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "SSR frontend unavailable") {
		t.Fatalf("SSR outage should be visible: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/health", nil))
	if w.Code != 200 {
		t.Fatal("API unavailable during SSR outage")
	}
}

func TestSSRURLValidation(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:3001", "http://[::1]:3001/"} {
		u, _ := url.Parse(raw)
		if !validSSRURL(u) {
			t.Errorf("loopback rejected: %s", raw)
		}
	}
	for _, raw := range []string{"http://example.com", "http://0.0.0.0:3001", "http://localhost:3001", "https://127.0.0.1", "http://user@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?token=x", "http://127.0.0.1#x", ":bad"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("NULAS_WEB_DIR", "")
			t.Setenv("NULAS_SSR_URL", raw)
			w := httptest.NewRecorder()
			frontendHandler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("invalid target accepted: %s", raw)
			}
		})
	}
}
