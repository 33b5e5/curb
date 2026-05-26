package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bufCfg(buf *bytes.Buffer) config {
	return config{stdout: buf, stderr: io.Discard}
}

func TestRun_StreamsBodyOn2xx(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello, curb")
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if err := run(srv.Client(), bufCfg(&buf), srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := buf.String(); got != "hello, curb" {
		t.Errorf("body = %q, want %q", got, "hello, curb")
	}
}

func TestRun_RejectsNonHTTPS(t *testing.T) {
	err := run(http.DefaultClient, config{stdout: io.Discard, stderr: io.Discard}, "http://example.com")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("expected https-only error, got %v", err)
	}
}

func TestRun_ErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err := run(srv.Client(), config{stdout: io.Discard, stderr: io.Discard}, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got %v", err)
	}
}

func TestRun_RejectsUnparseableURL(t *testing.T) {
	err := run(http.DefaultClient, config{stdout: io.Discard, stderr: io.Discard}, "://nope")
	if err == nil {
		t.Errorf("expected parse error, got nil")
	}
}

func TestRun_FollowsHTTPSRedirect(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, srv.URL+"/dest", http.StatusFound)
			return
		}
		io.WriteString(w, "after-redirect")
	}))
	defer srv.Close()

	client := srv.Client()
	client.CheckRedirect = checkRedirect

	var buf bytes.Buffer
	if err := run(client, bufCfg(&buf), srv.URL+"/start"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := buf.String(); got != "after-redirect" {
		t.Errorf("body = %q, want %q", got, "after-redirect")
	}
}

func TestRun_RejectsHTTPRedirect(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/blocked", http.StatusFound)
	}))
	defer srv.Close()

	client := srv.Client()
	client.CheckRedirect = checkRedirect

	err := run(client, config{stdout: io.Discard, stderr: io.Discard}, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Errorf("expected non-https refusal, got %v", err)
	}
}

func TestRun_DownloadsBinaryWithOutFlag(t *testing.T) {
	payload := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(payload)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	cfg := config{outPath: dst, forcedMode: modeDownload, stdout: io.Discard, stderr: io.Discard}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("file content mismatch: got %x, want %x", got, payload)
	}
}

func TestRun_StreamsBinaryOnPipe(t *testing.T) {
	payload := []byte{0x89, 0x50, 0x4E, 0x47}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(payload)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	cfg := config{stdout: &buf, stderr: io.Discard, stdoutIsTTY: false}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Errorf("stdout = %x, want %x", buf.Bytes(), payload)
	}
}

func TestRun_AutoSavesBinaryOnTTY(t *testing.T) {
	payload := []byte{0x00, 0x01, 0x02, 0x03}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="payload.bin"`)
		w.Write(payload)
	}))
	defer srv.Close()

	withCWD(t, t.TempDir())

	cfg := config{stdout: io.Discard, stderr: io.Discard, stdoutIsTTY: true}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := os.ReadFile("payload.bin")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("file content mismatch: got %x, want %x", got, payload)
	}
}

func TestRun_RefusesToClobberAutoFilename(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="exists.bin"`)
		w.Write([]byte{0x00})
	}))
	defer srv.Close()

	withCWD(t, t.TempDir())
	if err := os.WriteFile("exists.bin", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config{stdout: io.Discard, stderr: io.Discard, stdoutIsTTY: true}
	err := run(srv.Client(), cfg, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected clobber refusal, got %v", err)
	}
	got, _ := os.ReadFile("exists.bin")
	if string(got) != "old" {
		t.Errorf("file overwritten: %q", got)
	}
}

func TestRun_OutFlagOverwrites(t *testing.T) {
	payload := []byte("new content")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(payload)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config{outPath: dst, forcedMode: modeDownload, stdout: io.Discard, stderr: io.Discard}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, payload) {
		t.Errorf("file content = %q, want %q", got, payload)
	}
}

func TestRun_ForcedInspectStreamsBinary(t *testing.T) {
	body := []byte{0x89, 0x50, 0x4E, 0x47}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(body)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	cfg := config{forcedMode: modeInspect, stdout: &buf, stderr: io.Discard}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), body) {
		t.Errorf("body = %x, want %x", buf.Bytes(), body)
	}
}

func TestRun_ForcedDownloadOnTextual(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "hello")
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "saved.txt")
	cfg := config{outPath: dst, forcedMode: modeDownload, stdout: io.Discard, stderr: io.Discard}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestRun_ScriptModePassesBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "echo hi")
	}))
	defer srv.Close()

	var buf bytes.Buffer
	cfg := config{forcedMode: modeScript, stdout: &buf, stderr: io.Discard, noPin: true}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	if buf.String() != "echo hi" {
		t.Errorf("stdout = %q, want %q", buf.String(), "echo hi")
	}
}

func TestRun_ScriptModeBlocksEmpty(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
	}))
	defer srv.Close()

	var buf bytes.Buffer
	cfg := config{forcedMode: modeScript, stdout: &buf, stderr: io.Discard, noPin: true}
	err := run(srv.Client(), cfg, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected sieve block, got %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("stdout should be empty on block, got %q", buf.String())
	}
}

func TestRun_ScriptModeForcePipesDespiteBlock(t *testing.T) {
	body := "rm -rf /\n"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	cfg := config{
		forcedMode: modeScript,
		stdout:     &stdout,
		stderr:     &stderr,
		noPin:      true,
		force:      true,
	}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("expected pass with --force, got %v", err)
	}
	if stdout.String() != body {
		t.Errorf("stdout = %q, want %q", stdout.String(), body)
	}
	if !strings.Contains(stderr.String(), "--force in effect") {
		t.Errorf("expected --force warning on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "rm-rf-root") {
		t.Errorf("expected heuristic hit reported in warning, got %q", stderr.String())
	}
}

func TestRun_SniffsWhenContentTypeMissing(t *testing.T) {
	// PNG magic bytes; server doesn't set Content-Type (but Go's auto-sniff will
	// fill it in for the response). Force the empty CT path by overriding to
	// octet-stream so resolveMode falls back to sniffing.
	payload := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(payload)
	}))
	defer srv.Close()

	withCWD(t, t.TempDir())

	cfg := config{stdout: io.Discard, stderr: io.Discard, stdoutIsTTY: true}
	err := run(srv.Client(), cfg, srv.URL+"/snap.png")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := os.ReadFile("snap.png")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("file content mismatch")
	}
}

func TestSelectMode(t *testing.T) {
	cases := []struct {
		name     string
		i, d, s  bool
		hasOut   bool
		wantMode mode
		wantErr  bool
	}{
		{"none", false, false, false, false, modeAuto, false},
		{"inspect", true, false, false, false, modeInspect, false},
		{"download", false, true, false, false, modeDownload, false},
		{"script", false, false, true, false, modeScript, false},
		{"out implies download", false, false, false, true, modeDownload, false},
		{"out + inspect conflict", true, false, false, true, modeAuto, true},
		{"out + script conflict", false, false, true, true, modeAuto, true},
		{"out + download fine", false, true, false, true, modeDownload, false},
		{"inspect + download conflict", true, true, false, false, modeAuto, true},
		{"all three conflict", true, true, true, false, modeAuto, true},
	}
	for _, c := range cases {
		got, err := selectMode(c.i, c.d, c.s, c.hasOut)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
			continue
		}
		if err == nil && got != c.wantMode {
			t.Errorf("%s: mode=%v want=%v", c.name, got, c.wantMode)
		}
	}
}

func TestSelectNetwork(t *testing.T) {
	cases := []struct {
		name    string
		v4, v6  bool
		want    string
		wantErr bool
	}{
		{"neither", false, false, "tcp", false},
		{"v4 only", true, false, "tcp4", false},
		{"v6 only", false, true, "tcp6", false},
		{"both conflict", true, true, "", true},
	}
	for _, c := range cases {
		got, err := selectNetwork(c.v4, c.v6)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("%s: got=%q want=%q", c.name, got, c.want)
		}
	}
}

func TestNewClient_ForcedIPv6FailsAgainstV4Server(t *testing.T) {
	// httptest binds to 127.0.0.1; dialing that literal over tcp6 has no
	// suitable address and must fail before any HTTP round-trip.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "should not reach handler")
	}))
	defer srv.Close()

	client := newClient("tcp6")
	// Trust the test server's self-signed cert via the same transport's TLSConfig.
	client.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig

	err := run(client, config{stdout: io.Discard, stderr: io.Discard}, srv.URL)
	if err == nil {
		t.Fatal("expected error dialing v4 literal over tcp6, got nil")
	}
}

func TestNewClient_ForcedIPv4Succeeds(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := newClient("tcp4")
	client.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig

	var buf bytes.Buffer
	if err := run(client, bufCfg(&buf), srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	if buf.String() != "ok" {
		t.Errorf("body = %q, want %q", buf.String(), "ok")
	}
}

func TestNewClient_DefaultLeavesDialerAlone(t *testing.T) {
	// Sanity: when network is "tcp", DialContext stays nil so net/http uses its
	// own default dialer (preserving prior behavior).
	c := newClient("tcp")
	if c.Transport.(*http.Transport).DialContext != nil {
		t.Errorf("expected nil DialContext for default network, got non-nil")
	}
}

func TestSafeBasename(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"file.bin", "file.bin", false},
		{"../escape", "escape", false}, // filepath.Base strips ../
		{"/etc/passwd", "passwd", false},
		{"..", "", true},
		{".", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := safeBasename(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%q: err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("%q: got=%q want=%q", c.in, got, c.want)
		}
	}
}

func TestIsTextual(t *testing.T) {
	textual := []string{
		"text/plain", "text/html", "text/css",
		"application/json", "application/xml", "application/yaml",
		"application/ld+json", "application/atom+xml", "application/foo+yaml",
		"application/javascript",
	}
	binary := []string{
		"application/octet-stream", "image/png", "video/mp4",
		"application/zip", "application/x-tar", "",
	}
	for _, mt := range textual {
		if !isTextual(mt) {
			t.Errorf("%q: want textual", mt)
		}
	}
	for _, mt := range binary {
		if isTextual(mt) {
			t.Errorf("%q: want binary", mt)
		}
	}
}

// withCWD chdirs to dir for the duration of the test and restores on cleanup.
func withCWD(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Logf("chdir back: %v", err)
		}
	})
}
