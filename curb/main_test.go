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
	"time"
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

func TestRun_RejectsBadURLs(t *testing.T) {
	// A malformed target URL is a usage error (exit 2), not a runtime failure.
	cases := []struct {
		name string
		url  string
		want string // substring the error message should contain ("" to skip)
	}{
		{"non-https scheme", "http://example.com", "https"},
		{"unparseable", "://nope", ""},
		{"empty host", "https://", "host"},
		{"empty host with path", "https:///path", "host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := run(http.DefaultClient, config{stdout: io.Discard, stderr: io.Discard}, c.url)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err, c.want)
			}
			if code := exitCode(err); code != 2 {
				t.Errorf("exitCode = %d, want 2 (usage)", code)
			}
		})
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
	// A non-2xx response is a runtime failure (exit 1), not a usage error.
	if code := exitCode(err); code != 1 {
		t.Errorf("exitCode = %d, want 1 (runtime)", code)
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

func TestRun_StreamSummaryGoesToStderr(t *testing.T) {
	// Force modeDownload on a pipe so stream() runs deterministically, then
	// assert the payload/metrics contract: body to stdout, the byte/duration
	// summary to stderr (never stdout).
	payload := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(payload)
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	cfg := config{forcedMode: modeDownload, stdout: &stdout, stderr: &stderr, stdoutIsTTY: false}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	// stdout being exactly the payload already proves no summary leaked into it.
	if !bytes.Equal(stdout.Bytes(), payload) {
		t.Errorf("stdout = %x, want only the payload %x", stdout.Bytes(), payload)
	}
	// The byte/duration summary goes to stderr; assert it was written there
	// rather than pinning its exact text.
	if stderr.Len() == 0 {
		t.Errorf("expected a byte/duration summary on stderr, got none")
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

func TestRun_VetModePassesBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "echo hi")
	}))
	defer srv.Close()

	var buf bytes.Buffer
	cfg := config{forcedMode: modeVet, stdout: &buf, stderr: io.Discard, noPin: true}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("run: %v", err)
	}
	if buf.String() != "echo hi" {
		t.Errorf("stdout = %q, want %q", buf.String(), "echo hi")
	}
}

func TestRun_VetModeBlocksEmpty(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
	}))
	defer srv.Close()

	var buf bytes.Buffer
	cfg := config{forcedMode: modeVet, stdout: &buf, stderr: io.Discard, noPin: true}
	err := run(srv.Client(), cfg, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected sieve block, got %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("stdout should be empty on block, got %q", buf.String())
	}
}

func TestRun_VetModeForcePipesDespiteBlock(t *testing.T) {
	body := "rm -rf /\n"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	cfg := config{
		forcedMode: modeVet,
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
	// --force must not be silent: a warning lands on stderr. Match the concept,
	// not the exact phrasing or the heuristic's internal hit id.
	if !strings.Contains(strings.ToLower(stderr.String()), "force") {
		t.Errorf("expected a force warning on stderr, got %q", stderr.String())
	}
}

func TestRun_SniffsWhenContentTypeIsOctetStream(t *testing.T) {
	// application/octet-stream is treated as "no useful type": resolveMode falls
	// back to sniffing the body, which the PNG magic bytes resolve to image/png
	// (a download), saved under the URL-derived filename.
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

// The --vet hint fires only when a shell-shaped body streams to a pipe: not in
// vet mode (which is what we'd suggest), not with -o (writes a file), and not on
// a TTY (a human reading it isn't piping to sh). One row per gate condition.
func TestRun_ShellPipeHint(t *testing.T) {
	const shebang = "#!/bin/sh\necho hi\n"
	cases := []struct {
		name        string
		contentType string
		body        string
		stdoutIsTTY bool
		mode        mode
		toFile      bool
		noPin       bool
		wantHint    bool
	}{
		{"shebang piped", "text/plain", shebang, false, modeAuto, false, false, true},
		{"shell content-type piped", "application/x-shellscript", "install everything\n", false, modeAuto, false, false, true},
		{"shell shape on a TTY", "text/plain", shebang, true, modeAuto, false, false, false},
		{"vet mode suppresses hint", "text/x-shellscript", shebang, false, modeVet, false, true, false},
		{"non-shell body", "application/json", `{"ok":true}`, false, modeAuto, false, false, false},
		{"saving to a file", "text/x-shellscript", shebang, false, modeDownload, true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", c.contentType)
				io.WriteString(w, c.body)
			}))
			defer srv.Close()

			var stdout, stderr bytes.Buffer
			cfg := config{forcedMode: c.mode, stdout: &stdout, stderr: &stderr, stdoutIsTTY: c.stdoutIsTTY, noPin: c.noPin}
			if c.toFile {
				cfg.outPath = filepath.Join(t.TempDir(), "script.sh")
			}
			if err := run(srv.Client(), cfg, srv.URL); err != nil {
				t.Fatalf("run: %v", err)
			}
			if got := strings.Contains(stderr.String(), "consider --vet"); got != c.wantHint {
				t.Errorf("hint present = %v, want %v (stderr %q)", got, c.wantHint, stderr.String())
			}
			// When the body streams to a pipe, peeking for the shebang must not
			// eat any of it.
			if c.mode == modeAuto && !c.stdoutIsTTY && !c.toFile {
				if stdout.String() != c.body {
					t.Errorf("stdout = %q, want full body %q", stdout.String(), c.body)
				}
			}
		})
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
		{"vet", false, false, true, false, modeVet, false},
		{"out implies download", false, false, false, true, modeDownload, false},
		{"out + inspect conflict", true, false, false, true, modeAuto, true},
		{"out + vet conflict", false, false, true, true, modeAuto, true},
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

func TestNewClient_NetworkSelection(t *testing.T) {
	// httptest binds to 127.0.0.1: tcp4 reaches it, while tcp6 has no suitable
	// address for that literal and must fail before any HTTP round-trip.
	cases := []struct {
		name    string
		network string
		wantErr bool
	}{
		{"tcp4 reaches a v4 server", "tcp4", false},
		{"tcp6 cannot reach a v4 server", "tcp6", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, "ok")
			}))
			defer srv.Close()

			client := newClient(c.network)
			// Trust the test server's self-signed cert via its transport's TLSConfig.
			client.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig

			var buf bytes.Buffer
			err := run(client, bufCfg(&buf), srv.URL)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if !c.wantErr && buf.String() != "ok" {
				t.Errorf("body = %q, want %q", buf.String(), "ok")
			}
		})
	}
}

func TestNewClient_WiresStallTimeouts(t *testing.T) {
	// Every network gets a connect timeout (via DialContext), a TLS-handshake
	// timeout, and a response-header timeout; none of them depends on -4/-6.
	for _, network := range []string{"tcp", "tcp4", "tcp6"} {
		tr := newClient(network).Transport.(*http.Transport)
		if tr.DialContext == nil {
			t.Errorf("%s: DialContext should be set so the connect timeout applies", network)
		}
		if tr.TLSHandshakeTimeout == 0 {
			t.Errorf("%s: TLSHandshakeTimeout should be set", network)
		}
		if tr.ResponseHeaderTimeout == 0 {
			t.Errorf("%s: ResponseHeaderTimeout should be set", network)
		}
	}
}

func TestResolveTimeout(t *testing.T) {
	cases := []struct {
		name    string
		flagVal time.Duration
		set     bool
		mode    mode
		want    time.Duration
	}{
		{"vet default", 0, false, modeVet, defaultVetTimeout},
		{"inspect no default", 0, false, modeInspect, 0},
		{"download no default", 0, false, modeDownload, 0},
		{"auto no default", 0, false, modeAuto, 0},
		{"explicit overrides vet default", 5 * time.Second, true, modeVet, 5 * time.Second},
		{"explicit applies to inspect", 5 * time.Second, true, modeInspect, 5 * time.Second},
		{"explicit zero disables vet default", 0, true, modeVet, 0},
	}
	for _, c := range cases {
		if got := resolveTimeout(c.flagVal, c.set, c.mode); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRun_OverallTimeoutFires(t *testing.T) {
	// Server sends headers, then stalls the body forever. Only the overall
	// deadline (not the header timeout) can break this; cfg.timeout must fire.
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}))
	defer srv.Close()
	defer close(release)

	cfg := config{forcedMode: modeInspect, stdout: io.Discard, stderr: io.Discard, timeout: 100 * time.Millisecond}
	if err := run(srv.Client(), cfg, srv.URL); err == nil {
		t.Fatal("expected an overall-timeout error, got nil")
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
