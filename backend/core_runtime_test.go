package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagedCoreLifecycle(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.managedContext = ctx
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
 def log_message(self,*args): pass
http.server.HTTPServer(('127.0.0.1',9090),Handler).serve_forever()
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

func TestManagedCorePortConflict(t *testing.T) {
	isolatedCore(t)
	listener, err := net.Listen("tcp", "127.0.0.1:9090")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	a := testApp(t, "")
	a.managedContext = context.Background()
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
