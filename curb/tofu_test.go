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
	// Unit-level: forcePin turns a mismatch into a pass. That it also rewrites the
	// pin is covered end-to-end by TestRun_VetModeTOFUPinFlagOverrides.
	path := tofuTempPath(t)
	u, _ := url.Parse("https://example.com/install.sh")
	if err := savePin(path, u.String(), sha256hex("echo old")); err != nil {
		t.Fatal(err)
	}
	s := tofuSieve{path: path, forcePin: true, stderr: io.Discard}
	if v := s.Evaluate([]byte("echo new"), SieveMeta{URL: u}); v.Block {
		t.Fatalf("expected pass with --pin despite mismatch, got block: %s", v.Reason)
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

func TestSavePin_CreatesDirsAndPreservesComments(t *testing.T) {
	// First pin lands in a directory that does not exist yet: savePin must create
	// the parents.
	path := filepath.Join(t.TempDir(), "nested", "dir", "known.txt")
	if err := savePin(path, "https://b.example/y", sha256hex("b")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected pin file (and parent dirs) to be created: %v", err)
	}

	// Hand-add a comment and an unrelated entry, then update b's hash. The
	// comment and the unrelated pin survive; b's old hash is replaced.
	seeded := "# user notes\n" +
		"https://a.example/x  " + sha256hex("a") + "\n" +
		"\n" +
		"https://b.example/y  " + sha256hex("b") + "\n"
	if err := os.WriteFile(path, []byte(seeded), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := savePin(path, "https://b.example/y", sha256hex("b2")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# user notes", sha256hex("a"), sha256hex("b2")} {
		if !strings.Contains(string(got), want) {
			t.Errorf("expected %q preserved/applied, got:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), sha256hex("b")+"\n") {
		t.Errorf("old hash for b still present:\n%s", got)
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

func TestValidateVetFlags(t *testing.T) {
	cases := []struct {
		name    string
		pin     bool
		noPin   bool
		force   bool
		forced  mode
		wantErr bool
	}{
		{"neither", false, false, false, modeAuto, false},
		{"pin with vet", true, false, false, modeVet, false},
		{"no-pin with vet", false, true, false, modeVet, false},
		{"force with vet", false, false, true, modeVet, false},
		{"all three (force+pin)", true, false, true, modeVet, false},
		{"pin+no-pin conflict", true, true, false, modeVet, true},
		{"pin without vet", true, false, false, modeAuto, true},
		{"no-pin without vet", false, true, false, modeInspect, true},
		{"force without vet", false, false, true, modeAuto, true},
	}
	for _, c := range cases {
		err := validateVetFlags(c.pin, c.noPin, c.force, c.forced)
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

func TestRun_VetModeTOFUBlocksOnHashChange(t *testing.T) {
	body := "echo first"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer srv.Close()

	path := tofuTempPath(t)
	cfg := config{forcedMode: modeVet, stdout: io.Discard, stderr: io.Discard, tofuPath: path}
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

func TestRun_VetModeTOFUPinFlagOverrides(t *testing.T) {
	body := "echo first"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer srv.Close()

	path := tofuTempPath(t)
	cfg := config{forcedMode: modeVet, stdout: io.Discard, stderr: io.Discard, tofuPath: path}
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
