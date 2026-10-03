package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportCoreConfiguration(t *testing.T) {
	for _, input := range []string{
		"# example\nmixed-port: 8888 # HTTP / SOCKS\nmode: 'direct'\nallow-lan: true\nipv6: false\nlog-level: \"warning\"\n",
		`{"mixed-port":8888,"mode":"direct","allow-lan":true,"ipv6":false,"log-level":"warning"}`,
	} {
		c, err := importConfig(input)
		if err != nil || c.Port != 8888 || c.Mode != "direct" || !c.LAN || c.Log != "warning" {
			t.Fatalf("import: %+v %v", c, err)
		}
	}
	c, err := importConfig("mode: global")
	if err != nil || c.Port != 7890 || c.Log != "info" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	for _, input := range []string{"", "# comment", "{}", `{"mode":"rule","mode":"direct"}`, "mode: invalid", "mixed-port: 0", "mixed-port: null", "ipv6: yes", "mode: rule\nmode: direct", "proxies: []", "secret: password", "mode: rule\n  ipv6: true", `{"mixed-port":"7890"}`, `{"mode":null}`, `{"mode":"rule"} {}`, "mode: 'rule", "mixed-port: 1.5"} {
		if _, err := importConfig(input); err == nil {
			t.Errorf("accepted invalid import %q", input)
		}
	}
}

func TestProfileLibraryPersistenceAndLoad(t *testing.T) {
	a := testApp(t, "")
	// Existing persisted state without a library continues to work.
	if w := request(a, "GET", "/api/profiles", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Body.String())
	}
	body := `{"name":"Office","content":"mixed-port: 8888\nmode: direct"}`
	w := request(a, "POST", "/api/profiles", body)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var p Profile
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if a.state.Config.Port != 7890 || p.Source != "imported" {
		t.Fatal("creating profile modified current config")
	}
	if request(a, "POST", "/api/profiles", body).Code != 409 {
		t.Fatal("duplicate accepted")
	}
	b, err := newApp(a.dir, "", "")
	if err != nil || len(b.state.Profiles) != 1 {
		t.Fatalf("restart: %v", err)
	}
	// A pending job keeps its original configuration after loading a profile.
	request(b, "POST", "/api/jobs", `{"action":"generate"}`)
	if w = request(b, "POST", "/api/profiles/"+p.ID+"/load", "{}"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if b.state.Config.Port != 8888 || b.state.Jobs[0].Config.Port != 7890 {
		t.Fatal("snapshot modified")
	}
	c, err := newApp(a.dir, "", "")
	if err != nil || c.state.Config.Port != 8888 {
		t.Fatalf("load persistence: %v", err)
	}
	if request(c, "POST", "/api/profiles/missing/load", "{}").Code != 404 {
		t.Fatal("missing profile loaded")
	}
}

func TestProfileValidationAndPersistenceFailure(t *testing.T) {
	a := testApp(t, "")
	for _, body := range []string{
		`{"name":"","content":"mode: rule"}`,
		`{"name":"A"}`, `{"name":"A","config":{},"content":"mode: rule"}`,
		`{"name":"A","content":"proxies: []"}`, `{"name":"A","config":{"mixed-port":0}}`,
		`{"name":"A","content":"mode: rule","extra":true}`,
	} {
		if w := request(a, "POST", "/api/profiles", body); w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	body := `{"name":"Created","config":{"mixed-port":7890,"mode":"rule","log-level":"info"}}`
	if w := request(a, "POST", "/api/profiles", body); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	// Force atomic rename to fail and verify memory is rolled back as well.
	if err := os.Remove(filepath.Join(a.dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(a.dir, "state.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if request(a, "POST", "/api/profiles", strings.Replace(body, "Created", "Second", 1)).Code != 500 || len(a.state.Profiles) != 1 {
		t.Fatal("creation failure did not roll back")
	}
	a.state.Config.Port = 9000
	if request(a, "POST", "/api/profiles/"+a.state.Profiles[0].ID+"/load", "{}").Code != 500 || a.state.Config.Port != 9000 {
		t.Fatal("load failure did not roll back")
	}
}

func TestProfileLibraryLimits(t *testing.T) {
	if _, err := importConfig("mode: rule\n" + strings.Repeat("#", 6000)); err == nil {
		t.Fatal("oversized content accepted")
	}
	a := testApp(t, "")
	a.state.Profiles = make([]Profile, 100)
	if request(a, "POST", "/api/profiles", `{"name":"Extra","content":"mode: rule"}`).Code != 409 {
		t.Fatal("library limit exceeded")
	}
	if request(a, "POST", "/api/profiles", `{"name":"`+strings.Repeat("x", 61)+`","content":"mode: rule"}`).Code != 400 {
		t.Fatal("name limit exceeded")
	}
}
