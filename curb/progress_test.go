package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeClock returns a closure satisfying progressBar.now and lets the test
// advance virtual time deterministically.
func fakeClock(start time.Time) (func() time.Time, func(d time.Duration)) {
	cur := start
	return func() time.Time { return cur }, func(d time.Duration) { cur = cur.Add(d) }
}

func newTestBar(out io.Writer, total int64) (*progressBar, func(time.Duration)) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now, advance := fakeClock(start)
	bar := &progressBar{
		out:   out,
		label: "curb: downloading test",
		total: total,
		now:   now,
		start: start,
	}
	return bar, advance
}

func TestProgressBar_RendersWithKnownTotal(t *testing.T) {
	var buf bytes.Buffer
	bar, advance := newTestBar(&buf, 1000)

	bar.advance(250)
	advance(200 * time.Millisecond)
	bar.advance(250)

	out := buf.String()
	if !strings.Contains(out, "50.0%") {
		t.Errorf("expected 50%% in output, got %q", out)
	}
	if !strings.Contains(out, "ETA") {
		t.Errorf("expected ETA in output, got %q", out)
	}
	if !strings.Contains(out, "\r") {
		t.Errorf("expected carriage return for in-place update, got %q", out)
	}
}

func TestProgressBar_OmitsPercentWhenTotalUnknown(t *testing.T) {
	var buf bytes.Buffer
	bar, advance := newTestBar(&buf, -1)

	bar.advance(500)
	advance(200 * time.Millisecond)
	bar.advance(500)

	out := buf.String()
	if strings.Contains(out, "%") {
		t.Errorf("expected no percent when total unknown, got %q", out)
	}
	if strings.Contains(out, "ETA") {
		t.Errorf("expected no ETA when total unknown, got %q", out)
	}
	if !strings.Contains(out, "/s") {
		t.Errorf("expected rate in output, got %q", out)
	}
}

func TestProgressBar_ThrottlesUpdates(t *testing.T) {
	var buf bytes.Buffer
	bar, advance := newTestBar(&buf, 1000)

	bar.advance(100) // first render — always emits
	for i := 0; i < 9; i++ {
		advance(10 * time.Millisecond) // stays strictly under minProgressInterval
		bar.advance(10)
	}

	if got := strings.Count(buf.String(), "\r"); got != 1 {
		t.Errorf("expected 1 throttled render, got %d in %q", got, buf.String())
	}
}

func TestProgressBar_RendersOnCompletionAndClampsOvershoot(t *testing.T) {
	var buf bytes.Buffer
	bar, _ := newTestBar(&buf, 100)

	bar.advance(50) // first render
	bar.advance(50) // hits total even within the throttle window

	if got := strings.Count(buf.String(), "\r"); got != 2 {
		t.Errorf("expected render-on-complete, got %d renders in %q", got, buf.String())
	}
	if !strings.Contains(buf.String(), "100.0%") {
		t.Errorf("expected 100%% on completion, got %q", buf.String())
	}

	// Servers occasionally send more bytes than Content-Length advertised; the
	// overshoot still renders (n > total) but must clamp to 100%, never 150%.
	bar.advance(50)
	if out := buf.String(); strings.Contains(out, "150.0%") || !strings.Contains(out, "100.0%") {
		t.Errorf("overshoot should clamp to 100%%, got %q", out)
	}
}

func TestProgressBar_FinishClearsLine(t *testing.T) {
	var buf bytes.Buffer
	bar, _ := newTestBar(&buf, 1000)
	bar.advance(500)

	pre := buf.Len()
	bar.finish()
	post := buf.String()[pre:]

	if !strings.HasPrefix(post, "\r") || !strings.HasSuffix(post, "\r") {
		t.Errorf("finish should bracket clear with \\r, got %q", post)
	}
	if strings.TrimSpace(strings.Trim(post, "\r")) != "" {
		t.Errorf("finish clear region should be only spaces, got %q", post)
	}
}

func TestProgressBar_FinishNoopWhenNothingRendered(t *testing.T) {
	var buf bytes.Buffer
	bar, _ := newTestBar(&buf, 1000)
	bar.finish()
	if buf.Len() != 0 {
		t.Errorf("finish before render should write nothing, got %q", buf.String())
	}
}

func TestProgressReader_CountsAndForwards(t *testing.T) {
	var buf bytes.Buffer
	bar, _ := newTestBar(&buf, 11)
	r := bar.wrap(strings.NewReader("hello world"))

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("read returned %q, want %q", got, "hello world")
	}
	if bar.n != 11 {
		t.Errorf("bar counted %d bytes, want 11", bar.n)
	}
}

// withProgress attaches a bar only when stderr is a TTY; otherwise it returns
// the reader untouched and writes nothing (e.g. shell redirect, test buffer).
func TestWithProgress_TTYGate(t *testing.T) {
	cases := []struct {
		name      string
		stderrTTY bool
		wantBar   bool
	}{
		{"no bar when stderr is not a TTY", false, false},
		{"bar when stderr is a TTY", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stderr bytes.Buffer
			cfg := config{stderr: &stderr, stderrIsTTY: c.stderrTTY}
			src, bar := withProgress(strings.NewReader("hi"), "curb: x", 2, cfg)
			if (bar != nil) != c.wantBar {
				t.Fatalf("bar != nil = %v, want %v", bar != nil, c.wantBar)
			}
			if _, err := io.Copy(io.Discard, src); err != nil {
				t.Fatalf("copy: %v", err)
			}
			if !c.wantBar && stderr.Len() != 0 {
				t.Errorf("expected no stderr writes without a TTY, got %q", stderr.String())
			}
		})
	}
}
