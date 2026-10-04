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
		{"localhost:4669", "http://localhost:4669", 201},
		{"127.0.0.1:4669", "http://127.0.0.1:4669", 201},
		{"127.0.0.1:4589", "http://127.0.0.1:4589", 201},
		{"localhost:4669", "http://evil.example", 403},
		{"localhost:4669", "http://localhost:4000", 403},
		{"localhost:4669", "https://localhost:4669", 403},
		{"localhost:4669", "null", 403},
	} {
		t.Run(test.host+test.origin, func(t *testing.T) {
			a := testApp(t, "")
			r := httptest.NewRequest("POST", "http://"+test.host+"/api/profiles", strings.NewReader(`{"name":"same origin","config":{"mixed-port":7890,"mode":"rule","log-level":"info"}}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", test.origin)
			// Forwarded headers must not bypass the browser origin check.
			r.Header.Set("X-Forwarded-Host", "evil.example")
			w := httptest.NewRecorder()
			a.handler(4669, 4589).ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	a := testApp(t, "")
	r := httptest.NewRequest("PUT", "http://localhost:4669/api/runtime/mode", strings.NewReader(`{"mode":"direct"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	a.handler(4669, 4589).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("runtime write bypassed origin guard")
	}
}

func TestNodeDelay(t *testing.T) {
	const name = "节点 / 香港?#%"
	for _, tc := range []struct {
		name, response         string
		coreStatus, wantStatus int
	}{
		{"success", `{"delay":123,"secret":"must-not-leak"}`, 200, 200},
		{"timeout", `{"message":"private-core-detail"}`, 504, 502},
		{"unreachable", `{}`, 503, 502},
		{"invalid JSON", `invalid`, 200, 502},
		{"missing delay", `{}`, 200, 502},
		{"zero delay", `{"delay":0}`, 200, 502},
		{"negative delay", `{"delay":-1}`, 200, 502},
		{"oversized delay", `{"delay":65536}`, 200, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probes := 0
			core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Error("missing server-side credential")
				}
				if r.Method != "GET" {
					t.Error("probe changed core state")
				}
				switch r.URL.Path {
				case "/proxies":
					reply(w, 200, map[string]any{"proxies": map[string]any{name: map[string]string{"type": "Vless"}, "REJECT": map[string]string{"type": "Reject"}, "PASS": map[string]string{"type": "Pass"}, "DROP": map[string]string{"type": "RejectDrop"}}})
				case "/proxies/" + name + "/delay":
					probes++
					if r.URL.Query().Get("url") != "https://www.gstatic.com/generate_204" || r.URL.Query().Get("timeout") != "5000" || r.URL.Query().Get("expected") != "204" {
						t.Errorf("invalid probe query: %s", r.URL.RawQuery)
					}
					w.WriteHeader(tc.coreStatus)
					w.Write([]byte(tc.response))
				default:
					t.Errorf("unexpected request: %s", r.URL)
				}
			}))
			defer core.Close()
			a := testApp(t, core.URL)
			for _, body := range []string{`{}`, `{"name":"missing"}`, `{"name":"REJECT"}`, `{"name":"PASS"}`, `{"name":"DROP"}`, `{"name":"` + strings.Repeat("a", 1025) + `"}`, `{"name":"x","url":"http://private.example"}`} {
				if w := request(a, "POST", "/api/nodes/delay", body); w.Code != 400 {
					t.Fatalf("invalid probe accepted: %d %s", w.Code, w.Body.String())
				}
			}
			if probes != 0 {
				t.Fatal("invalid probe reached delay endpoint")
			}
			body, _ := json.Marshal(map[string]string{"name": name})
			w := request(a, "POST", "/api/nodes/delay", string(body))
			if w.Code != tc.wantStatus || probes != 1 {
				t.Fatalf("status=%d probes=%d body=%s", w.Code, probes, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "must-not-leak") || strings.Contains(w.Body.String(), "private-core-detail") {
				t.Fatal("core details leaked")
			}
			if tc.wantStatus == 200 {
				var result struct {
					Name  string `json:"name"`
					Delay int    `json:"delay"`
				}
				if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Name != name || result.Delay != 123 {
					t.Fatal(w.Body.String())
				}
			}
			if len(a.delaySlots) != 0 {
				t.Fatal("probe slot leaked")
			}
			for i := 0; i < cap(a.delaySlots); i++ {
				a.delaySlots <- struct{}{}
			}
			if w := request(a, "POST", "/api/nodes/delay", string(body)); w.Code != 429 {
				t.Fatal("probe concurrency limit not enforced")
			}
		})
	}
	if w := request(testApp(t, ""), "POST", "/api/nodes/delay", `{"name":"node"}`); w.Code != 502 {
		t.Fatal("missing controller failure hidden")
	}
}

func TestNodeDelayOriginProtection(t *testing.T) {
	a := testApp(t, "")
	r := httptest.NewRequest("POST", "http://localhost:4669/api/nodes/delay", strings.NewReader(`{"name":"node"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	a.handler(4669, 4589).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("delay probe bypassed origin guard")
	}
}
