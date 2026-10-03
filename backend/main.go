package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	Port int    `json:"mixed-port"`
	Mode string `json:"mode"`
	LAN  bool   `json:"allow-lan"`
	IPv6 bool   `json:"ipv6"`
	Log  string `json:"log-level"`
}
type Job struct {
	ID      string    `json:"id"`
	Action  string    `json:"action"`
	Status  string    `json:"status"`
	Message string    `json:"message"`
	Created time.Time `json:"created"`
	Config  Config    `json:"config"`
}
type State struct {
	Config   Config    `json:"config"`
	Jobs     []Job     `json:"jobs"`
	Profiles []Profile `json:"profiles"`
}
type App struct {
	mu                      sync.Mutex
	controlMu               sync.Mutex
	state                   State
	dir, controller, secret string
	wake                    chan struct{}
	client                  *http.Client
	managedContext          context.Context
	coreCommand             *exec.Cmd
	coreDone                chan struct{}
	coreRuntime             CoreStatus
}

func validate(c Config) error {
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if c.Mode != "rule" && c.Mode != "global" && c.Mode != "direct" {
		return errors.New("invalid mode")
	}
	switch c.Log {
	case "info", "warning", "error", "debug", "silent":
	default:
		return errors.New("invalid log level")
	}
	return nil
}
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (a *App) persist() error {
	b, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(a.dir, "state.json"), b)
}
func newApp(dir, controller, secret string) (*App, error) {
	if controller != "" {
		u, e := url.Parse(controller)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("invalid MIHOMO_CONTROLLER URL")
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	a := &App{dir: dir, controller: strings.TrimRight(controller, "/"), secret: secret, wake: make(chan struct{}, 1), client: &http.Client{Timeout: 20 * time.Second}}
	a.state = State{Config: Config{7890, "rule", false, false, "info"}, Jobs: []Job{}}
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil {
		if err = json.Unmarshal(b, &a.state); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for i := range a.state.Jobs {
		if a.state.Jobs[i].Status == "running" {
			a.state.Jobs[i].Status = "failed"
			a.state.Jobs[i].Message = "Backend interrupted; inspect core state before retrying"
		}
	}
	if err = a.persist(); err != nil {
		return nil, err
	}
	return a, nil
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, err error) {
	reply(w, status, map[string]string{"error": err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}
func (a *App) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", frontendHandler())
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, errors.New("unknown API endpoint")) })
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		reply(w, 200, map[string]any{"status": "ok", "controllerConfigured": a.controller != ""})
	})
	mux.HandleFunc("GET /api/core", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		reply(w, 200, a.coreStatus())
	})
	mux.HandleFunc("POST /api/core/ensure", a.ensureCore)
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		reply(w, 200, a.state.Config)
	})
	mux.HandleFunc("PUT /api/config", func(w http.ResponseWriter, r *http.Request) {
		var c Config
		if e := decode(w, r, &c); e != nil {
			fail(w, 400, e)
			return
		}
		if e := validate(c); e != nil {
			fail(w, 400, e)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		old := a.state.Config
		a.state.Config = c
		if e := a.persist(); e != nil {
			a.state.Config = old
			fail(w, 500, e)
			return
		}
		reply(w, 200, c)
	})
	a.profileRoutes(mux)
	a.nodeRoutes(mux)
	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		reply(w, 200, a.state.Jobs)
	})
	mux.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Action string `json:"action"`
		}
		if e := decode(w, r, &body); e != nil {
			fail(w, 400, e)
			return
		}
		if body.Action != "generate" && body.Action != "apply" && body.Action != "install-core" {
			fail(w, 400, errors.New("invalid action"))
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if body.Action == "apply" && a.controller == "" {
			fail(w, 409, errors.New("Set MIHOMO_CONTROLLER on the backend first"))
			return
		}
		if len(a.state.Jobs) >= 1000 {
			fail(w, 409, errors.New("job history limit reached; archive state before submitting more jobs"))
			return
		}
		for _, j := range a.state.Jobs {
			if j.Status == "queued" || j.Status == "running" {
				fail(w, 409, errors.New("a background operation is already pending"))
				return
			}
		}
		id := make([]byte, 16)
		if _, e := rand.Read(id); e != nil {
			fail(w, 500, e)
			return
		}
		j := Job{hex.EncodeToString(id), body.Action, "queued", "Waiting for worker", time.Now().UTC(), a.state.Config}
		a.state.Jobs = append(a.state.Jobs, j)
		if e := a.persist(); e != nil {
			a.state.Jobs = a.state.Jobs[:len(a.state.Jobs)-1]
			fail(w, 500, e)
			return
		}
		select {
		case a.wake <- struct{}{}:
		default:
		}
		reply(w, 202, j)
	})
	// Reject cross-origin writes, including form submissions from other sites.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == "PUT" || r.Method == "POST" || r.Method == "PATCH" || r.Method == "DELETE" {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				fail(w, 415, errors.New("application/json required"))
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if e != nil || u.Scheme != scheme || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
					fail(w, 403, errors.New("cross-origin write rejected"))
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *App) runJob(j Job) error {
	if j.Action == "install-core" {
		if err := a.installCore(); err != nil {
			return err
		}
		return a.startCore(j.Config)
	}
	b, e := json.MarshalIndent(j.Config, "", "  ")
	if e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(a.dir, j.ID+".yaml"), b); e != nil {
		return e
	}
	if j.Action == "generate" {
		return nil
	}
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	a.mu.Lock()
	controller, secret := a.controller, a.secret
	a.mu.Unlock()
	// PATCH updates only supported runtime settings, preserving proxies, rules and controller credentials.
	req, e := http.NewRequest(http.MethodPatch, controller+"/configs", bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	res, e := a.client.Do(req)
	if e != nil {
		return errors.New("Mihomo controller unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Mihomo rejected configuration (HTTP %d)", res.StatusCode)
	}
	return nil
}
func (a *App) process() bool {
	a.mu.Lock()
	idx := -1
	for i, j := range a.state.Jobs {
		if j.Status == "queued" {
			idx = i
			break
		}
	}
	if idx < 0 {
		a.mu.Unlock()
		return false
	}
	a.state.Jobs[idx].Status = "running"
	a.state.Jobs[idx].Message = "Processing configuration"
	if e := a.persist(); e != nil {
		a.state.Jobs[idx].Status = "queued"
		a.mu.Unlock()
		log.Printf("persist running job: %v", e)
		return false
	}
	j := a.state.Jobs[idx]
	a.mu.Unlock()
	err := a.runJob(j)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Jobs[idx].Status = "succeeded"
	a.state.Jobs[idx].Message = "Configuration generated"
	if j.Action == "install-core" {
		a.state.Jobs[idx].Message = "Mihomo core installed"
		if a.managedContext != nil {
			a.state.Jobs[idx].Message = "Mihomo core started and controller verified"
		}
	}
	if j.Action == "apply" {
		a.state.Jobs[idx].Message = "Runtime settings applied to Mihomo"
	}
	if err != nil {
		a.state.Jobs[idx].Status = "failed"
		a.state.Jobs[idx].Message = err.Error()
	}
	if e := a.persist(); e != nil {
		log.Printf("persist completed job: %v", e)
	}
	return true
}
func (a *App) worker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for ctx.Err() == nil && a.process() {
		}
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		case <-ticker.C:
		}
	}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	a, e := newApp(env("NULAS_DATA_DIR", ".data"), os.Getenv("MIHOMO_CONTROLLER"), os.Getenv("MIHOMO_SECRET"))
	if e != nil {
		log.Fatal(e)
	}
	listener, err := net.Listen("tcp", env("NULAS_ADDR", "127.0.0.1:8080"))
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if a.controller == "" {
		a.managedContext = ctx
		a.mu.Lock()
		if _, err := a.queueCore(); err != nil {
			log.Printf("queue core: %v", err)
		}
		a.mu.Unlock()
	}
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); a.worker(ctx) }()
	s := &http.Server{Addr: env("NULAS_ADDR", "127.0.0.1:8080"), Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Nulas backend listening on %s", s.Addr)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.Shutdown(shutdown)
	}()
	if err := s.Serve(listener); err != nil && err != http.ErrServerClosed {
		stop()
		log.Printf("server: %v", err)
	}
	stop()
	<-workerDone
	a.mu.Lock()
	done := a.coreDone
	a.mu.Unlock()
	if done != nil {
		<-done
	}
}
