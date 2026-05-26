# curb

A modern, HTTPS-only transport utility written in Go, using only the standard library.

## What curb is

A small, focused tool for HTTPS transport at the command line, built around the shapes of work modern developers
actually do: hitting JSON APIs, downloading release artifacts, and (carefully) piping installers to shells.

curb picks the right behavior automatically based on two things: what kind of response comes back, and whether the
output is going to a human or a pipe. The result is fewer flags to remember and sensible defaults that match the
situation.

Two constraints shape everything:

- **HTTPS only.** No other protocols, no plaintext fallback.
- **Zero runtime dependencies.** Pure Go standard library.

## Modes

There are currently 3 supported modes; curb automatically picks based on `Content-Type` or by sniffing the first few
bytes.

The mode can be forced via the following flags:

- **`--inspect`** stream textual payloads (JSON, HTML, XML, ...) to stdout. The default for anything that looks like
  text.
- **`--download`** save binaries to disk; stream to stdout on a pipe; `-o PATH` always takes precedence. The default for
  anything that doesn't look like text.
- **`--vet`** buffer the body, run it through security sieves, emit only on pass. Opt-in; intended for the `curl | sh`
  use case.

The mode choice is the heart of curb: instead of remembering `-o` versus no flag, the tool does the right thing for
what's in front of it.

## Sieves

`--vet` runs the body through a chain of checks before any bytes reach a shell:

- **`nonempty`** refuses empty bodies (e.g. HTTP 204) that would otherwise pipe silently.
- **`heuristic`** pattern smell-tests (`rm -rf /`, fetch-pipe-shell, base64-pipe-shell, `sudo sh -c`). Friction layer,
  not a guarantee.
- **`tofu`** trust-on-first-use SHA-256 pinning, persisted at `~/.config/curb/known.txt`. `--pin` to accept a change,
  `--no-pin` to skip, `--force` to override any sieve once.

## What curb is not

curb is **not** a curl replacement. curl is a masterpiece of plumbing that powers huge swaths of the internet,
maintained under difficult conditions by people doing extraordinary work. curb deliberately covers a small slice of the
same territory with modern defaults and a tighter focus; it has no ambition to match curl's protocol breadth or feature
surface.

curb is also not a wget-style downloader. It will save a file when that's clearly what you want, but it does not
recurse, mirror, resume across runs, or retry indefinitely.

## Install

```sh
go install gocurb.dev/curb@latest
```

## License

GPLv3. See [`LICENSE`](LICENSE).
