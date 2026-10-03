package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeSystem(a *App, values map[string]string, enabled *bool, linger bool, failKey string) {
	a.systemCommand = func(_ context.Context, name string, args ...string) (string, error) {
		switch name {
		case "gsettings":
			key := strings.TrimPrefix(args[1], "org.gnome.system.proxy") + "/" + args[2]
			if args[0] == "set" {
				if key == failKey && args[3] == "'manual'" {
					return "", errors.New("write failed")
				}
				values[key] = args[3]
				return "", nil
			}
			return values[key], nil
		case "systemctl":
			switch args[1] {
			case "show":
				if args[3] == "--property=Description" {
					return "Nulas frontend and backend", nil
				}
				return "loaded", nil
			case "is-enabled":
				if *enabled {
					return "enabled", nil
				}
				return "disabled", errors.New("disabled")
			case "enable":
				*enabled = true
			case "disable":
				*enabled = false
			}
			return "", nil
		case "loginctl":
			if linger {
				return "yes", nil
			}
			return "no", nil
		}
		return "", errors.New("unexpected command")
	}
}
func proxyFixture(t *testing.T, failKey string) (*App, map[string]string) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]int{"mixed-port": port}) }))
	t.Cleanup(core.Close)
	a := testApp(t, core.URL)
	values := map[string]string{}
	for _, key := range proxyKeys {
		values[key] = "0"
		if strings.HasSuffix(key, "/host") {
			values[key] = "'old.example'"
		}
	}
	values["/mode"] = "'auto'"
	values[".http/use-authentication"] = "true"
	enabled := false
	fakeSystem(a, values, &enabled, true, failKey)
	return a, values
}
func TestSystemProxyDurableRestore(t *testing.T) {
	a, values := proxyFixture(t, "")
	if w := request(a, "POST", "/api/jobs", `{"action":"proxy-enable"}`); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	a.process()
	if a.state.Jobs[0].Status != "succeeded" || values["/mode"] != "'manual'" || len(a.state.ProxyBackup) == 0 {
		t.Fatal("proxy enable was not verified and persisted")
	}
	restarted, err := newApp(a.dir, a.controller, "")
	if err != nil || len(restarted.state.ProxyBackup) == 0 {
		t.Fatal("recovery data lost", err)
	}
	enabled := false
	fakeSystem(restarted, values, &enabled, true, "")
	if err := restarted.setSystemProxy(false); err != nil {
		t.Fatal(err)
	}
	if values["/mode"] != "'auto'" || values[".http/host"] != "'old.example'" || values[".http/use-authentication"] != "true" || len(restarted.state.ProxyBackup) != 0 {
		t.Fatal("original settings not restored")
	}
	if err := restarted.setSystemProxy(false); err == nil {
		t.Fatal("unowned proxy changed")
	}
}
func TestSystemProxyFailureRollsBack(t *testing.T) {
	a, values := proxyFixture(t, "/mode")
	if err := a.setSystemProxy(true); err == nil {
		t.Fatal("failed write accepted")
	}
	if values["/mode"] != "'auto'" || values[".http/host"] != "'old.example'" || len(a.state.ProxyBackup) != 0 {
		t.Fatal("failed enable not rolled back")
	}
}
func TestStartupRequiresBootLingerAndReadsBack(t *testing.T) {
	a := testApp(t, "")
	enabled := false
	fakeSystem(a, map[string]string{}, &enabled, false, "")
	if w := request(a, "PUT", "/api/runtime/system", `{"startup":true}`); w.Code != 409 || enabled {
		t.Fatal("login-only service claimed boot startup")
	}
	fakeSystem(a, map[string]string{}, &enabled, true, "")
	for _, body := range []string{`{}`, `{"silent":true}`, `{"startup":"yes"}`} {
		if request(a, "PUT", "/api/runtime/system", body).Code != 400 {
			t.Fatal("invalid settings accepted")
		}
	}
	if w := request(a, "PUT", "/api/runtime/system", `{"startup":true}`); w.Code != 200 || !enabled {
		t.Fatal(w.Body.String())
	}
	if w := request(a, "PUT", "/api/runtime/system", `{"startup":false}`); w.Code != 200 || enabled {
		t.Fatal(w.Body.String())
	}
}
func TestSystemProxyRejectsRemoteController(t *testing.T) {
	a, values := proxyFixture(t, "")
	a.controller = "http://192.0.2.1:9090"
	if err := a.setSystemProxy(true); err == nil || values["/mode"] != "'auto'" {
		t.Fatal("remote controller altered local proxy")
	}
}

func TestProxyStatusDetectsExternalChanges(t *testing.T) {
	a, values := proxyFixture(t, "")
	if err := a.setSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	if !a.proxyStatus(context.Background()).Enabled {
		t.Fatal("verified proxy not enabled")
	}
	values[".socks/port"] = "12345"
	status := a.proxyStatus(context.Background())
	if status.Enabled || !status.Recovery {
		t.Fatal("external change was hidden")
	}
}
