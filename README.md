# curb

A modern, HTTPS-only transport utility written in Go, using only the standard library.

## What curb is

A small, focused tool for the common `curl`/`wget` workflows used in modern shell
scripting and operations work. It targets a narrow problem space:

- **HTTPS only.** No other protocols, no plaintext fallback.
- **Zero runtime dependencies.** Pure Go standard library.

## Modes

There are currently 3 supported modes; curb automatically picks based on `Content-Type` or by sniffing the first few bytes.

The mode can be forced via the following flags:

- **`--inspect`** — stream textual payloads (JSON, HTML, XML, …) to stdout.
- **`--download`** — save binaries to disk; streams to stdout on a pipe. `-o PATH` takes precedence.
- **`--script`** — pipe-guard: buffer the body, run it through security sieves, emit only on pass.

## Sieves

`--script` runs the body through a chain of checks before any bytes reach a shell:

- **`nonempty`** — refuses empty bodies (e.g. HTTP 204) that would otherwise pipe silently.
- **`heuristic`** — pattern smell-tests (`rm -rf /`, fetch-pipe-shell, base64-pipe-shell, `sudo sh -c`). Friction layer, not a guarantee.
- **`tofu`** — trust-on-first-use SHA-256 pinning, persisted at `~/.config/curb/known.txt`. `--pin` to accept a change, `--no-pin` to skip, `--force` to override any sieve once.

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
