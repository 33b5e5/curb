# curb roadmap

Prioritized backlog for what comes after v0.1.
**P0** = next iteration. **P1** = soon. **P2** = eventually.

> **Next up:** Mode C (binary download).

## P0 — beyond v0.1

- **Mode dispatch skeleton.** Detect `Content-Type` and fall back to magic-byte
  sniffing when the header is missing or `application/octet-stream`. Introduce an
  internal `mode` enum (B is today's default). Add `--script`, `--inspect`,
  `--download` flags to force a mode.
- **Mode C: binary download.** Default behavior with no `-o`:
  - stdout is a TTY → save to CWD, filename from URL path or `Content-Disposition`,
    refuse to clobber an existing file.
  - stdout is piped/redirected → stream to stdout (so `curb URL | tar xz` works).
  - `-o PATH` always wins.
  - Progress metrics on stderr.
- **`Sieve` interface and harness.** Define the Go interface (name, evaluate, verdict).
  Wire it into the Mode A code path even if only a trivial stub sieve ships first.

## P1 — making Mode A real

- **TOFU sieve.** SHA-256 of the script body, persisted at
  `~/.config/curb/known.txt` keyed by URL. Warn on change; `--pin` / `--no-pin`
  flags to control behavior.
- **Heuristic sieve.** Pattern checks for `sudo` without context, `rm -rf /`,
  nested `curl|wget … | bash`, suspicious `base64 -d | sh`. Honest framing in
  the output: this is a smell test, not a guarantee.
- **Mode A block UX.** When a sieve blocks, the stderr report ends with concrete
  next steps: `curb --inspect URL` (preview the script) and `curb --force URL`
  (override).

## P2 — broader request surface

- Custom request headers (`-H`).
- Request bodies for POST/PUT (`-X`, `-d`, `--data-binary`).
- Auth helpers (`--bearer TOKEN`, `-u user:pass`).
- Progress bar for downloads (Mode C polish).

## Infra

- ~~Purchase `gocurb.dev`, point DNS at GitHub Pages.~~ Done.
- ~~Let's Encrypt SAN cert covering apex + `www`.~~ Done.
- ~~Enable **Enforce HTTPS** in Repo → Settings → Pages.~~ Done.
- **Site security headers (low priority).** Observatory C/50 (CSP, XFO, XCTO
  missing). Cosmetic for our static page; see CLAUDE.md → *Site posture*.
