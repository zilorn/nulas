package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTUNExternalControllerIsUntouched(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	a := testApp(t, server.URL)
	status := request(a, "GET", "/api/runtime/tun", "")
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"supported":false`) {
		t.Fatal(status.Body.String())
	}
	for _, action := range []string{"tun-enable", "tun-disable"} {
		if w := request(a, "POST", "/api/jobs", `{"action":"`+action+`"}`); w.Code != 202 {
			t.Fatal(w.Body.String())
		}
		a.process()
		if a.state.Jobs[len(a.state.Jobs)-1].Status != "failed" {
			t.Fatal("unsupported operation succeeded")
		}
	}
	if calls != 0 {
		t.Fatal("external controller touched")
	}
}

func TestTUNPermissionAndInterruptedJob(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux managed core")
	}
	a := testApp(t, "")
	a.managedContext = context.Background()
	if a.tunPermission().Ready {
		t.Fatal("missing process accepted")
	}
	request(a, "POST", "/api/jobs", `{"action":"tun-enable"}`)
	a.process()
	if a.state.Jobs[0].Status != "failed" {
		t.Fatal("permission failure hidden")
	}
	request(a, "POST", "/api/jobs", `{"action":"tun-enable"}`)
	a.state.Jobs[1].Status = "running"
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.state.Jobs[1].Status != "failed" || b.process() {
		t.Fatal("interrupted TUN side effect replayed")
	}
}

func TestTUNDisableOnlyPatchesTUNAndVerifies(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux managed core")
	}
	patched, read := false, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/configs" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Method == "PATCH" {
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body) != 1 || string(body["tun"]) != `{"enable":false}` {
				t.Errorf("unrelated settings changed: %v", body)
			}
			patched = true
			w.WriteHeader(204)
		} else {
			read = true
			w.Write([]byte(`{"tun":{"enable":false,"device":"Nulas"}}`))
		}
	}))
	defer server.Close()
	a := testApp(t, server.URL)
	a.managedContext = context.Background()
	if err := a.setTUN(false); err != nil {
		t.Fatal(err)
	}
	if !patched || !read {
		t.Fatal("operation not verified")
	}
}

func TestTUNDoesNotTrustControllerEnableFlag(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux managed core")
	}
	for _, body := range []string{`{}`, `{"tun":{"enable":true,"device":"nulas-missing"}}`, `{"tun":{"enable":true,"device":"lo"}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		a := testApp(t, server.URL)
		a.managedContext = context.Background()
		if _, err := a.tunStatus(context.Background()); err == nil {
			t.Errorf("unverified status accepted: %s", body)
		}
		server.Close()
	}
}

func TestEffectiveTUNCapabilities(t *testing.T) {
	for _, status := range []string{"", "CapEff: invalid", "CapEff: 0000000000002000", "CapPrm: 0000000000003000"} {
		if effectiveCapabilities(status)&tunCapabilities == tunCapabilities {
			t.Fatal("insufficient effective privileges accepted")
		}
	}
	if effectiveCapabilities("Name: mihomo\nCapEff:\t0000000000003000\n")&tunCapabilities != tunCapabilities {
		t.Fatal("effective network privileges not recognized")
	}
}

func TestTUNFailedListenerCreationRollsBack(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux managed core")
	}
	patches := []bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			var body struct {
				TUN struct {
					Enable bool `json:"enable"`
				} `json:"tun"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			patches = append(patches, body.TUN.Enable)
			w.WriteHeader(204)
		} else {
			// Mimic Mihomo accepting PATCH but failing to create a listener.
			w.Write([]byte(`{"tun":{"enable":true,"device":"Nulas"}}`))
		}
	}))
	defer server.Close()
	a := testApp(t, server.URL)
	a.managedContext = context.Background()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := a.applyTUN(ctx, true); err == nil {
		t.Fatal("failed listener reported successful")
	}
	if len(patches) != 2 || !patches[0] || patches[1] {
		t.Fatalf("missing rollback: %v", patches)
	}
}

func TestTUNRejectsProfileSnapshot(t *testing.T) {
	a := testApp(t, "")
	for _, action := range []string{"tun-enable", "tun-disable"} {
		w := request(a, "POST", "/api/jobs", `{"action":"`+action+`","profileId":"unused"}`)
		if w.Code != 400 || len(a.state.Jobs) != 0 {
			t.Fatal("TUN operation accepted a profile document")
		}
	}
}
