# curb roadmap

Prioritized backlog for what comes after v0.1.

## Broader request surface (MED)

- Custom request headers (`-H`).
- Request bodies for POST/PUT (`-X`, `-d`, `--data-binary`).
- Auth helpers (`--bearer TOKEN`, `-u user:pass`).
- **`-4` / `-6` flags.** Force IPv4 or IPv6 resolution. Default (no flag) stays as-is.
- **`--version` flag.** Print version (and commit, via `runtime/debug.ReadBuildInfo`) and exit.
- Progress bar for downloads (Mode C polish).

## Infra (LOW)

- **Site security headers (low priority).** Observatory C/50 (CSP, XFO, XCTO
  missing). Cosmetic for our static page; see AGENTS.md → *Site posture*.
