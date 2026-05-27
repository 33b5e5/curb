# curb release process

Steps to cut a new tagged release. Run from the repo root. Replace `vX.Y.Z` with the target version. The `v` prefix is
required (Go's module system ignores tags without it).

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

## 4. Create the GitHub Release

```sh
gh release create vX.Y.Z --title "vX.Y.Z" --notes "Release notes here."
```

Alternatives: `--notes-file path/to/notes.md` reads from disk; omit `--notes` to open `$EDITOR`. `--generate-notes` is
also available but only useful with a PR-based workflow (it summarizes merged PRs in the compare range; on a
direct-to-`main` history it just emits a bare compare link). Pair any of these with `--draft` to stage the release for
review before publishing.

## 5. Verify

End-to-end check via the module proxy:

```sh
go install gocurb.dev/curb@vX.Y.Z && ~/go/bin/curb --version
```

Should print `curb vX.Y.Z` and the Go toolchain that built it. Proxy-sourced builds don't include a `commit` line —
that's expected (no `.git` in the build environment); the version itself is the identity.

## Versioning (pre-1.0)

Bump the patch (`v0.0.2`) for fixes, the minor (`v0.1.0`) for new features. Anything pre-1.0 is allowed to break; bump
the major (`v1.0.0`) when the CLI surface stabilizes.
