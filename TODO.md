# curb roadmap

Prioritized backlog for what comes after v0.1.
**P0** = next iteration. **P1** = soon. **P2** = eventually.

> **Next up:** TOFU sieve (P1).

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
- **Per-`Verdict` next-step hints.** Each `Verdict` carries its own remedy so
  the block report can suggest the right action per sieve (TOFU drift →
  `--inspect` / `--force`; heuristic → `--inspect`; nonempty → check the URL).
  Today's harness suggests no hint at all, which is honest but not actionable.
- **Widen `SieveMeta`.** Pass HTTP status and useful response headers so sieves
  can produce diagnostic reasons (e.g. nonempty saying "HTTP 204 No Content"
  instead of just "body is empty").

## P2 — broader request surface

- Custom request headers (`-H`).
- Request bodies for POST/PUT (`-X`, `-d`, `--data-binary`).
- Auth helpers (`--bearer TOKEN`, `-u user:pass`).
- **`-4` / `-6` flags.** Force IPv4 or IPv6 resolution. Default (no flag) stays as-is.
- **`--version` flag.** Print version (and commit, via `runtime/debug.ReadBuildInfo`) and exit.
- Progress bar for downloads (Mode C polish).

## Infra

- **Site security headers (low priority).** Observatory C/50 (CSP, XFO, XCTO
  missing). Cosmetic for our static page; see AGENTS.md → *Site posture*.
