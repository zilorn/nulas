package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	a := testApp(t, "")
	a.state.Profiles = make([]Profile, 100)
	if request(a, "POST", "/api/profiles", `{"name":"Extra","content":"mode: rule"}`).Code != 409 {
		t.Fatal("library limit exceeded")
	}
	if request(a, "POST", "/api/profiles", `{"name":"`+strings.Repeat("x", 61)+`","content":"mode: rule"}`).Code != 400 {
		t.Fatal("name limit exceeded")
	}
}

func TestNetworkImport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("controller credentials leaked")
		}
		switch r.URL.Path {
		case "/valid":
			fmt.Fprint(w, "mixed-port: 8888\nmode: direct")
		case "/json":
			fmt.Fprint(w, `{"mode":"global"}`)
		case "/redirect":
			http.Redirect(w, r, "/valid", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/large":
			w.Header().Set("Content-Length", fmt.Sprint((1<<20)+1))
			fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
		case "/chunked":
			w.(http.Flusher).Flush()
			fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
		case "/unsupported":
			fmt.Fprint(w, "proxies: []")
		case "/empty":
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	a := testApp(t, "")
	a.secret = "controller-secret"
	for i, path := range []string{"/valid", "/json", "/redirect"} {
		body, _ := json.Marshal(map[string]string{"name": fmt.Sprintf("Network %d", i), "url": server.URL + path + "?token=private-token"})
		w := request(a, "POST", "/api/profiles", string(body))
		if w.Code != 201 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		var p Profile
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.Source != "network" || a.state.Config.Port != 7890 {
			t.Fatal("incorrect source or current config changed")
		}
	}
	b, err := newApp(a.dir, "", "")
	if err != nil || len(b.state.Profiles) != 3 || b.state.Profiles[0].Config.Port != 8888 {
		t.Fatalf("persistence: %v", err)
	}
	persisted, err := os.ReadFile(filepath.Join(a.dir, "state.json"))
	if err != nil || strings.Contains(string(persisted), "private-token") || strings.Contains(string(persisted), server.URL) {
		t.Fatal("URL persisted", err)
	}
	for _, path := range []string{"/loop", "/large", "/chunked", "/unsupported", "/empty", "/missing"} {
		body, _ := json.Marshal(map[string]string{"name": "Failure", "url": server.URL + path + "?token=private-token"})
		w := request(a, "POST", "/api/profiles", string(body))
		if w.Code != 400 || len(a.state.Profiles) != 3 || strings.Contains(w.Body.String(), "private-token") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{
		`{"name":"A","url":"file:///etc/passwd"}`,
		`{"name":"A","url":"https://user:pass@example.com/config"}`,
		`{"name":"A","url":"https://example.com/config#fragment"}`,
		`{"name":"A","url":"https://"}`,
		`{"name":"A","url":""}`,
		`{"name":"A","url":"https://example.com","content":"mode: rule"}`,
		`{"name":"A","url":"https://example.com","config":{}}`,
	} {
		if w := request(a, "POST", "/api/profiles", body); w.Code != 400 || len(a.state.Profiles) != 3 {
			t.Fatalf("accepted invalid request: %s", body)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetchImportConfig(ctx, server.URL+"/valid?token=private-token"); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("cancellation or error redaction failed")
	}
}

func TestNetworkImportOptionalName(t *testing.T) {
	for _, tc := range []struct {
		label, content, name, want string
		status                     int
	}{
		{"yaml name", "name: '  文件名称  '\nmode: direct", "", "文件名称", 201},
		{"json name", `{"name":"JSON 名称","mode":"global"}`, "", "JSON 名称", 201},
		{"manual override", "name: 文件名称\nmode: rule", " 手动名称 ", "手动名称", 201},
		{"fallback", "mode: rule", "", "网络导入配置", 201},
		{"blank names", "name: '  '\nmode: rule", "  ", "网络导入配置", 201},
		{"invalid name type", `{"name":123,"mode":"rule"}`, "", "", 400},
		{"null name", `{"name":null,"mode":"rule"}`, "", "", 400},
		{"long embedded name", "name: " + strings.Repeat("名", 61) + "\nmode: rule", "", "", 400},
		{"long manual name", "mode: rule", strings.Repeat("名", 61), "", 400},
		{"name without settings", "name: 文件名称", "", "", 400},
		{"duplicate name field", "name: A\nname: B\nmode: rule", "", "", 400},
		{"unsupported settings", "name: 文件名称\nproxies: []", "", "", 400},
	} {
		t.Run(tc.label, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.content)
			}))
			defer server.Close()
			a := testApp(t, "")
			body := map[string]string{"url": server.URL + "/config?token=private"}
			if tc.name != "" {
				body["name"] = tc.name
			}
			encoded, _ := json.Marshal(body)
			w := request(a, "POST", "/api/profiles", string(encoded))
			if w.Code != tc.status {
				t.Fatalf("status: %d %s", w.Code, w.Body.String())
			}
			if tc.status != 201 {
				if len(a.state.Profiles) != 0 {
					t.Fatal("failed import saved a profile")
				}
				return
			}
			var p Profile
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			if p.Name != tc.want || p.Source != "network" {
				t.Fatalf("profile: %+v", p)
			}
			b, err := newApp(a.dir, "", "")
			if err != nil || len(b.state.Profiles) != 1 || b.state.Profiles[0].Name != tc.want {
				t.Fatalf("persistence: %v", err)
			}
			if duplicate := request(a, "POST", "/api/profiles", string(encoded)); duplicate.Code != 409 {
				t.Fatalf("duplicate: %d %s", duplicate.Code, duplicate.Body.String())
			}
		})
	}
}

func TestLargeImports(t *testing.T) {
	// Valid imports larger than the previous file and API limits remain accepted.
	content := "mode: rule\n#" + strings.Repeat("x", 2<<20)
	if _, err := importConfig(content); err != nil {
		t.Fatalf("large import: %v", err)
	}
	// Control bytes in YAML comments expand sixfold when JSON-encoded.
	escaped := "mode: direct\n#" + strings.Repeat("\x01", 2<<20)
	a := testApp(t, "")
	body, _ := json.Marshal(map[string]string{"name": "Large local", "content": escaped})
	if w := request(a, "POST", "/api/profiles", string(body)); w.Code != 201 {
		t.Fatalf("large local import: %d %s", w.Code, w.Body.String())
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chunked" {
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, content)
	}))
	defer server.Close()
	for _, path := range []string{"/regular", "/chunked"} {
		body, _ = json.Marshal(map[string]string{"name": "Large network " + path, "url": server.URL + path})
		if w := request(a, "POST", "/api/profiles", string(body)); w.Code != 201 {
			t.Fatalf("large network import: %d %s", w.Code, w.Body.String())
		}
	}
	if len(a.state.Profiles) != 3 {
		t.Fatal("large imports not saved")
	}
}

func TestSubscriptionImportErrors(t *testing.T) {
	links := "ss://private-credential@example.com:443#node\r\nvless://private-id@example.com:443#node\n"
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		encoded := encoding.EncodeToString([]byte(links))
		// BOM, wrapping and whitespace are common in exported subscriptions.
		content := "\ufeff " + encoded[:12] + "\r\n\t" + encoded[12:] + "\n"
		t.Run(encoding.EncodeToString([]byte{255, 255}), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, content)
			}))
			defer server.Close()
			for _, source := range []string{"url", "content"} {
				a := testApp(t, "")
				value := content
				if source == "url" {
					value = server.URL + "?token=private-token"
				}
				body, _ := json.Marshal(map[string]string{"name": "Subscription", source: value})
				w := request(a, "POST", "/api/profiles", string(body))
				if w.Code != 400 || !strings.Contains(w.Body.String(), "Base64") || !strings.Contains(w.Body.String(), "当前不支持节点订阅") {
					t.Fatalf("%s: %d %s", source, w.Code, w.Body.String())
				}
				if len(a.state.Profiles) != 0 || a.state.Config.Port != 7890 {
					t.Fatal("rejected subscription changed state")
				}
				for _, secret := range []string{"private-credential", "private-id", "private-token", "example.com", encoded} {
					if strings.Contains(w.Body.String(), secret) {
						t.Fatal("subscription details leaked")
					}
				}
			}
		})
	}
	if _, err := importConfig(links); err == nil || !strings.Contains(err.Error(), "节点订阅链接") {
		t.Fatalf("plain subscription: %v", err)
	}
	// Valid scalar configurations with URL-like names must remain importable.
	if _, err := importConfig("name: https://example.com\nmode: rule"); err != nil {
		t.Fatal(err)
	}
	// Arbitrary encoded text must not be mislabeled as a node subscription.
	if err := unsupportedImportFormat(base64.StdEncoding.EncodeToString([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
}

func TestNetworkImportFormatDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"html", "<!DOCTYPE html><html><body>private-token</body></html>", "HTML 网页"},
		{"html without doctype", "\ufeff  <HTML><body>private-token</body></HTML>", "HTML 网页"},
		{"unknown format", "# comment\nprivate-token", "第 2 行"},
		{"binary base64", base64.StdEncoding.EncodeToString([]byte{0xff, 0x00, 0xfa}), "Base64 编码的二进制数据"},
		{"invalid json", "{private-token}", "JSON 配置格式不正确"},
		{"unsupported full config", "proxies: []", "不支持字段"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.content)
			}))
			defer server.Close()
			a := testApp(t, "")
			body, _ := json.Marshal(map[string]string{"url": server.URL + "?token=private-token"})
			w := request(a, "POST", "/api/profiles", string(body))
			if w.Code != 400 || !strings.Contains(w.Body.String(), "下载内容无法作为核心配置导入") || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private-token") || len(a.state.Profiles) != 0 {
				t.Fatal("failed import leaked content or changed state")
			}
		})
	}
}

func TestNetworkImportRequestsClashRepresentation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "clash.meta" {
			t.Errorf("unexpected User-Agent: %q", r.Header.Get("User-Agent"))
			fmt.Fprint(w, base64.StdEncoding.EncodeToString([]byte{0xff, 0x00, 0xfa}))
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/config", http.StatusFound)
			return
		}
		fmt.Fprint(w, "mode: direct\nmixed-port: 8888")
	}))
	defer server.Close()
	for _, path := range []string{"/config", "/redirect"} {
		c, err := fetchImportConfig(context.Background(), server.URL+path)
		if err != nil || c.Mode != "direct" || c.Port != 8888 {
			t.Fatalf("%s: %+v %v", path, c, err)
		}
	}
}
