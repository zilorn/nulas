package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Only the production entry point enables ownership; external controllers are untouched.
func (a *App) startCore(c Config) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.managedContext == nil {
		return nil
	}
	if a.coreCommand != nil {
		return nil
	}
	if err := a.managedContext.Err(); err != nil {
		return err
	}
	if a.state.Applied != nil {
		c = a.state.Applied.Config
	}
	a.coreRuntime = CoreStatus{"starting", "正在启动内核…"}
	failStart := func(err error) error { a.coreRuntime = CoreStatus{"failed", err.Error()}; return err }
	// Do not connect to or terminate another process occupying the controller port.
	listener, err := net.Listen("tcp", "127.0.0.1:9090")
	if err != nil {
		return failStart(fmt.Errorf("内核控制端口 9090 不可用：%w", err))
	}
	listener.Close()
	proxyListener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", c.Port))
	if err != nil {
		return failStart(fmt.Errorf("内核代理端口 %d 不可用：%w", c.Port, err))
	}
	proxyListener.Close()
	if c.Port == 9090 {
		return failStart(errors.New("代理端口不能与内核控制端口 9090 相同"))
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return failStart(err)
	}
	secret := hex.EncodeToString(key)
	dir, err := filepath.Abs(filepath.Join(a.dir, "managed-core"))
	if err != nil {
		return failStart(err)
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return failStart(err)
	}
	// Fresh credentials each launch; restore only the last successful snapshot.
	config, err := a.managedStartupConfig(c, secret)
	if err != nil {
		return failStart(err)
	}
	data, err := json.Marshal(config)
	if err != nil {
		return failStart(err)
	}
	file, err := os.CreateTemp(dir, "nulas-*.json")
	if err != nil {
		return failStart(err)
	}
	path := file.Name()
	if _, err = file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return failStart(err)
	}
	if err = file.Close(); err != nil {
		os.Remove(path)
		return failStart(err)
	}
	binary, err := filepath.Abs(corePath())
	if err != nil {
		os.Remove(path)
		return failStart(err)
	}
	cmd := exec.CommandContext(a.managedContext, binary, "-d", dir, "-f", path)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 2 * time.Second
	output := &coreOutput{}
	cmd.Stdout, cmd.Stderr = output, output
	if err = cmd.Start(); err != nil {
		os.Remove(path)
		return failStart(fmt.Errorf("内核启动失败：%w", err))
	}
	done := make(chan struct{})
	a.coreCommand, a.coreDone = cmd, done
	go func() {
		err := cmd.Wait()
		os.Remove(path)
		a.mu.Lock()
		a.coreCommand = nil
		a.controller, a.secret = "", ""
		a.coreRuntime = CoreStatus{"failed", fmt.Sprintf("内核已退出：%v；%s", err, output.String())}
		a.mu.Unlock()
		close(done)
	}()
	// Release the lock while probing readiness, allowing status requests and process exit.
	a.mu.Unlock()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(a.managedContext, "GET", "http://127.0.0.1:9090/version", nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		response, probeErr := client.Do(req)
		if probeErr == nil {
			var version struct {
				Version string `json:"version"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&version)
			response.Body.Close()
			ready = response.StatusCode == 200 && decodeErr == nil && version.Version != ""
		}
		if ready {
			break
		}
		select {
		case <-done:
		case <-a.managedContext.Done():
		case <-time.After(100 * time.Millisecond):
		}
		select {
		case <-done:
			deadline = time.Time{}
		case <-a.managedContext.Done():
			deadline = time.Time{}
		default:
		}
	}
	a.mu.Lock()
	if ready && a.coreCommand == cmd {
		a.controller, a.secret = "http://127.0.0.1:9090", secret
		a.coreRuntime = CoreStatus{"running", "内核运行中，控制接口已连接（基础配置仅直连）。"}
		if a.state.Applied != nil {
			a.coreRuntime.Message = "内核运行中，已加载保存的应用配置：" + a.state.Applied.Name
		}
		return nil
	}
	if a.coreCommand == cmd {
		cmd.Process.Kill()
	}
	a.mu.Unlock()
	<-done
	a.mu.Lock()
	return failStart(errors.New("内核未能启动并通过控制接口检查；" + output.String()))
}

type coreOutput struct {
	mu     sync.Mutex
	buffer limitedOutput
}

func (o *coreOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buffer.Write(p)
}
func (o *coreOutput) String() string { o.mu.Lock(); defer o.mu.Unlock(); return string(o.buffer.data) }
