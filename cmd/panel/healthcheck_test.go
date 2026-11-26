package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	var ready atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" || !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")
	if err := healthcheck(addr); err == nil {
		t.Fatal("a panel that is not ready passed")
	}
	ready.Store(true)
	if err := healthcheck(addr); err != nil {
		t.Fatal(err)
	}
	// A wildcard listen address is asked on loopback.
	_, port, _ := strings.Cut(addr, ":")
	if err := healthcheck("0.0.0.0:" + port); err != nil {
		t.Fatal(err)
	}
	if err := healthcheck(":" + port); err != nil {
		t.Fatal(err)
	}
	if err := healthcheck("nonsense"); err == nil {
		t.Fatal("a bad address passed")
	}
}
