package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestConfigurationRejection(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"reason", `{"message":"rules[2] [GEOIP,CN,DIRECT] error: database unavailable"}`, ": rules[2] [GEOIP,CN,DIRECT] error: database unavailable"},
		{"empty", `{"message":"  "}`, ""},
		{"missing", `{"error":"private response"}`, ""},
		{"wrong type", `{"message":42}`, ""},
		{"html", `<html>private response</html>`, ""},
		{"trailing", `{"message":"reason"} {}`, ""},
		{"oversized", `{"message":"` + strings.Repeat("x", configurationErrorLimit) + `"}`, ""},
		{"whitespace", `{"message":"rules[2]\n\tmissing group\u001b\u202e"}`, ": rules[2] missing group"},
		{"long reason", `{"message":"` + strings.Repeat("错", 1500) + `"}`, ": " + strings.Repeat("错", 1024) + "…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := configurationRejection(400, strings.NewReader(tc.body), "", "").Error()
			if want := "Mihomo rejected configuration (HTTP 400)" + tc.want; got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestConfigurationDiagnosticCredentials(t *testing.T) {
	document := fullProfileFixture + `
credentials: &credentials
  username: private-user
  password: "private p@ss/&word"
extra: *credentials
uuid: private-uuid
private-key: |
  -----BEGIN PRIVATE KEY-----
  PRIVATEKEYDATA
  -----END PRIVATE KEY-----
users:
  private-tuic-user: private-tuic-password
`
	secrets := []string{"test-secret", "private-node-password", "imported-controller-secret", "private-provider-token", "private-user", "private p@ss/&word", "private-uuid", "PRIVATEKEYDATA", "private-tuic-user", "private-tuic-password"}
	message := "proxy[0] invalid credentials: " + strings.Join(secrets, ", ") + " " + url.QueryEscape(secrets[5]) + " " + url.PathEscape(secrets[5]) + " https://unknown.invalid/?token=another-private-token"
	got := privateConfigurationDiagnostic(message, "test-secret", document)
	for _, secret := range append(secrets, "another-private-token", "unknown.invalid", url.QueryEscape(secrets[5]), url.PathEscape(secrets[5])) {
		if strings.Contains(got, secret) {
			t.Fatalf("credential exposed: %q", secret)
		}
	}
	if !strings.HasPrefix(got, "proxy[0] invalid credentials:") || !strings.Contains(got, "[redacted]") {
		t.Fatal("useful diagnostic was lost")
	}
	if got := privateConfigurationDiagnostic("untrusted detail", "", "invalid: ["); got != "" {
		t.Fatal("unparseable snapshot allowed an unredacted diagnostic")
	}
	if got := privateConfigurationDiagnostic(`invalid password: p'a"ss`, "", `password: "p'a\"ss"`); strings.Contains(got, `p'a"ss`) {
		t.Fatal("JSON-escaped credential exposed")
	}
	if got := privateConfigurationDiagnostic("password: p''ass", "", "password: p'ass"); strings.Contains(got, "p''ass") {
		t.Fatal("YAML-escaped credential exposed")
	}
	// Recursive aliases cannot hang error reporting.
	privateConfigurationDiagnostic("invalid alias", "", "auth: &auth {self: *auth}")
}

func TestFullApplyRejectionDetailIsDurableAndPrivate(t *testing.T) {
	calls := 0
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPut || r.URL.Path != "/configs" {
			t.Error("unexpected controller request")
		}
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "proxy[0] invalid password private-node-password; test-secret; https://example.invalid/subscribe?token=private-provider-token"})
	}))
	defer core.Close()
	a := testApp(t, core.URL)
	p := createFullProfile(t, a, "content", fullProfileFixture)
	if w := request(a, "POST", "/api/jobs", fmt.Sprintf(`{"action":"apply","profileId":%q}`, p.ID)); w.Code != http.StatusAccepted {
		t.Fatal(w.Body.String())
	}
	if !a.process() || a.state.Jobs[0].Status != "failed" || a.state.Applied != nil {
		t.Fatal("rejected apply was not recorded as failed")
	}
	b, err := newApp(a.dir, core.URL, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	response := request(b, "GET", "/api/jobs", "").Body.String()
	if !strings.Contains(response, "proxy[0] invalid password") || !strings.Contains(response, "HTTP 400") {
		t.Fatal("reason was not persisted and exposed")
	}
	for _, secret := range []string{"test-secret", "private-node-password", "private-provider-token", "example.invalid"} {
		if strings.Contains(response, secret) {
			t.Fatal("job history leaked credentials")
		}
	}
	if b.process() || calls != 1 {
		t.Fatal("failed operation was replayed")
	}
}
