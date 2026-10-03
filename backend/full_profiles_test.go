package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fullProfileFixture = `name: Example
mixed-port: 8888
mode: rule
external-controller: 0.0.0.0:9999
secret: imported-controller-secret
proxies:
  - name: ExampleNode
    type: ss
    server: example.invalid
    port: 443
    cipher: aes-128-gcm
    password: private-node-password
proxy-groups:
  - name: Select
    type: select
    proxies: [ExampleNode, DIRECT]
rules:
  - MATCH,Select
dns:
  enable: true
  listen: 0.0.0.0:1053
  nameserver: [https://dns.example.invalid/dns-query]
proxy-providers:
  Remote:
    type: http
    url: https://example.invalid/subscribe?token=private-provider-token
    path: user-config.yaml
    interval: 3600
tun:
  enable: true
  auto-route: true
iptables:
  enable: true
ntp:
  enable: false
  write-to-system: true
listeners:
  - name: user-listener
    type: mixed
    port: 9099
`

func createFullProfile(t *testing.T, a *App, source, content string) Profile {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": "Full", source: content})
	w := request(a, "POST", "/api/profiles", string(body))
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var p Profile
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Full || p.Document != "" || strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "imported-controller-secret") {
		t.Fatal("full profile metadata or credential isolation failed")
	}
	return p
}

func TestFullProfileImportSnapshotAndGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, fullProfileFixture) }))
	defer server.Close()
	for _, source := range []string{"content", "url"} {
		t.Run(source, func(t *testing.T) {
			a := testApp(t, "")
			content := fullProfileFixture
			if source == "url" {
				content = server.URL
			}
			p := createFullProfile(t, a, source, content)
			if a.state.Profiles[0].Document != fullProfileFixture || a.state.Config.Port != 7890 {
				t.Fatal("original document was lost or auto-loaded")
			}
			if w := request(a, "GET", "/api/profiles", ""); strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "document") {
				t.Fatal("profile list leaked full document")
			}
			if w := request(a, "POST", "/api/profiles/"+p.ID+"/load", "{}"); w.Code != 409 {
				t.Fatal("full profile was reduced to core settings")
			}
			body, _ := json.Marshal(map[string]string{"action": "generate", "profileId": p.ID})
			w := request(a, "POST", "/api/jobs", string(body))
			if w.Code != 202 || strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "document") {
				t.Fatal("job submission failed or leaked document")
			}
			// Later profile changes must not affect an already queued job.
			a.state.Profiles[0].Document = "rules: [MATCH,DIRECT]"
			if err := a.persist(); err != nil {
				t.Fatal(err)
			}
			b, err := newApp(a.dir, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if !b.process() || b.state.Jobs[0].Status != "succeeded" {
				t.Fatal("durable full snapshot did not resume")
			}
			output, err := os.ReadFile(filepath.Join(a.dir, b.state.Jobs[0].ID+".yaml"))
			if err != nil || string(output) != fullProfileFixture {
				t.Fatal("generation discarded full configuration", err)
			}
			if w := request(b, "GET", "/api/jobs", ""); strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "document") {
				t.Fatal("job history leaked document")
			}
		})
	}
}

func TestFullProfileApply(t *testing.T) {
	calls := 0
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("controller credentials changed")
		}
		if r.Method == "GET" {
			t.Error("direct application must not require a runtime-state preflight")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		calls++
		if r.Method != "PUT" || r.URL.Path != "/configs" || r.URL.Query().Get("force") != "true" {
			t.Error("full configuration did not use reload API")
		}
		var body struct {
			Payload string `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		fields, err := parseFullDocument(body.Payload)
		if err != nil {
			t.Error(err)
			return
		}
		if len(fields["proxies"].([]any)) != 1 || len(fields["proxy-groups"].([]any)) != 1 || len(fields["rules"].([]any)) != 1 || !strings.Contains(body.Payload, "private-node-password") {
			t.Error("nodes, groups, rules or credentials lost")
		}
		if fields["bind-address"] != "127.0.0.1" || fields["allow-lan"] != false || fields["tun"].(map[string]any)["enable"] != false || fields["iptables"].(map[string]any)["enable"] != false {
			t.Error("applied config can change system routing or bind publicly")
		}
		for _, key := range []string{"secret", "external-controller", "listeners", "tunnels", "redir-port", "tproxy-port"} {
			if _, exists := fields[key]; exists {
				t.Errorf("unsafe applied field: %s", key)
			}
		}
		if _, exists := fields["dns"].(map[string]any)["listen"]; exists {
			t.Error("DNS listener retained")
		}
		if fields["ntp"].(map[string]any)["write-to-system"] != false {
			t.Error("NTP can write system clock")
		}
		provider := fields["proxy-providers"].(map[string]any)["Remote"].(map[string]any)
		if !strings.HasPrefix(provider["path"].(string), ".nulas/proxy-providers/") || provider["url"] != "https://example.invalid/subscribe?token=private-provider-token" {
			t.Error("provider content or cache isolation wrong")
		}
		w.WriteHeader(204)
	}))
	defer core.Close()
	a := testApp(t, core.URL)
	p := createFullProfile(t, a, "content", fullProfileFixture)
	body, _ := json.Marshal(map[string]string{"action": "apply", "profileId": p.ID})
	if w := request(a, "POST", "/api/jobs", string(body)); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	a.process()
	if calls != 1 || a.state.Jobs[0].Status != "succeeded" || a.secret != "test-secret" || a.controller != core.URL {
		t.Fatal("full apply failed or changed controller state")
	}
}

func TestFullProfileFailureAndInterruptedApply(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/configs" {
					t.Error("direct application sent an unexpected request")
				}
				calls++
				w.WriteHeader(status)
			}))
			defer core.Close()
			a := testApp(t, core.URL)
			p := createFullProfile(t, a, "content", fullProfileFixture)
			body, _ := json.Marshal(map[string]string{"action": "apply", "profileId": p.ID})
			if w := request(a, "POST", "/api/jobs", string(body)); w.Code != http.StatusAccepted {
				t.Fatal(w.Body.String())
			}
			a.process()
			if a.state.Jobs[0].Status != "failed" || calls != 1 || !strings.Contains(a.state.Jobs[0].Message, fmt.Sprint(status)) {
				t.Fatal("controller rejection was hidden or retried")
			}
			a.state.Jobs[0].Status = "running"
			if err := a.persist(); err != nil {
				t.Fatal(err)
			}
			b, err := newApp(a.dir, core.URL, "test-secret")
			if err != nil || b.state.Jobs[0].Status != "failed" || b.process() || calls != 1 {
				t.Fatal("interrupted full apply was replayed", err)
			}
		})
	}
}

func TestFullProfileValidation(t *testing.T) {
	for _, content := range []string{
		"proxies: [private-token", "rules: [MATCH,DIRECT]\nrules: []", "rules: [MATCH,DIRECT]\n---\nmode: direct",
		"rules: [MATCH,DIRECT]\nmode: invalid", "rules: [MATCH,DIRECT]\nmixed-port: null", "rules: [MATCH,DIRECT]\nipv6: yes",
		"rules: [MATCH,DIRECT]\nproxies: bad", "rules: [MATCH,DIRECT]\ndns: bad", "rules: [MATCH,DIRECT]\ndns: {enable: true, enable: false}",
	} {
		if _, err := importProfileConfig(content); err == nil {
			t.Errorf("accepted invalid full configuration %q", content)
		}
	}
	if _, err := importProfileConfig("---\nrules: &rules [MATCH,DIRECT]\nextra-rules: *rules\n..."); err != nil {
		t.Fatal("valid YAML anchors or document markers rejected", err)
	}
	a := testApp(t, "")
	if w := request(a, "POST", "/api/jobs", `{"action":"generate","profileId":"missing"}`); w.Code != 404 {
		t.Fatal("unknown profile accepted")
	}
}

func TestFullProfileRealCoreSyntax(t *testing.T) {
	binary, err := filepath.Abs(corePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(binary); os.IsNotExist(err) {
		t.Skip("local Mihomo is not installed")
	}
	fields, err := parseFullDocument(fullProfileFixture)
	if err != nil {
		t.Fatal(err)
	}
	// Check only syntax/semantics in a private directory; no provider fetches,
	// running listeners, real node connections or installed-core changes.
	delete(fields, "proxy-providers")
	fields["dns"] = map[string]any{"enable": false}
	source, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := fullApplyPayload(string(source), "test")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "-t", "-d", dir, "-f", file).CombinedOutput()
	if err != nil {
		t.Fatalf("Mihomo rejected applied payload: %v\n%s", err, output)
	}
}

func TestFullProfileControllerPortCollision(t *testing.T) {
	for _, controller := range []string{"http://127.0.0.1:8888", "http://127.0.0.1:8888/prefix", "https://example.com"} {
		content := "rules: [MATCH,DIRECT]\nmixed-port: 8888"
		if controller == "https://example.com" {
			content = "rules: [MATCH,DIRECT]\nmixed-port: 443"
		}
		if err := validateApplyPorts(content, controller); err == nil {
			t.Fatalf("controller collision accepted: %s", controller)
		}
	}
}

func TestCoreProfileStandardYAML(t *testing.T) {
	c, err := importProfileConfig("---\nmode: 'direct' # comment\nmixed-port: 8888\n...")
	if err != nil || c.Mode != "direct" || c.Port != 8888 || c.Document != "" {
		t.Fatalf("valid core YAML rejected: %+v %v", c, err)
	}
}
