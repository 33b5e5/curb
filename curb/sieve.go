package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Sieve evaluates a buffered script body before it's passed to a shell.
// Compiled-in sieves run sequentially; any block halts the pipe-guard.
type Sieve interface {
	Name() string
	Evaluate(body []byte, meta SieveMeta) Verdict
}

// SieveMeta carries response context that sieves may use to produce
// diagnostic reasons or hints.
type SieveMeta struct {
	URL    *url.URL
	Status int         // HTTP status code; 0 if not set (e.g. in unit tests)
	Header http.Header // response headers; may be nil
}

// Verdict is a sieve's decision. A blocking verdict carries a human-readable
// Reason; Hint is an optional next-step suggestion specific to that sieve.
type Verdict struct {
	Block  bool
	Reason string
	Hint   string
}

// nonemptySieve blocks empty bodies: a 200 with no payload would otherwise
// silently produce a no-op pipe and look like success.
type nonemptySieve struct{}

func (nonemptySieve) Name() string { return "nonempty" }
func (nonemptySieve) Evaluate(body []byte, meta SieveMeta) Verdict {
	if len(body) != 0 {
		return Verdict{}
	}
	reason := "response body is empty"
	if meta.Status != 0 {
		reason = fmt.Sprintf("response body is empty (HTTP %d)", meta.Status)
	}
	hint := ""
	if meta.Status == http.StatusNoContent {
		hint = "HTTP 204 means no content by design — likely the wrong URL"
	}
	return Verdict{Block: true, Reason: reason, Hint: hint}
}

// sieveHit pairs a sieve's name with its (blocking) verdict, so we can format
// the report uniformly across the abort and --force paths.
type sieveHit struct {
	name string
	v    Verdict
}

// script implements pipe-guard mode: buffer the body, run sieves, emit only
// if all pass. With cfg.force, sieve blocks become warnings on stderr and the
// body is piped anyway.
func script(body io.Reader, meta SieveMeta, sieves []Sieve, cfg config) error {
	buf, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	var hits []sieveHit
	for _, s := range sieves {
		if v := s.Evaluate(buf, meta); v.Block {
			hits = append(hits, sieveHit{s.Name(), v})
		}
	}
	if len(hits) == 0 {
		_, err := cfg.stdout.Write(buf)
		return err
	}
	if cfg.force {
		fmt.Fprintf(cfg.stderr, "curb: %s\n", formatHits(hits, meta.URL, true))
		_, err := cfg.stdout.Write(buf)
		return err
	}
	return errors.New(formatHits(hits, meta.URL, false))
}

func formatHits(hits []sieveHit, u *url.URL, forced bool) string {
	var msg strings.Builder
	if forced {
		msg.WriteString("--force in effect; sieve(s) flagged this body:")
	} else {
		msg.WriteString("--script blocked by sieve(s):")
	}
	for _, h := range hits {
		fmt.Fprintf(&msg, "\n  - %s: %s", h.name, h.v.Reason)
		if h.v.Hint != "" {
			fmt.Fprintf(&msg, "\n      → %s", h.v.Hint)
		}
	}
	if !forced && u != nil {
		fmt.Fprintf(&msg, "\n\nnext steps:")
		fmt.Fprintf(&msg, "\n  curb --inspect %s", u)
		fmt.Fprintf(&msg, "\n  curb --force --script %s", u)
	}
	return msg.String()
}
