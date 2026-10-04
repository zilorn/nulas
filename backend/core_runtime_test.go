package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagedCoreLifecycle(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.managedContext = ctx
	a.state.Config.Port = 0
	a.state.Config.Log = "silent"
	if err := os.MkdirAll(filepath.Dir(corePath()), 0700); err != nil {
		t.Fatal(err)
	}
	// Fake executable controller verifies the secret and process lifetime without a real core.
	source := `#!/usr/bin/env python3
import json,sys,http.server
config=json.load(open(sys.argv[sys.argv.index('-f')+1]))
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  if self.headers.get('Authorization') != 'Bearer '+config['secret']:
   self.send_response(401);self.end_headers();return
  self.send_response(200);self.end_headers();self.wfile.write(b'{"version":"test"}')
 def do_PATCH(self):
  if self.headers.get('Authorization') != 'Bearer '+config['secret']:
   self.send_response(401);self.end_headers();return
  body=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  self.send_response(204 if body == {'log-level':'silent'} else 400);self.end_headers()
 def log_message(self,*args): pass
assert config['external-controller'] == '127.0.0.1:0'
assert config['log-level'] == 'info'
host,port=config['external-controller'].rsplit(':',1)
server=http.server.HTTPServer((host,int(port)),Handler)
print('x'*9000,flush=True)
print('RESTful API listening at: '+host+':'+str(server.server_port),flush=True)
server.serve_forever()
`
	if err := os.WriteFile(corePath(), []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	if w := request(a, "POST", "/api/core/ensure", "{}"); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if !a.process() {
		t.Fatal("missing startup job")
	}
	a.mu.Lock()
	status := a.coreStatus()
	controller, secret, done := a.controller, a.secret, a.coreDone
	a.mu.Unlock()
	if status.Status != "running" || controller == "" || secret == "" {
		t.Fatalf("not ready: %+v", status)
	}
	var version struct {
		Version string `json:"version"`
	}
	if err := a.controllerRequest(ctx, "GET", "/version", nil, &version); err != nil || version.Version != "test" {
		t.Fatalf("discovered controller unusable: %v", err)
	}
	if w := request(a, "POST", "/api/core/ensure", "{}"); w.Code != 200 {
		t.Fatal("duplicate startup")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("child survived shutdown")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.controller != "" || a.coreStatus().Status != "failed" {
		t.Fatal("exit hidden")
	}
	files, err := filepath.Glob(filepath.Join(a.dir, "managed-core", "nulas-*.json"))
	if err != nil || len(files) != 0 {
		t.Fatal("private launch config retained")
	}
}

func TestManagedCoreProxyPortConflict(t *testing.T) {
	isolatedCore(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	a := testApp(t, "")
	a.managedContext = context.Background()
	a.state.Config.Port = listener.Addr().(*net.TCPAddr).Port
	if err := a.startCore(a.state.Config); err == nil {
		t.Fatal("occupied port accepted")
	}
	if a.coreStatus().Status != "failed" || a.controller != "" {
		t.Fatal("failure hidden")
	}
}

func TestManagedCoreInterruptedStartNotReplayed(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	os.MkdirAll(filepath.Dir(corePath()), 0700)
	os.WriteFile(corePath(), []byte("existing"), 0700)
	a.managedContext = context.Background()
	request(a, "POST", "/api/core/ensure", "{}")
	a.state.Jobs[0].Status = "running"
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	b.managedContext = context.Background()
	request(b, "POST", "/api/core/ensure", "{}")
	if len(b.state.Jobs) != 1 || b.coreStatus().Status != "failed" || b.process() {
		t.Fatal("interrupted start replayed")
	}
}

func TestCoreOutputControllerAnnouncement(t *testing.T) {
	for _, invalid := range []string{"RESTful API listening at: 0.0.0.0:1234", "RESTful API listening at: 192.0.2.1:1234", "RESTful API listening at: 127.0.0.1:0", "RESTful API listening at: 127.0.0.1:65536", "RESTful API listening at: 127.0.0.1:1234evil", "RESTful API unix listening at: /tmp/core.sock"} {
		output := &coreOutput{}
		fmt.Fprintln(output, invalid)
		if output.Controller() != "" {
			t.Fatalf("invalid announcement accepted: %s", invalid)
		}
	}
	output := &coreOutput{}
	fmt.Fprintln(output, strings.Repeat("x", 10000))
	for _, part := range []string{`time="..." level=info msg="RESTful API listen`, `ing at: 127.0.0.1:`, "54321\"\n"} {
		output.Write([]byte(part))
	}
	if output.Controller() != "http://127.0.0.1:54321" || len(output.String()) > 8192 {
		t.Fatal("fragmented announcement or bounded output failed")
	}
}

func TestManagedCoreReadinessFailureCleansUp(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		versionStatus, patchStatus int
	}{
		{"unauthorized", 401, 204}, {"log restore rejected", 200, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedCore(t)
			var probes, patches atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "" {
					t.Error("missing launch credential")
				}
				if r.Method == "PATCH" {
					patches.Add(1)
					w.WriteHeader(tc.patchStatus)
					return
				}
				probes.Add(1)
				w.WriteHeader(tc.versionStatus)
				w.Write([]byte(`{"version":"test"}`))
			}))
			defer server.Close()
			a := testApp(t, "")
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			a.managedContext = ctx
			a.state.Config.Port = 0
			if err := os.MkdirAll(filepath.Dir(corePath()), 0700); err != nil {
				t.Fatal(err)
			}
			source := fmt.Sprintf("#!/usr/bin/env python3\nimport time\nprint('RESTful API listening at: %s',flush=True)\ntime.sleep(5)\n", strings.TrimPrefix(server.URL, "http://"))
			if err := os.WriteFile(corePath(), []byte(source), 0700); err != nil {
				t.Fatal(err)
			}
			if err := a.startCore(a.state.Config); err == nil || a.controller != "" || a.coreCommand != nil || a.coreStatus().Status != "failed" {
				t.Fatal("failed readiness accepted or child retained")
			}
			if probes.Load() == 0 || (tc.versionStatus == 200 && patches.Load() == 0) {
				t.Fatal("readiness failure path was not exercised")
			}
			files, err := filepath.Glob(filepath.Join(a.dir, "managed-core", "nulas-*.json"))
			if err != nil || len(files) != 0 {
				t.Fatal("private launch config retained")
			}
		})
	}
}
