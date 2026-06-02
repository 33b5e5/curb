package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// minProgressInterval throttles re-renders so the bar updates feel smooth
// without flooding stderr on fast connections.
const minProgressInterval = 100 * time.Millisecond

// progressBar renders single-line download progress to stderr, overwriting
// itself with \r between updates. Total may be -1 when the server omitted
// Content-Length (or the body was transparently decompressed); in that case
// the bar shows bytes + rate only, with no percent or ETA.
type progressBar struct {
	out      io.Writer
	label    string
	total    int64
	now      func() time.Time
	start    time.Time
	lastTick time.Time
	n        int64
	width    int
}

func newProgressBar(out io.Writer, label string, total int64) *progressBar {
	now := time.Now()
	return &progressBar{
		out:   out,
		label: label,
		total: total,
		now:   time.Now,
		start: now,
	}
}

func (p *progressBar) wrap(r io.Reader) io.Reader {
	return &progressReader{p: p, r: r}
}

func (p *progressBar) advance(n int) {
	p.n += int64(n)
	now := p.now()
	if now.Sub(p.lastTick) < minProgressInterval && (p.total < 0 || p.n < p.total) {
		return
	}
	p.render(now)
	p.lastTick = now
}

func (p *progressBar) render(now time.Time) {
	elapsed := now.Sub(p.start).Seconds()
	var rate float64
	if elapsed > 0 {
		rate = float64(p.n) / elapsed
	}

	var line string
	if p.total > 0 {
		pct := float64(p.n) / float64(p.total) * 100
		if pct > 100 {
			pct = 100
		}
		eta := "--"
		if rate > 0 && p.n < p.total {
			remaining := time.Duration(float64(p.total-p.n) / rate * float64(time.Second))
			eta = remaining.Round(time.Second).String()
		}
		line = fmt.Sprintf("%s  %5.1f%%  %s / %s  %s/s  ETA %s",
			p.label, pct, humanBytes(p.n), humanBytes(p.total),
			humanBytes(int64(rate)), eta)
	} else {
		line = fmt.Sprintf("%s  %s  %s/s",
			p.label, humanBytes(p.n), humanBytes(int64(rate)))
	}

	pad := ""
	if len(line) < p.width {
		pad = strings.Repeat(" ", p.width-len(line))
	}
	fmt.Fprintf(p.out, "\r%s%s", line, pad)
	if len(line) > p.width {
		p.width = len(line)
	}
}

// finish clears the rendered line so the caller can print a clean summary.
func (p *progressBar) finish() {
	if p.width == 0 {
		return
	}
	fmt.Fprintf(p.out, "\r%s\r", strings.Repeat(" ", p.width))
}

type progressReader struct {
	p *progressBar
	r io.Reader
}

func (pr *progressReader) Read(b []byte) (int, error) {
	n, err := pr.r.Read(b)
	if n > 0 {
		pr.p.advance(n)
	}
	return n, err
}
