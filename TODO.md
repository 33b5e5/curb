# curb roadmap

## Broader request surface

### Custom request headers (MED)
`-H "Name: value"`, repeatable.
Required for any non-trivial API work (auth, Accept, content negotiation).
Need to decide how callers override curb's own defaults — curl uses
empty-value suppression (`-H "User-Agent:"`), worth matching for least
surprise.

### Request bodies for POST/PUT (MED)
`-X METHOD`, `-d DATA`, `--data-binary DATA`
Pairs with `-H` for real API testing. `-d` should auto-promote to POST when
`-X` is absent (curl-compatible). Support `@file` and `@-` for disk and stdin
sources. `--data-binary` skips curl's CRLF stripping for byte-exact uploads.
Don't default a `Content-Type` — make callers set it via `-H` so nothing is
sent silently.

### Auth helpers (MED)
`--bearer TOKEN`, `-u user:pass`
Both are sugar over `-H "Authorization: …"`, but worth dedicated flags:
`--bearer` for the API-token 90% case, `-u` for HTTP Basic. Consider
`--bearer @file` and/or env-var reads so tokens don't land in shell history.
`-u user:` (no password) could prompt on TTY the way curl does.

## Infrastructure

### Site security headers (LOW).
Mozilla Observatory score of C (50/100) due to missing CSP, XFO, XCTO.
Cosmetic for our static page; see AGENTS.md → *Site posture*.
