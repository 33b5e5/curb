package main

import (
	"bytes"
	"io"
	"net/url"
	"strings"
	"testing"
)

type passSieve struct{}

func (passSieve) Name() string                          { return "pass" }
func (passSieve) Evaluate([]byte, SieveMeta) Verdict    { return Verdict{} }

type blockSieve struct{ name, reason string }

func (b blockSieve) Name() string { return b.name }
func (b blockSieve) Evaluate([]byte, SieveMeta) Verdict {
	return Verdict{Block: true, Reason: b.reason}
}

type sawMetaSieve struct{ seen SieveMeta }

func (s *sawMetaSieve) Name() string { return "saw-meta" }
func (s *sawMetaSieve) Evaluate(_ []byte, m SieveMeta) Verdict {
	s.seen = m
	return Verdict{}
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

func TestScript_PassesWhenAllSievesPass(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	u, _ := url.Parse("https://example.com/s")
	err := script(strings.NewReader("echo hello"), u, []Sieve{passSieve{}}, cfg)
	if err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	if stdout.String() != "echo hello" {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestScript_BlocksAndWithholdsPayload(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	u, _ := url.Parse("https://example.com/s")
	err := script(strings.NewReader("echo hello"), u,
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

func TestScript_CollectsAllBlocks(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	u, _ := url.Parse("https://example.com/s")
	err := script(strings.NewReader("x"), u, []Sieve{
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

func TestScript_OneBlockAmongPasses(t *testing.T) {
	var stdout bytes.Buffer
	cfg := config{stdout: &stdout, stderr: io.Discard}
	u, _ := url.Parse("https://example.com/s")
	err := script(strings.NewReader("x"), u, []Sieve{
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

func TestScript_PassesURLToSieves(t *testing.T) {
	saw := &sawMetaSieve{}
	cfg := config{stdout: io.Discard, stderr: io.Discard}
	u, _ := url.Parse("https://example.com/install.sh")
	if err := script(strings.NewReader("x"), u, []Sieve{saw}, cfg); err != nil {
		t.Fatalf("script: %v", err)
	}
	if saw.seen.URL != u {
		t.Errorf("sieve didn't receive URL: got %v, want %v", saw.seen.URL, u)
	}
}
