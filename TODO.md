# curb roadmap

Prioritized backlog for what comes after v0.1.
**P0** = next iteration. **P1** = soon. **P2** = eventually.

> **Next up:** custom request headers (P2).

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
