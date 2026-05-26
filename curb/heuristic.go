package main

import (
	"fmt"
	"regexp"
	"strings"
)

// heuristicSieve runs pattern-based smell tests over the body.
// False positives are expected: this is a friction layer, not a guarantee.
// The Reason text says so explicitly.
type heuristicSieve struct{}

func (heuristicSieve) Name() string { return "heuristic" }

type heuristicRule struct {
	name    string
	pattern *regexp.Regexp
}

// heuristicRules are scanned in order; every match contributes to the verdict.
var heuristicRules = []heuristicRule{
	// `rm -rf /` and friends — targeting the filesystem root. Allows -r/-f
	// reordering. The trailing class ensures `rm -rf /tmp` is NOT flagged:
	// only literal root, root-with-glob, or root followed by a shell separator.
	{
		name:    "rm-rf-root",
		pattern: regexp.MustCompile(`(?m)\brm\s+-[a-zA-Z]*[rR][a-zA-Z]*\s+/(\s|$|\*|;|&|\|)`),
	},
	// curl/wget/fetch piped to a shell on the same line — the
	// nested-pipe-to-bash shape inside an already-piped-to-bash script.
	{
		name:    "fetch-pipe-shell",
		pattern: regexp.MustCompile(`(?m)\b(curl|wget|fetch)\b[^\n]*\|\s*(sudo\s+)?(bash|sh|zsh|dash|ksh)\b`),
	},
	// base64 piped to a shell — the classic obfuscation channel. Flags
	// independent of `-d`/`--decode` so `base64 | sh` and `base64 -d | sh`
	// both trip.
	{
		name:    "base64-pipe-shell",
		pattern: regexp.MustCompile(`(?m)\bbase64\b[^\n]*\|\s*(sudo\s+)?(bash|sh|zsh|dash|ksh)\b`),
	},
	// sudo invoking a shell with `-c` — privileged execution of an inline
	// string. Plain `sudo apt-get install foo` is not flagged; only the
	// shape that hides what's being privileged.
	{
		name:    "sudo-shell",
		pattern: regexp.MustCompile(`(?m)\bsudo\s+(bash|sh|zsh|dash|ksh)\s+-c\b`),
	},
}

func (heuristicSieve) Evaluate(body []byte, _ SieveMeta) Verdict {
	var hits []string
	for _, r := range heuristicRules {
		if r.pattern.Match(body) {
			hits = append(hits, r.name)
		}
	}
	if len(hits) == 0 {
		return Verdict{}
	}
	return Verdict{
		Block:  true,
		Reason: fmt.Sprintf("smell test matched %s (heuristic — not a guarantee)", strings.Join(hits, ", ")),
	}
}
