package main

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type passSieve struct{}

func (passSieve) Name() string                       { return "pass" }
func (passSieve) Evaluate([]byte, SieveMeta) Verdict { return Verdict{} }

type blockSieve struct{ name, reason, hint string }

func (b blockSieve) Name() string { return b.name }
func (b blockSieve) Evaluate([]byte, SieveMeta) Verdict {
	return Verdict{Block: true, Reason: b.reason, Hint: b.hint}
}

type sawMetaSieve struct{ seen SieveMeta }

func (s *sawMetaSieve) Name() string { return "saw-meta" }
func (s *sawMetaSieve) Evaluate(_ []byte, m SieveMeta) Verdict {
	s.seen = m
	return Verdict{}
}

func metaFor(rawurl string) SieveMeta {
	u, _ := url.Parse(rawurl)
	return SieveMeta{URL: u}
}

func TestNonemptySieve(t *testing.T) {
	s := nonemptySieve{}
	if v := s.Evaluate([]byte("x"), SieveMeta{}); v.Block {
		t.Errorf("non-empty body should pass, got block: %s", v.Reason)
	}
	if v := s.Evaluate(nil, SieveMeta{}); !v.Block {
		t.Errorf("empty body should block")
	}
	if v := s.Evaluate([]byte{}, SieveMeta{}); !v.Block {
		t.Errorf("zero-length body should block")
	}
}

func TestNonemptySieve_MentionsHTTPStatus(t *testing.T) {
	v := nonemptySieve{}.Evaluate(nil, SieveMeta{Status: http.StatusNoContent})
	if !v.Block {
		t.Fatal("expected block")
	}
	if !strings.Contains(v.Reason, "204") {
		t.Errorf("reason should mention HTTP 204, got %q", v.Reason)
	}
	if !strings.Contains(v.Hint, "204") {
		t.Errorf("hint should explain 204, got %q", v.Hint)
	}
}

func TestVet_PassesWhenAllSievesPass(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	err := vet(strings.NewReader("echo hello"), metaFor("https://example.com/s"),
		[]Sieve{passSieve{}}, cfg)
	if err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	if stdout.String() != "echo hello" {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestVet_BlocksAndWithholdsPayload(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	err := vet(strings.NewReader("echo hello"), metaFor("https://example.com/s"),
		[]Sieve{blockSieve{name: "test", reason: "nope"}}, cfg)
	if err == nil {
		t.Fatal("expected block error")
	}
	if !strings.Contains(err.Error(), "test: nope") {
		t.Errorf("expected 'test: nope' in error, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on block, got %q", stdout.String())
	}
}

func TestVet_CollectsAllBlocks(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	err := vet(strings.NewReader("x"), metaFor("https://example.com/s"), []Sieve{
		blockSieve{name: "a", reason: "r1"},
		blockSieve{name: "b", reason: "r2"},
	}, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"a: r1", "b: r2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in error %q", want, err.Error())
		}
	}
}

func TestVet_OneBlockAmongPasses(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	err := vet(strings.NewReader("x"), metaFor("https://example.com/s"), []Sieve{
		passSieve{},
		blockSieve{name: "bad", reason: "stop"},
		passSieve{},
	}, cfg)
	if err == nil {
		t.Fatal("expected block")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on block")
	}
}

func TestVet_PassesMetaToSieves(t *testing.T) {
	saw := &sawMetaSieve{}
	cfg := config{stdout: io.Discard, stderr: io.Discard}
	u, _ := url.Parse("https://example.com/install.sh")
	meta := SieveMeta{URL: u, Status: 200, Header: http.Header{"X-Test": {"yes"}}}
	if err := vet(strings.NewReader("x"), meta, []Sieve{saw}, cfg); err != nil {
		t.Fatalf("vet: %v", err)
	}
	if saw.seen.URL != u {
		t.Errorf("URL not propagated: got %v, want %v", saw.seen.URL, u)
	}
	if saw.seen.Status != 200 {
		t.Errorf("Status not propagated: got %d", saw.seen.Status)
	}
	if saw.seen.Header.Get("X-Test") != "yes" {
		t.Errorf("Header not propagated: %v", saw.seen.Header)
	}
}

func TestVet_BlockReportIncludesHintsAndFooter(t *testing.T) {
	cfg := config{stdout: io.Discard, stderr: io.Discard}
	err := vet(strings.NewReader("x"), metaFor("https://example.com/s"), []Sieve{
		blockSieve{name: "test", reason: "boom", hint: "do the thing"},
	}, cfg)
	if err == nil {
		t.Fatal("expected block")
	}
	msg := err.Error()
	for _, want := range []string{
		"test: boom",
		"→ do the thing",
		"next steps:",
		"curb --inspect https://example.com/s",
		"curb --force --vet https://example.com/s",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q\n--- full message ---\n%s", want, msg)
		}
	}
}

func TestVet_ForcePipesAndWarns(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg := config{stdout: &stdout, stderr: &stderr, force: true}
	err := vet(strings.NewReader("danger"), metaFor("https://example.com/s"), []Sieve{
		blockSieve{name: "test", reason: "boom", hint: "do the thing"},
	}, cfg)
	if err != nil {
		t.Fatalf("expected pass with --force, got %v", err)
	}
	if stdout.String() != "danger" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "danger")
	}
	if !strings.Contains(stderr.String(), "--force in effect") {
		t.Errorf("expected --force warning on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "test: boom") {
		t.Errorf("expected sieve reason in warning, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "next steps:") {
		t.Errorf("forced warning should not include next-steps footer: %q", stderr.String())
	}
}

// infiniteReader yields an endless stream of bytes. vet must stop reading at
// the cap rather than buffer until OOM (the decompression-bomb scenario, where
// resp.Body is already gunzipped so the bytes vet sees are the expanded ones).
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func TestVet_CapsOversizeBody(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	err := vet(infiniteReader{}, metaFor("https://example.com/install.sh"),
		[]Sieve{passSieve{}}, cfg)
	if err == nil {
		t.Fatal("expected a cap error on an oversize body")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Errorf("error should mention the cap, got %v", err)
	}
	for _, want := range []string{"--inspect", "--download"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should advise %s, got %v", want, err)
		}
	}
	if stdout.Len() != 0 {
		t.Errorf("oversize body must not be emitted, got %d bytes", stdout.Len())
	}
}

func TestVet_AtLimitBodyPasses(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	body := strings.NewReader(strings.Repeat("a", maxVetBytes))
	if err := vet(body, metaFor("https://example.com/s"), []Sieve{passSieve{}}, cfg); err != nil {
		t.Fatalf("a body exactly at the cap should pass, got %v", err)
	}
	if stdout.Len() != maxVetBytes {
		t.Errorf("emitted %d bytes, want %d", stdout.Len(), maxVetBytes)
	}
}
