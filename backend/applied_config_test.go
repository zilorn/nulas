package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppliedSnapshotPersistenceAndFailure(t *testing.T) {
	reject := false
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reject {
			w.WriteHeader(500)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer core.Close()
	a := testApp(t, core.URL)
	content := "mixed-port: 7891\nmode: rule\nproxies:\n  - name: private-node\n    type: socks5\n    server: localhost\n    port: 1080\n    password: private-password\nrules: [MATCH,DIRECT]\n"
	body, _ := json.Marshal(map[string]string{"name": "日常配置", "content": content})
	w := request(a, "POST", "/api/profiles", string(body))
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	p := a.state.Profiles[0]
	body, _ = json.Marshal(map[string]string{"action": "apply", "profileId": p.ID})
	request(a, "POST", "/api/jobs", string(body))
	a.process()
	if a.state.Applied == nil || a.state.Applied.ID != p.ID {
		t.Fatal("successful apply not recorded")
	}
	w = request(a, "GET", "/api/config/applied", "")
	if strings.Contains(w.Body.String(), "private-password") || strings.Contains(w.Body.String(), "document") {
		t.Fatal("private document exposed")
	}
	reject = true
	a.state.Config.Port = 8888
	request(a, "POST", "/api/jobs", `{"action":"apply"}`)
	a.process()
	if a.state.Applied.ID != p.ID {
		t.Fatal("failed apply replaced successful snapshot")
	}
	b, err := newApp(a.dir, core.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if b.state.Applied == nil || b.state.Applied.Config.Port != 7891 || b.state.Applied.Document != content {
		t.Fatal("restart lost applied document")
	}
	if b.process() {
		t.Fatal("completed apply replayed")
	}
	fields, err := b.managedStartupConfig(b.state.Config, "fresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	if fields["mixed-port"] != float64(7891) || fields["secret"] != "fresh-secret" || len(fields["proxies"].([]any)) != 1 {
		t.Fatal("startup did not restore full snapshot")
	}
	reject = false
	request(b, "POST", "/api/jobs", `{"action":"apply"}`)
	b.process()
	fields, err = b.managedStartupConfig(b.state.Config, "next-secret")
	if err != nil {
		t.Fatal(err)
	}
	if fields["mixed-port"] != float64(8888) || len(fields["proxies"].([]any)) != 1 || b.state.Applied.ID != "" {
		t.Fatal("core patch lost full document or mislabelled profile")
	}
}

func TestAppliedSnapshotUpgradeAndInterruptedJob(t *testing.T) {
	a := testApp(t, "")
	a.state.Jobs = []Job{
		{ID: "full", Action: "apply", Status: "succeeded", Config: a.state.Config, Document: "rules: [MATCH,DIRECT]\n"},
		{ID: "patch", Action: "apply", Status: "succeeded", Config: Config{7892, "global", false, true, "debug"}},
		{ID: "interrupted", Action: "apply", Status: "running", Config: Config{7893, "direct", false, false, "info"}},
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.state.Applied.JobID != "patch" || !b.state.Applied.Full || b.state.Applied.Config.Port != 7892 {
		t.Fatal("upgrade lost successful composed configuration")
	}
	if b.state.Jobs[2].Status != "failed" || b.process() {
		t.Fatal("interrupted job replayed")
	}
}

func TestRestoredConfigKeepsSafeStartupSettings(t *testing.T) {
	a := testApp(t, "")
	a.state.Applied = &AppliedConfig{Profile: Profile{Config: Config{7894, "rule", true, false, "info"}, Full: true, Document: `mixed-port: 7894
external-controller: 0.0.0.0:9091
secret: imported-secret
allow-lan: true
tun: {enable: true}
iptables: {enable: true}
dns: {enable: true, listen: "0.0.0.0:53"}
rules: [MATCH,DIRECT]
`}, JobID: "saved"}
	fields, err := a.managedStartupConfig(a.state.Config, "new-secret")
	if err != nil {
		t.Fatal(err)
	}
	if fields["allow-lan"] != false || fields["bind-address"] != "127.0.0.1" || fields["external-controller"] != "127.0.0.1:9090" || fields["secret"] != "new-secret" {
		t.Fatal("unsafe startup listener or controller")
	}
	if fields["tun"].(map[string]any)["enable"] != false || fields["iptables"].(map[string]any)["enable"] != false {
		t.Fatal("privileged operation restored")
	}
	if _, ok := fields["dns"].(map[string]any)["listen"]; ok {
		t.Fatal("DNS listener restored")
	}
}
