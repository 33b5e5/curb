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
	got, err := loadPin(path, tofuKey(u))
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
	if err := savePin(path, tofuKey(u), sha256hex("echo hi")); err != nil {
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
	if err := savePin(path, tofuKey(u), sha256hex("echo old")); err != nil {
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
	if !strings.Contains(v.Hint, "--inspect") || !strings.Contains(v.Hint, u.String()) {
		t.Errorf("hint should suggest --inspect with the URL, got %q", v.Hint)
	}
}

func TestTofuSieve_ForcePinOverridesMismatch(t *testing.T) {
	// Unit-level: forcePin turns a mismatch into a pass. That it also rewrites the
	// pin is covered end-to-end by TestRun_VetModeTOFUPinFlagOverrides.
	path := tofuTempPath(t)
	u, _ := url.Parse("https://example.com/install.sh")
	if err := savePin(path, tofuKey(u), sha256hex("echo old")); err != nil {
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
	// Re-evaluating A with B's body should NOT match; the pins are per-URL.
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
	// comment and the unrelated pin survive; b's old hash is replaced. Keys are
	// stored percent-encoded, so seed them that way.
	seeded := "# user notes\n" +
		encodeKey("https://a.example/x") + "  " + sha256hex("a") + "\n" +
		"\n" +
		encodeKey("https://b.example/y") + "  " + sha256hex("b") + "\n"
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
	parsedSrvURL, _ := url.Parse(srv.URL)
	got, _ := loadPin(path, tofuKey(parsedSrvURL))
	if got != sha256hex("echo second") {
		t.Errorf("pin not updated to new hash: got %q", got)
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestTofuKey_Canonicalization(t *testing.T) {
	base := "https://example.com/install.sh"
	equivalents := []string{
		"https://Example.COM/install.sh",
		"https://EXAMPLE.com:443/install.sh",
		"https://example.com./install.sh",
		"https://example.com/install.sh#frag",
		"https://user:pass@example.com/install.sh",
	}
	want := tofuKey(mustParse(t, base))
	for _, e := range equivalents {
		if got := tofuKey(mustParse(t, e)); got != want {
			t.Errorf("tofuKey(%q) = %q, want %q (== tofuKey(%q))", e, got, want, base)
		}
	}

	distinct := []string{
		"https://example.com/other.sh",
		"https://other.com/install.sh",
		"https://example.com:8443/install.sh",
		"https://example.com/install.sh?v=2",
	}
	for _, d := range distinct {
		if got := tofuKey(mustParse(t, d)); got == want {
			t.Errorf("tofuKey(%q) = %q, should differ from base key", d, got)
		}
	}

	// Idempotence: re-parsing the canonical form yields the same key.
	for _, raw := range append(equivalents, distinct...) {
		k := tofuKey(mustParse(t, raw))
		if again := tofuKey(mustParse(t, k)); again != k {
			t.Errorf("tofuKey not idempotent for %q: %q -> %q", raw, k, again)
		}
	}
}

func TestEncodeKey_RoundTrip(t *testing.T) {
	for _, s := range []string{
		"https://example.com/x",
		"https://example.com/has space",
		"https://example.com/has+plus",
		"https://example.com/has#hash",
		"https://example.com/has%percent",
		"https://example.com/has\ttab",
	} {
		enc := encodeKey(s)
		if len(strings.Fields(enc)) != 1 {
			t.Errorf("encodeKey(%q) = %q is not a single whitespace-free field", s, enc)
		}
		dec, err := decodeKey(enc)
		if err != nil {
			t.Fatalf("decodeKey(%q): %v", enc, err)
		}
		if dec != s {
			t.Errorf("round-trip: decodeKey(encodeKey(%q)) = %q", s, dec)
		}
	}
}

// A URL with a literal space in the query would, under the old whitespace-split
// format, produce a 3+ field line that never matched on read (re-pinning every
// fetch) and never replaced on write (unbounded growth). Percent-encoding the
// key fixes both.
func TestTofuSieve_SpaceInQueryPinsAndMatches(t *testing.T) {
	path := tofuTempPath(t)
	u := mustParse(t, "https://example.com/x?q=a b")
	if !strings.Contains(u.String(), " ") {
		t.Fatalf("precondition: expected a raw space in %q", u.String())
	}
	s := tofuSieve{path: path, stderr: io.Discard}
	if v := s.Evaluate([]byte("body"), SieveMeta{URL: u}); v.Block {
		t.Fatalf("first use blocked: %s", v.Reason)
	}
	if v := s.Evaluate([]byte("body"), SieveMeta{URL: u}); v.Block {
		t.Fatalf("second eval blocked (pin did not round-trip): %s", v.Reason)
	}
}

func TestSavePin_NoDuplicateGrowthForSpaceURL(t *testing.T) {
	path := tofuTempPath(t)
	key := tofuKey(mustParse(t, "https://example.com/x?q=a b"))
	if err := savePin(path, key, sha256hex("first")); err != nil {
		t.Fatal(err)
	}
	if err := savePin(path, key, sha256hex("second")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := encodeKey(key)
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == want {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one line for key, got %d:\n%s", count, data)
	}
	got, err := loadPin(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if got != sha256hex("second") {
		t.Errorf("pin = %q, want second hash %q", got, sha256hex("second"))
	}
}

// Pin under one spelling, then re-evaluate under an equivalent spelling: the
// canonical key means a same body hits the pin and a changed body still blocks.
func TestTofuSieve_CanonicalKeyAcrossSpellings(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantBlock bool
	}{
		{"same body hits pin", "body", false},
		{"changed body blocks", "other", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := tofuTempPath(t)
			orig := mustParse(t, "https://example.com/x")
			s := tofuSieve{path: path, stderr: io.Discard}
			if v := s.Evaluate([]byte("body"), SieveMeta{URL: orig}); v.Block {
				t.Fatalf("first use blocked: %s", v.Reason)
			}
			equiv := mustParse(t, "https://EXAMPLE.com:443/x")
			s2 := tofuSieve{path: path, stderr: io.Discard}
			if v := s2.Evaluate([]byte(c.body), SieveMeta{URL: equiv}); v.Block != c.wantBlock {
				t.Errorf("block = %v, want %v (reason: %s)", v.Block, c.wantBlock, v.Reason)
			}
		})
	}
}

func TestTofuSieve_UnreadablePinFileBlocks(t *testing.T) {
	// A directory at the pin path makes os.Open fail with a non-IsNotExist
	// error regardless of privilege: the sieve must fail closed.
	dir := filepath.Join(t.TempDir(), "known.txt")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := tofuSieve{path: dir, stderr: io.Discard}
	v := s.Evaluate([]byte("body"), SieveMeta{URL: mustParse(t, "https://example.com/x")})
	if !v.Block {
		t.Fatal("expected fail-closed block on unreadable pin file")
	}
	if !strings.Contains(v.Reason, "cannot read") || !strings.Contains(v.Reason, dir) {
		t.Errorf("reason = %q, want 'cannot read' and the path", v.Reason)
	}
}

func TestRun_VetModeBlocksOnUnreadablePin(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "echo hi")
	}))
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "known.txt")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	cfg := config{forcedMode: modeVet, stdout: &stdout, stderr: io.Discard, tofuPath: dir}
	err := run(srv.Client(), cfg, srv.URL)
	if err == nil {
		t.Fatal("expected error from unreadable pin file in vet mode")
	}
	if !strings.Contains(err.Error(), "tofu") {
		t.Errorf("expected tofu in error, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on block, got %q", stdout.String())
	}
}

// With --pin, the sieve always re-pins and passes; the stderr notice differs by
// whether this is a first pin, an unchanged hash, or a genuine change.
func TestTofuSieve_ForcePinMessaging(t *testing.T) {
	cases := []struct {
		name         string
		prepin       string // body whose hash is pinned beforehand; "" = no prior pin
		body         string
		wantContains []string
		wantAbsent   []string
	}{
		{"first use", "", "body", []string{"pinning"}, []string{"->"}},
		{"unchanged", "body", "body", nil, []string{"->", "re-pinning"}},
		{"changed", "old", "new", []string{short(sha256hex("old")), short(sha256hex("new")), "re-pin"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := tofuTempPath(t)
			u := mustParse(t, "https://example.com/x")
			if c.prepin != "" {
				if err := savePin(path, tofuKey(u), sha256hex(c.prepin)); err != nil {
					t.Fatal(err)
				}
			}
			var stderr bytes.Buffer
			s := tofuSieve{path: path, forcePin: true, stderr: &stderr}
			if v := s.Evaluate([]byte(c.body), SieveMeta{URL: u}); v.Block {
				t.Fatalf("forcePin should pass, got block: %s", v.Reason)
			}
			out := stderr.String()
			for _, w := range c.wantContains {
				if !strings.Contains(out, w) {
					t.Errorf("stderr %q missing %q", out, w)
				}
			}
			for _, w := range c.wantAbsent {
				if strings.Contains(out, w) {
					t.Errorf("stderr %q should not contain %q", out, w)
				}
			}
			got, err := loadPin(path, tofuKey(u))
			if err != nil {
				t.Fatal(err)
			}
			if got != sha256hex(c.body) {
				t.Errorf("pin = %q, want %q (forcePin always writes current)", got, sha256hex(c.body))
			}
		})
	}
}

func TestSavePin_RemovesTempOnRenameFailure(t *testing.T) {
	// Make the final path component an existing directory so os.Rename fails;
	// the temp file must not be left behind.
	dir := t.TempDir()
	path := filepath.Join(dir, "known.txt")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := savePin(path, "https://example.com/x", sha256hex("body")); err == nil {
		t.Fatal("expected savePin to fail when target is a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".known.tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}
