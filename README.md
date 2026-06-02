# curb

A modern HTTPS-only alternative to curl and wget, written in Go using only the standard library.

```sh
curb https://api.example.com/users          # streams JSON to stdout
curb https://example.com/release.tar.gz     # saves to disk
curb --vet https://get.example.com/sh | sh  # vets before piping
```

curb is built around the shapes of work modern developers actually do: hitting JSON APIs, downloading release artifacts,
and (carefully) piping installers to shells.

curb picks the right behavior automatically based on what the server returns and whether the output is going to a human
or a pipe. Fewer flags to memorize, sensible defaults that match the situation.

## Modes

Three modes, picked automatically from `Content-Type` or by sniffing the first few bytes. Override with a flag:

- **`--inspect`** stream textual payloads (JSON, HTML, XML, ...) to stdout. The default for anything that looks like
  text.
- **`--download`** save binaries to disk; stream to stdout on a pipe; `-o PATH` always takes precedence. The default for
  anything that doesn't look like text.
- **`--vet`** buffer the body, run it through security sieves, emit only on pass. Opt-in; intended for the `curl | sh`
  use case.

## Sieves

`--vet` runs the body through a chain of checks before any bytes reach a shell:

- **`nonempty`** refuses empty bodies (e.g. HTTP 204) that would otherwise pipe silently.
- **`heuristic`** pattern smell-tests (`rm -rf /`, fetch-pipe-shell, base64-pipe-shell, `sudo sh -c`,
  `eval "$(curl …)"`). Friction layer, not a guarantee.
- **`tofu`** trust-on-first-use SHA-256 pinning, persisted at `~/.config/curb/known.txt`. `--pin` to accept a change,
  `--no-pin` to skip, `--force` to override any sieve once.

## Foundations

- **Memory-safe runtime.** Go. The memory-unsafe failure modes that bite C-based transports don't apply.
- **Standard library only.** The attack surface is whatever ships with the Go toolchain, plus the few hundred lines of
  curb itself.
- **HTTPS with no escape hatch.** No plaintext fallback, no `http://`, no carveouts (not even `localhost`).
- **Opt-in friction on top.** `--vet` adds the sieves above for the `curl | sh` case.

## Scope

curb covers a small slice of what curl and wget cover, with modern defaults and a tighter focus. No protocol breadth, no
recursive mirroring, no resume-across-runs, no retry-forever. Different tools for different jobs.

## Install

Homebrew (macOS):

```sh
brew install --cask 33b5e5/tap/curb
```

Go 1.23+:

```sh
go install gocurb.dev/curb@latest
```

Prebuilt binaries for macOS and Linux (amd64 and arm64) are attached to each
[release](https://github.com/33b5e5/curb/releases).

## License

GPLv3. See [`LICENSE`](LICENSE).
