# curb release process

Steps to cut a new tagged release. Run from the repo root. Replace `vX.Y.Z` with the target version. The `v` prefix is
required (Go's module system ignores tags without it).

## One-time setup

Install the release tooling. `gh` and `go` are assumed; `goreleaser` is needed for the artifact build (Phase 1 onward):

```sh
brew install --cask goreleaser/tap/goreleaser
```

## 1. Pre-flight

Working tree clean, HEAD pushed, gh authed to the right repo:

```sh
git status
git log -1 --oneline && git rev-parse @ @{u}
gh auth status && gh repo view --json nameWithOwner
```

## 2. Tag locally

Local-only, reversible with `git tag -d vX.Y.Z`:

```sh
git tag -a vX.Y.Z -m "vX.Y.Z"
```

## 3. Push the tag

Hard-to-reverse: proxy.golang.org caches on first lookup, so the version is effectively permanent in the public Go
ecosystem the moment anyone fetches it. Treat this as the point of no return.

```sh
git push origin vX.Y.Z
```

## 4. Build and publish the release

goreleaser reads the tag you just pushed (it does not create tags), cross-compiles the four targets, archives them with
`LICENSE` and `README.md`, writes checksums, creates the GitHub release with the binaries attached, and pushes the
updated Homebrew cask to the `33b5e5/homebrew-tap` repo. It needs a GitHub token and does *not* reuse `gh`'s stored
auth, so bridge it in:

```sh
export GITHUB_TOKEN=$(gh auth token)
goreleaser release --clean
```

`.goreleaser.yml` sets `draft: true`, so the result is an unpublished draft. Review it on github.com, replace the
auto-generated notes (a raw commit list) with real notes, then click Publish.

### What the draft gates

Two independent distribution channels exist, and the draft holds back only one:

- **GitHub release (binaries + notes):** gated by the draft. Nothing is public until you click Publish, and the
  pre-filled notes are a raw commit dump meant to be overwritten in the GitHub UI first.
- **`go install` and pkg.go.dev:** *not* gated. Both resolve from the pushed git tag via proxy.golang.org, so
  `go install gocurb.dev/curb@vX.Y.Z` and the pkg.go.dev listing go live the moment step 3 completes, regardless of the
  draft. pkg.go.dev renders only source-derived content (synopsis, godoc, version list) and never reads the GitHub
  release notes, so nothing written there can affect the listing.
- **Homebrew cask:** the cask is pushed to the tap during the release run, but its `url`s point at the release assets,
  so `brew install --cask 33b5e5/tap/curb` only resolves once you publish the draft.

## 5. Verify

End-to-end check via the module proxy:

```sh
go install gocurb.dev/curb@vX.Y.Z && ~/go/bin/curb --version
```

Should print `curb vX.Y.Z` and the Go toolchain that built it. Proxy-sourced builds don't include a `commit` line —
that's expected (no `.git` in the build environment); the version itself is the identity.

Confirm the published cask too:

```sh
brew info --cask 33b5e5/tap/curb
```

Should report the new version, sourced from the tap repo.

## Dry-run (testing `.goreleaser.yml` changes)

`--snapshot` does not work with `gomod.proxy: true` (snapshot disables the proxy, then mishandles the module path). To
validate a config change, dry-run against an already-published version instead. This builds into `dist/` and writes the
cask there for inspection, with no remote side effects:

```sh
GORELEASER_CURRENT_TAG=vX.Y.Z goreleaser release --skip=validate,publish --clean
```

## Versioning (pre-1.0)

Bump the patch (`v0.0.2`) for fixes, the minor (`v0.1.0`) for new features. Anything pre-1.0 is allowed to break; bump
the major (`v1.0.0`) when the CLI surface stabilizes.
