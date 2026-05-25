# curb

A modern, HTTPS-only transport utility written in Go, using only the standard library.

curb is in early development; the behavior and interface are still evolving.

## What curb is

A small, focused tool for the common `curl`/`wget` workflows used in modern shell
scripting and operations work. It targets a narrow problem space:

- **HTTPS only.** No other protocols, no plaintext fallback.
- **Zero runtime dependencies.** Pure Go standard library.

## What curb is not

curb is **not** a curl replacement. curl is a masterpiece of plumbing that powers
huge swaths of the internet, maintained under difficult conditions by people doing
extraordinary work. curb deliberately covers a small slice of the same territory
with modern defaults and a tighter focus; it has no ambition to match curl's
protocol breadth or feature surface.

## Install

```sh
go install gocurb.dev/curb@latest
```

## License

GPLv3. See [`LICENSE`](LICENSE).
