# curb roadmap

Prioritized backlog for what comes after v0.1.
**P0** = next iteration. **P1** = soon. **P2** = eventually.

> **Next up:** `Sieve` interface and harness.

## P0 — beyond v0.1

- ~~**Mode dispatch skeleton.**~~ Done. Internal `mode` enum, `Content-Type` →
  `http.DetectContentType` sniff fallback, `--inspect`/`--download`/`--script`
  force flags.
- ~~**Mode C: binary download.**~~ Done. `-o` overwrites; piped stdout streams;
  TTY auto-saves to CWD with filename from `Content-Disposition` or URL path,
  refusing to clobber. Summary metrics on stderr.
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
