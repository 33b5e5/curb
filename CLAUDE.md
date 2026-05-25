# curb — agent guidance

`curb` is a modern, HTTPS-only transport utility written in Go. The user (rig) is
iterating on it privately before sharing publicly.

## Hard constraints

- **Standard library only.** No third-party dependencies, ever. If a feature seems
  to require one, push back or design around it.
- **HTTPS only.** No `http://`, no other protocols. No carveouts (not even
  `localhost`).
- **Go 1.23+.** Module path is `gocurb.dev`.

## Project philosophy

- Stay small. Curb is meant to be a tool a curious developer can read in one
  sitting.
- Honest framing about safety: curb offers a modern
  memory-safe runtime, a tiny HTTPS-only attack surface, and (in pipe-guard mode)
  friction + smell tests + audit trail. Do not overpromise safety.
- No punching down at curl. curl is a masterpiece of plumbing maintained under
  hard conditions; curb is a specialized complement, not a critique.

## Architecture (planned)

- Three runtime modes, dispatched by `Content-Type` with magic-byte sniff fallback
  and override flags:
  - **B — structured inspection:** stream JSON/HTML/XML/etc to stdout. The only
    mode in v0.1.
  - **C — binary download:** auto-save on TTY, stream on pipe, `-o` always wins.
  - **A — pipe-guard:** buffer + validate before passing bytes to a shell.
- **Modular sieves** (Go interface, compiled-in) drive Mode A's validation. TOFU
  pinning and heuristic checks are sieves; more can be added over time.

See [`TODO.md`](TODO.md) for the prioritized roadmap.

## Style

- Exit codes: `0` success, `1` runtime error, `2` usage error.
- Errors to stderr, payload to stdout, progress/metrics to stderr.
- Prefer `flag` (stdlib) once we need more than one CLI argument; raw `os.Args`
  for v0.1 since there's exactly one.

## `docs/` (GitHub Pages)

`docs/index.html` serves two purposes from one file: a minimal human landing
page at `https://gocurb.dev/`, and the Go vanity-import resolver via the
`<meta name="go-import">` tag — that tag is what makes
`go install gocurb.dev@latest` find the GitHub repo. Don't remove it.
`docs/CNAME` is GitHub Pages' custom-domain marker.

## Site posture (`gocurb.dev` on GitHub Pages)

GH Pages does not let you set custom response headers on custom domains. The
"Enforce HTTPS" toggle adds an HTTP→HTTPS 301 but does *not* send
`Strict-Transport-Security`, `Content-Security-Policy`, `X-Frame-Options`,
or `X-Content-Type-Options`. Mozilla Observatory rates the site C/50 as a
result; SSL Labs rates it A.

This is cosmetic for our actual risk model. The landing page is one static
HTML file with no JavaScript, no forms, no user input, no third-party
resources, no interactivity. CSP/XFO/XCTO protect against attacks that have
no vector here. HSTS at the browser level is already provided by the `.dev`
TLD preload list.

Don't suggest "fixes" (Cloudflare proxy, moving host) unless the site grows
features that create real attack surface — forms, JS, third-party embeds,
user-generated content. Until then the grade is a checklist artifact, not a
real risk.

## Source of truth

This file and [`TODO.md`](TODO.md) are the source of truth for project direction.
During early iteration, avoid over-polishing user-facing messaging (README copy,
slogans, positioning) — those will evolve as the design firms up.
