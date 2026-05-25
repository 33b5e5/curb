package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRun_StreamsBodyOn2xx(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello, curb")
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if err := run(srv.Client(), &buf, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := buf.String(); got != "hello, curb" {
		t.Errorf("body = %q, want %q", got, "hello, curb")
	}
}

func TestRun_RejectsNonHTTPS(t *testing.T) {
	err := run(http.DefaultClient, io.Discard, "http://example.com")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("expected https-only error, got %v", err)
	}
}

func TestRun_ErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err := run(srv.Client(), io.Discard, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got %v", err)
	}
}

func TestRun_RejectsUnparseableURL(t *testing.T) {
	err := run(http.DefaultClient, io.Discard, "://nope")
	if err == nil {
		t.Errorf("expected parse error, got nil")
	}
}
