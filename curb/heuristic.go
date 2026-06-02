package main

import (
	"fmt"
	"regexp"
	"strings"
)

// heuristicSieve runs pattern-based smell tests over the body. It is a friction
// layer, not a guarantee.
type heuristicSieve struct{}

func (heuristicSieve) Name() string { return "heuristic" }

type heuristicRule struct {
	name    string
	pattern *regexp.Regexp
}

// shellAlt is the interpreter set shared by the pipe-to-shell rules.
const shellAlt = `(?:bash|sh|zsh|dash|ksh|csh|tcsh|fish)`

// heuristicRules are scanned in order; every match contributes to the verdict.
var heuristicRules = []heuristicRule{
	// rm -rf of the filesystem root, allowing -r/-f reordering and root spelled
	// `/`, `//`, or `/.`. The trailing class keeps subpaths like /tmp clear.
	{
		name:    "rm-rf-root",
		pattern: regexp.MustCompile(`(?m)\brm\s+-[a-zA-Z]*[rR][a-zA-Z]*\s+/[/.]*(\s|$|\*|;|&|\|)`),
	},
	// curl/wget/fetch piped into a shell.
	{
		name:    "fetch-pipe-shell",
		pattern: regexp.MustCompile(`(?m)\b(curl|wget|fetch)\b[^\n]*\|\s*(sudo\s+)?` + shellAlt + `\b`),
	},
	// base64 piped into a shell, with or without -d/--decode.
	{
		name:    "base64-pipe-shell",
		pattern: regexp.MustCompile(`(?m)\bbase64\b[^\n]*\|\s*(sudo\s+)?` + shellAlt + `\b`),
	},
	// sudo running a shell with -c, tolerating options, `--`, and `env` before
	// the shell name.
	{
		name:    "sudo-shell",
		pattern: regexp.MustCompile(`(?m)\bsudo\s+(?:(?:-{1,2}[a-zA-Z][\w-]*|--|env)\s+)*` + shellAlt + `\s+-c\b`),
	},
	// eval of a fetch inside a command substitution, e.g. eval "$(curl ...)".
	{
		name:    "eval-fetch",
		pattern: regexp.MustCompile(`(?m)\beval\b[^\n]*(?:\$\(|` + "`" + `)[^\n]*\b(curl|wget|fetch)\b`),
	},
}

// lineContinuation matches a shell backslash-newline join, which we collapse to
// a space so a pipeline split across lines matches as one logical line.
var lineContinuation = regexp.MustCompile(`\\\r?\n`)

func (heuristicSieve) Evaluate(body []byte, _ SieveMeta) Verdict {
	scan := lineContinuation.ReplaceAll(body, []byte(" "))
	var hits []string
	for _, r := range heuristicRules {
		if r.pattern.Match(scan) {
			hits = append(hits, r.name)
		}
	}
	if len(hits) == 0 {
		return Verdict{}
	}
	return Verdict{
		Block:  true,
		Reason: fmt.Sprintf("smell test matched %s (heuristic, not a guarantee)", strings.Join(hits, ", ")),
	}
}
