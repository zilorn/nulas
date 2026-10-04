package main

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHostAllowlist(t *testing.T) {
	for _, host := range []string{"127.0.0.1:4799", "localhost:4799", "[::1]:4799", "127.0.0.1:4590", "localhost:4590", "[::1]:4590"} {
		t.Run(host, func(t *testing.T) {
			a := testApp(t, "")
			r := httptest.NewRequest("POST", "http://"+host+"/api/jobs", strings.NewReader(`{"action":"generate"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", "http://"+host)
			w := httptest.NewRecorder()
			a.handler(4799, 4590).ServeHTTP(w, r)
			if w.Code != http.StatusAccepted || len(a.state.Jobs) != 1 {
				t.Fatalf("valid configured host rejected: %d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, host := range []string{
		"evil.example:4799", "evil.example:4590", "localhost.evil.example:4799",
		"127.0.0.1.evil.example:4799", "localhost.:4799", "127.0.0.2:4799",
		"[::ffff:127.0.0.1]:4799", "0.0.0.0:4799", "localhost", "[::1]",
		"127.0.0.1:4669", "localhost:4589", "[::1]:4668", "localhost:04799",
		"localhost:80", "localhost:4799@evil.example", "", "localhost:4799,evil.example:4799",
	} {
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			t.Run(method+"/"+host, func(t *testing.T) {
				a := testApp(t, "")
				statePath := filepath.Join(a.dir, "state.json")
				before, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(method, "http://localhost:4799/api/jobs", strings.NewReader(`{"action":"install-core"}`))
				r.Host = host
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", "http://"+host)
				r.Header.Set("Forwarded", "host=localhost:4799;proto=http")
				r.Header.Set("X-Forwarded-Host", "localhost:4799")
				w := httptest.NewRecorder()
				a.handler(4799, 4590).ServeHTTP(w, r)
				if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "untrusted request host") {
					t.Fatalf("untrusted host reached routes: %d %s", w.Code, w.Body.String())
				}
				if len(a.state.Jobs) != 0 || len(a.wake) != 0 {
					t.Fatal("rejected host queued a job")
				}
				after, err := os.ReadFile(statePath)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("rejected host changed persisted state: %v", err)
				}
			})
		}
	}
}

func TestDNSRebindingOverHTTP(t *testing.T) {
	a := testApp(t, "")
	server := httptest.NewUnstartedServer(nil)
	port := server.Listener.Addr().(*net.TCPAddr).Port
	server.Config.Handler = a.handler(port, 4589)
	server.Start()
	defer server.Close()
	host := net.JoinHostPort("evil.example", strconv.Itoa(port))
	for _, origin := range []string{"http://" + host, ""} {
		r, err := http.NewRequest("POST", server.URL+"/api/jobs", strings.NewReader(`{"action":"install-core"}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Host = host
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("DNS rebinding accepted with Origin %q: %d", origin, response.StatusCode)
		}
	}
	if len(a.state.Jobs) != 0 {
		t.Fatal("DNS rebinding persisted a job")
	}
}

func TestDefaultHTTPPortHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "[::1]"} {
		a := testApp(t, "")
		r := httptest.NewRequest("POST", "http://"+host+"/api/jobs", strings.NewReader(`{"action":"generate"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://"+host)
		w := httptest.NewRecorder()
		a.handler(80, 4589).ServeHTTP(w, r)
		if w.Code != http.StatusAccepted {
			t.Fatalf("default HTTP port rejected for %s: %d %s", host, w.Code, w.Body.String())
		}
	}
}

func TestLANHostGuard(t *testing.T) {
	a := testApp(t, "")
	handler := a.handler(4799, 4590, "192.168.1.10")
	for _, tc := range []struct {
		host, origin string
		code         int
	}{
		{"192.168.1.10:4799", "http://192.168.1.10:4799", 202},
		{"192.168.1.10:4799", "http://evil.example:4799", 403},
		{"192.168.1.11:4799", "http://192.168.1.11:4799", 403},
		{"192.168.1.10:4668", "http://192.168.1.10:4668", 403},
		{"evil.example:4799", "http://evil.example:4799", 403},
	} {
		r := httptest.NewRequest("POST", "http://"+tc.host+"/api/jobs", strings.NewReader(`{"action":"generate"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s / %s: %d %s", tc.host, tc.origin, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "http://192.168.1.10:4799/api/health", nil)
	w := httptest.NewRecorder()
	a.handler(4799, 4590).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("LAN accepted by default")
	}
}

func TestLANInterfaceHosts(t *testing.T) {
	var addresses []net.Addr
	for _, cidr := range []string{"127.0.0.1/8", "192.168.1.10/24", "10.0.0.2/8", "172.16.0.2/16", "8.8.8.8/32", "::1/128", "fd00::1/64", "fe80::1/64"} {
		ip, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		network.IP = ip
		addresses = append(addresses, network)
	}
	got := lanInterfaceHosts(addresses)
	if strings.Join(got, ",") != "192.168.1.10,10.0.0.2,172.16.0.2,fd00::1" {
		t.Fatalf("hosts: %v", got)
	}
}
