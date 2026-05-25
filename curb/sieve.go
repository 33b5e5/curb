package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// Sieve evaluates a buffered script body before it's passed to a shell.
// Compiled-in sieves run sequentially; any block halts the pipe-guard.
type Sieve interface {
	Name() string
	Evaluate(body []byte, meta SieveMeta) Verdict
}

type SieveMeta struct {
	URL *url.URL
}

type Verdict struct {
	Block  bool
	Reason string
}

// nonemptySieve blocks empty bodies: a 200 with no payload would otherwise
// silently produce a no-op pipe and look like success.
type nonemptySieve struct{}

func (nonemptySieve) Name() string { return "nonempty" }
func (nonemptySieve) Evaluate(body []byte, _ SieveMeta) Verdict {
	if len(body) == 0 {
		return Verdict{Block: true, Reason: "response body is empty"}
	}
	return Verdict{}
}

// script implements Mode A: buffer the body, run sieves, emit only if all pass.
func script(body io.Reader, u *url.URL, sieves []Sieve, cfg config) error {
	buf, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	meta := SieveMeta{URL: u}
	var blocks []string
	for _, s := range sieves {
		if v := s.Evaluate(buf, meta); v.Block {
			blocks = append(blocks, fmt.Sprintf("%s: %s", s.Name(), v.Reason))
		}
	}
	if len(blocks) > 0 {
		var msg strings.Builder
		msg.WriteString("--script blocked by sieve(s):")
		for _, b := range blocks {
			fmt.Fprintf(&msg, "\n  - %s", b)
		}
		return errors.New(msg.String())
	}
	_, err = cfg.stdout.Write(buf)
	return err
}
