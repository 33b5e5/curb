package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tofuTempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "known.txt")
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestTofuSieve_FirstUsePinsAndPasses(t *testing.T) {
	path := tofuTempPath(t)
	var stderr bytes.Buffer
	s := tofuSieve{path: path, stderr: &stderr}
	u, _ := url.Parse("https://example.com/install.sh")

	v := s.Evaluate([]byte("echo hi"), SieveMeta{URL: u})
	if v.Block {
		t.Fatalf("expected pass on first use, got block: %s", v.Reason)
	}
	got, err := loadPin(path, u.String())
	if err != nil {
		t.Fatalf("loadPin: %v", err)
	}
	if got != sha256hex("echo hi") {
		t.Errorf("pin = %q, want %q", got, sha256hex("echo hi"))
	}
	if !strings.Contains(stderr.String(), "pinned") {
		t.Errorf("expected pinned notice on stderr, got %q", stderr.String())
	}
}

func TestTofuSieve_MatchingHashPasses(t *testing.T) {
	path := tofuTempPath(t)
	u, _ := url.Parse("https://example.com/install.sh")
	if err := savePin(path, u.String(), sha256hex("echo hi")); err != nil {
		t.Fatal(err)
	}
	s := tofuSieve{path: path, stderr: io.Discard}
	v := s.Evaluate([]byte("echo hi"), SieveMeta{URL: u})
	if v.Block {
		t.Errorf("expected pass on match, got block: %s", v.Reason)
	}
}

func TestTofuSieve_MismatchBlocks(t *testing.T) {
	path := tofuTempPath(t)
	u, _ := url.Parse("https://example.com/install.sh")
	if err := savePin(path, u.String(), sha256hex("echo old")); err != nil {
		t.Fatal(err)
	}
	s := tofuSieve{path: path, stderr: io.Discard}
	v := s.Evaluate([]byte("echo new"), SieveMeta{URL: u})
	if !v.Block {
		t.Fatal("expected block on hash change")
	}
	if !strings.Contains(v.Reason, "changed") {
		t.Errorf("reason = %q, want it to mention 'changed'", v.Reason)
	}
	if !strings.Contains(v.Hint, "--pin") || !strings.Contains(v.Hint, u.String()) {
		t.Errorf("hint should suggest --pin with the URL, got %q", v.Hint)
	}
}

func TestTofuSieve_ForcePinOverridesMismatch(t *testing.T) {
	path := tofuTempPath(t)
	u, _ := url.Parse("https://example.com/install.sh")
	if err := savePin(path, u.String(), sha256hex("echo old")); err != nil {
		t.Fatal(err)
	}
	s := tofuSieve{path: path, forcePin: true, stderr: io.Discard}
	v := s.Evaluate([]byte("echo new"), SieveMeta{URL: u})
	if v.Block {
		t.Fatalf("expected pass with --pin, got block: %s", v.Reason)
	}
	got, _ := loadPin(path, u.String())
	if got != sha256hex("echo new") {
		t.Errorf("pin not updated: got %q, want %q", got, sha256hex("echo new"))
	}
}

func TestTofuSieve_IsolatesByURL(t *testing.T) {
	path := tofuTempPath(t)
	uA, _ := url.Parse("https://a.example/x.sh")
	uB, _ := url.Parse("https://b.example/y.sh")
	s := tofuSieve{path: path, stderr: io.Discard}

	if v := s.Evaluate([]byte("A body"), SieveMeta{URL: uA}); v.Block {
		t.Fatalf("A first use blocked: %s", v.Reason)
	}
	if v := s.Evaluate([]byte("B body"), SieveMeta{URL: uB}); v.Block {
		t.Fatalf("B first use blocked: %s", v.Reason)
	}
	// Re-evaluating A with B's body should NOT match — the pins are per-URL.
	if v := s.Evaluate([]byte("B body"), SieveMeta{URL: uA}); !v.Block {
		t.Errorf("expected block: A pinned to A-body but presented B-body")
	}
}

func TestTofuSieve_MissingURLIsNoOp(t *testing.T) {
	path := tofuTempPath(t)
	s := tofuSieve{path: path, stderr: io.Discard}
	if v := s.Evaluate([]byte("x"), SieveMeta{}); v.Block {
		t.Errorf("expected no-op when URL missing, got block: %s", v.Reason)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("pin file should not have been written, stat err = %v", err)
	}
}

func TestSavePin_AtomicAndPreservesComments(t *testing.T) {
	path := tofuTempPath(t)
	initial := "# user notes\n" +
		"https://a.example/x  " + sha256hex("a") + "\n" +
		"\n" +
		"https://b.example/y  " + sha256hex("b") + "\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	// Update b's hash.
	if err := savePin(path, "https://b.example/y", sha256hex("b2")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "# user notes") {
		t.Errorf("comment lost: %s", got)
	}
	if !strings.Contains(string(got), sha256hex("a")) {
		t.Errorf("unrelated entry lost: %s", got)
	}
	if !strings.Contains(string(got), sha256hex("b2")) {
		t.Errorf("update not applied: %s", got)
	}
	if strings.Contains(string(got), sha256hex("b")+"\n") {
		t.Errorf("old hash for b still present: %s", got)
	}
}

func TestSavePin_CreatesMissingDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "known.txt")
	if err := savePin(path, "https://e.example/s", sha256hex("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected pin file to exist: %v", err)
	}
}

func TestLoadPin_AbsentFileNoError(t *testing.T) {
	got, err := loadPin(filepath.Join(t.TempDir(), "does-not-exist.txt"), "https://x")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

func TestValidateScriptFlags(t *testing.T) {
	cases := []struct {
		name    string
		pin     bool
		noPin   bool
		force   bool
		forced  mode
		wantErr bool
	}{
		{"neither", false, false, false, modeAuto, false},
		{"pin with script", true, false, false, modeScript, false},
		{"no-pin with script", false, true, false, modeScript, false},
		{"force with script", false, false, true, modeScript, false},
		{"all three (force+pin)", true, false, true, modeScript, false},
		{"pin+no-pin conflict", true, true, false, modeScript, true},
		{"pin without script", true, false, false, modeAuto, true},
		{"no-pin without script", false, true, false, modeInspect, true},
		{"force without script", false, false, true, modeAuto, true},
	}
	for _, c := range cases {
		err := validateScriptFlags(c.pin, c.noPin, c.force, c.forced)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestBuildSieves_RespectsNoPin(t *testing.T) {
	cfg := config{noPin: true, stderr: io.Discard}
	got, err := buildSieves(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.Name() == "tofu" {
			t.Errorf("expected no tofu sieve when --no-pin set, got %v", got)
		}
	}
}

func TestBuildSieves_IncludesTofuByDefault(t *testing.T) {
	cfg := config{tofuPath: tofuTempPath(t), stderr: io.Discard}
	got, err := buildSieves(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range got {
		if s.Name() == "tofu" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected tofu sieve in default chain, got %v", got)
	}
}

func TestRun_ScriptModeTOFUBlocksOnHashChange(t *testing.T) {
	body := "echo first"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer srv.Close()

	path := tofuTempPath(t)
	cfg := config{forcedMode: modeScript, stdout: io.Discard, stderr: io.Discard, tofuPath: path}
	// First run: TOFU pins.
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Server now serves a different body.
	body = "echo second"
	var stdout bytes.Buffer
	cfg.stdout = &stdout
	err := run(srv.Client(), cfg, srv.URL)
	if err == nil {
		t.Fatal("expected block on second run with changed body")
	}
	if !strings.Contains(err.Error(), "tofu") {
		t.Errorf("expected tofu in error, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on block, got %q", stdout.String())
	}
}

func TestRun_ScriptModeTOFUPinFlagOverrides(t *testing.T) {
	body := "echo first"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer srv.Close()

	path := tofuTempPath(t)
	cfg := config{forcedMode: modeScript, stdout: io.Discard, stderr: io.Discard, tofuPath: path}
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("first run: %v", err)
	}
	body = "echo second"
	var stdout bytes.Buffer
	cfg.stdout = &stdout
	cfg.forcePin = true
	if err := run(srv.Client(), cfg, srv.URL); err != nil {
		t.Fatalf("forcePin run: %v", err)
	}
	if stdout.String() != "echo second" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "echo second")
	}
	got, _ := loadPin(path, srv.URL)
	if got != sha256hex("echo second") {
		t.Errorf("pin not updated to new hash: got %q", got)
	}
}
