# curb — agent guidance

`curb` is a modern, HTTPS-only transport utility written in Go. The repo is public on GitHub and published to pkg.go.dev
with a tagged release.

## Hard constraints

- **Standard library only.** No third-party dependencies, ever. If a feature seems to require one, push back or design
  around it.
- **HTTPS only.** No `http://`, no other protocols. No carveouts (not even `localhost`).
- **Go 1.23+.** Module path is `gocurb.dev`.

## Project philosophy

- Stay small. Curb is meant to be a tool a curious developer can read in one sitting.
- Honest framing about safety: curb offers a modern memory-safe runtime, a tiny HTTPS-only attack surface, and in vet
  mode (`--vet`), extra friction. Do not overpromise on safety in our messaging.
- No punching down at curl or other tools. curl is a masterpiece of plumbing maintained under difficult conditions. curb
  is a minimalist alternative.

## Architecture (planned)

- Three runtime modes, picked from `Content-Type` with magic-byte sniff fallback and override flags:
  - **inspect:** stream JSON/HTML/XML/etc to stdout. The only mode in v0.1.
  - **download:** auto-save on TTY, stream on pipe, `-o` always wins.
  - **vet:** buffer the body, validate, emit only on pass. Intended for the `curl | sh` use case.
- **Modular sieves** (Go interface, compiled-in) drive `vet` validation. TOFU pinning and heuristic checks are sieves;
  more can be added over time.

## Roadmap and issue tracking

Planned work, feature requests, and bug reports live in GitHub issues at https://github.com/33b5e5/curb/issues. There is
no checked-in roadmap file.

With `gh` installed and run from inside the repo:

- `gh issue list` shows what's open.
- `gh issue list --label enhancement` filters to feature requests.
- `gh issue view <number>` shows full detail, including the design rationale captured at filing time.

When picking up an issue, reference its number in commit messages and PR descriptions so the history stays linked back
to its motivation.

## Style

- Exit codes: `0` success, `1` runtime error, `2` usage error.
- Errors to stderr, payload to stdout, progress/metrics to stderr.
- Prefer `flag` (stdlib) once we need more than one CLI argument; raw `os.Args` for v0.1 since there's exactly one.

## `docs/` (GitHub Pages)

`docs/index.html` serves two purposes from one file: a minimal human landing page at `https://gocurb.dev/`, and the Go
vanity-import resolver via the `<meta name="go-import">` tag — that tag is what makes `go install gocurb.dev@latest`
find the GitHub repo. Don't remove it. `docs/CNAME` is GitHub Pages' custom-domain marker.

## Site posture (`gocurb.dev` on GitHub Pages)

GH Pages does not let you set custom response headers on custom domains. The "Enforce HTTPS" toggle adds an HTTP→HTTPS
301 but does *not* send `Strict-Transport-Security`, `Content-Security-Policy`, `X-Frame-Options`, or
`X-Content-Type-Options`. Thus, Mozilla Observatory rates the site C/50 as a result; SSL Labs rates it A. This is
cosmetic for our actual risk model. The landing page is one static HTML file with no JavaScript, no forms, no user
input, no third-party resources, no interactivity. CSP/XFO/XCTO protect against attacks that have no vector. HSTS at the
browser level is already provided by the `.dev` TLD preload list.

## Source of truth

This file is the source of truth for project direction and conventions; GitHub issues track planned work (see *Roadmap
and issue tracking* above).

## Claude

In a new repo you might want to add a symlink like:

`ln -s AGENTS.md CLAUDE.md`

We used AGENTS.md to be agnostic as to the tool used, but CLAUDE.md if present is not tracked in Git.
