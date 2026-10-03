package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNodeSelectionAndRuntimeMode(t *testing.T) {
	var mu sync.Mutex
	selected, mode := "节点 A", "rule"
	group := "代理 / 香港?#"
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing server-side authorization")
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/proxies":
			reply(w, 200, map[string]any{"proxies": map[string]any{group: map[string]any{"type": "Selector", "now": selected, "all": []string{"节点 A", "节点 B"}, "secret": "must-not-leak"}, "自动": map[string]any{"type": "URLTest", "all": []string{"节点 A"}}}})
		case r.Method == "PUT" && r.URL.Path == "/proxies/"+group:
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid selection")
			}
			selected = body["name"]
			w.WriteHeader(204)
		case r.Method == "GET" && r.URL.Path == "/configs":
			reply(w, 200, map[string]string{"mode": mode, "secret": "must-not-leak"})
		case r.Method == "PATCH" && r.URL.Path == "/configs":
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 {
				t.Error("mode patch changed other settings")
			}
			mode = body["mode"]
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected core request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer core.Close()
	a := testApp(t, core.URL)
	w := request(a, "GET", "/api/nodes", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "must-not-leak") {
		t.Fatal(w.Body.String())
	}
	for _, body := range []string{`{"group":"missing","name":"节点 B"}`, `{"group":"自动","name":"节点 A"}`, `{"group":"代理 / 香港?#","name":"outside"}`, `{"group":"","name":""}`} {
		if w := request(a, "PUT", "/api/nodes/selection", body); w.Code != 400 {
			t.Fatal("invalid node selection accepted", w.Code)
		}
	}
	w = request(a, "PUT", "/api/nodes/selection", `{"group":"代理 / 香港?#","name":"节点 B"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(a, "GET", "/api/nodes", "")
	if !strings.Contains(w.Body.String(), `"now":"节点 B"`) {
		t.Fatal("selected node not reflected")
	}
	for _, value := range []string{"global", "direct", "rule"} {
		w = request(a, "PUT", "/api/runtime/mode", `{"mode":"`+value+`"}`)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		w = request(a, "GET", "/api/runtime/mode", "")
		if w.Body.String() != `{"mode":"`+value+`"}`+"\n" {
			t.Fatal(w.Body.String())
		}
	}
	if request(a, "PUT", "/api/runtime/mode", `{"mode":"bad"}`).Code != 400 {
		t.Fatal("bad mode accepted")
	}
	if a.state.Config.Mode != "rule" {
		t.Fatal("runtime mode should not overwrite local draft")
	}
}
func TestNodeControllerFailures(t *testing.T) {
	for _, status := range []int{401, 500, 200} {
		core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); w.Write([]byte("invalid JSON")) }))
		a := testApp(t, core.URL)
		for _, path := range []string{"/api/nodes", "/api/runtime/mode"} {
			if request(a, "GET", path, "").Code != 502 {
				t.Fatal("core failure hidden")
			}
		}
		core.Close()
		if request(a, "PUT", "/api/runtime/mode", `{"mode":"direct"}`).Code != 502 {
			t.Fatal("unavailable controller accepted")
		}
	}
	a := testApp(t, "")
	if request(a, "GET", "/api/nodes", "").Code != 502 {
		t.Fatal("missing controller accepted")
	}
}

func TestRuntimeFallback(t *testing.T) {
	for _, tc := range []struct {
		name, body, target string
		configured         bool
		status             int
	}{
		{"configured", `{"rules":[{"type":"Domain","proxy":"Other","payload":"private-domain"},{"type":"Match","proxy":"兜底"}]}`, "兜底", true, 200},
		{"first enabled match", `{"rules":[{"type":"Match","proxy":"Disabled","extra":{"disabled":true}},{"type":"MATCH","proxy":"First"},{"type":"Match","proxy":"Last"}]}`, "First", true, 200},
		{"pass", `{"rules":[{"type":"Match","proxy":"PASS"},{"type":"Match","proxy":"PASS-RULE"},{"type":"Match","proxy":"REJECT"}]}`, "REJECT", true, 200},
		{"no match", `{"rules":[{"type":"Domain","proxy":"Other"}]}`, "DIRECT", false, 200},
		{"empty rules", `{"rules":[]}`, "DIRECT", false, 200},
		{"missing rules", `{}`, "", false, 502},
		{"invalid response", `invalid`, "", false, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/rules" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				w.Write([]byte(tc.body))
			}))
			defer core.Close()
			w := request(testApp(t, core.URL), "GET", "/api/runtime/fallback", "")
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.status != 200 {
				return
			}
			var result map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result) != 2 || result["target"] != tc.target || result["configured"] != tc.configured {
				t.Fatalf("unexpected fallback response: %s", w.Body.String())
			}
		})
	}
}

func TestWriteOriginProtection(t *testing.T) {
	for _, test := range []struct {
		host, origin string
		status       int
	}{
		{"localhost:3000", "http://localhost:3000", 201},
		{"127.0.0.1:3000", "http://127.0.0.1:3000", 201},
		{"127.0.0.1:8080", "http://127.0.0.1:8080", 201},
		{"localhost:3000", "http://evil.example", 403},
		{"localhost:3000", "http://localhost:4000", 403},
		{"localhost:3000", "https://localhost:3000", 403},
		{"localhost:3000", "null", 403},
	} {
		t.Run(test.host+test.origin, func(t *testing.T) {
			a := testApp(t, "")
			r := httptest.NewRequest("POST", "http://"+test.host+"/api/profiles", strings.NewReader(`{"name":"same origin","config":{"mixed-port":7890,"mode":"rule","log-level":"info"}}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", test.origin)
			// Forwarded headers must not bypass the browser origin check.
			r.Header.Set("X-Forwarded-Host", "evil.example")
			w := httptest.NewRecorder()
			a.handler().ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	a := testApp(t, "")
	r := httptest.NewRequest("PUT", "http://localhost/api/runtime/mode", strings.NewReader(`{"mode":"direct"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("runtime write bypassed origin guard")
	}
}
