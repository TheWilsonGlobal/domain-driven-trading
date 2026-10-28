package main

import (
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPServer_HasTimeoutsAndHeaderLimit(t *testing.T) {
	s := newHTTPServer("5470", http.NewServeMux(), false)
	if s.Addr != ":5470" {
		t.Errorf("Addr = %q", s.Addr)
	}
	if s.ReadHeaderTimeout <= 0 || s.ReadHeaderTimeout > 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %s, want 1s..10s (slowloris)", s.ReadHeaderTimeout)
	}
	if s.ReadTimeout <= 0 || s.ReadTimeout > 30*time.Second {
		t.Errorf("ReadTimeout = %s", s.ReadTimeout)
	}
	if s.WriteTimeout <= 0 || s.WriteTimeout > 30*time.Second {
		t.Errorf("WriteTimeout = %s", s.WriteTimeout)
	}
	if s.IdleTimeout <= 0 || s.IdleTimeout > 2*time.Minute {
		t.Errorf("IdleTimeout = %s", s.IdleTimeout)
	}
	if s.MaxHeaderBytes <= 0 || s.MaxHeaderBytes > 64<<10 {
		t.Errorf("MaxHeaderBytes = %d, want 1..64KiB (default is 1MiB)", s.MaxHeaderBytes)
	}
	if s.Handler == nil {
		t.Error("Handler is nil")
	}
}

// SEC-20: the unauthenticated dev mode must only listen on loopback.
func TestNewHTTPServer_DevAuthBindsLoopbackOnly(t *testing.T) {
	if s := newHTTPServer("5470", http.NewServeMux(), true); s.Addr != "127.0.0.1:5470" {
		t.Errorf("dev-auth Addr = %q, want 127.0.0.1:5470", s.Addr)
	}
	if s := newHTTPServer("5470", http.NewServeMux(), false); s.Addr != ":5470" {
		t.Errorf("normal Addr = %q, want :5470", s.Addr)
	}
}
