package main

import (
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Go remains the same-origin public entry point; Node renders pages on loopback.
func frontendHandler() http.Handler {
	if dir := os.Getenv("NULAS_WEB_DIR"); dir != "" {
		mux := http.NewServeMux()
		mux.Handle("/", http.FileServer(http.Dir(dir)))
		for _, path := range []string{"/tasks", "/nodes"} {
			mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
				http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			})
		}
		return mux
	}
	origin, configErr := serverSSRURL()
	u, err := url.Parse(origin)
	if configErr != nil || err != nil || !validSSRURL(u) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fail(w, http.StatusServiceUnavailable, errors.New("invalid NULAS_SSR_URL: expected a loopback HTTP origin"))
		})
	}
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.Out.Host = r.In.Host
		},
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ResponseHeaderTimeout: 15 * time.Second,
			IdleConnTimeout:       60 * time.Second,
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			fail(w, http.StatusBadGateway, errors.New("SSR frontend unavailable; start the Node frontend service"))
		},
	}
}

func validSSRURL(u *url.URL) bool {
	if u == nil || u.Scheme != "http" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}
