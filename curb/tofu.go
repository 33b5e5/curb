package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// tofuSieve implements trust-on-first-use pinning. It records SHA-256 of the
// body keyed by URL in a text file. Subsequent fetches must match the pinned
// hash, or the sieve blocks.
type tofuSieve struct {
	path     string
	forcePin bool
	stderr   io.Writer
}

func (tofuSieve) Name() string { return "tofu" }

// Evaluate compares the body's hash against the recorded pin and is
// side-effect-free: a first-use or --pin body is reported as passing but is not
// written here. Persisting the pin is deferred to Commit, which vet calls only
// once it has decided to emit the body, so a body another sieve blocks never
// gets pinned.
func (t tofuSieve) Evaluate(body []byte, meta SieveMeta) Verdict {
	if meta.URL == nil {
		return Verdict{}
	}
	pinned, err := loadPin(t.path, tofuKey(meta.URL))
	if err != nil {
		// Fail closed on a genuine read error. loadPin maps a missing file to
		// ("", nil), so first use still pins; any other error blocks.
		return Verdict{
			Block:  true,
			Reason: fmt.Sprintf("cannot read pin file %s: %v", t.path, err),
			Hint:   "fix the file's permissions/path, or bypass pinning with --no-pin",
		}
	}

	switch {
	case t.forcePin, pinned == "", pinned == bodyHash(body):
		// --pin overrides any mismatch; first use and an unchanged hash both pass.
		// Whatever needs writing happens in Commit, after vet decides to emit.
		return Verdict{}
	default:
		hint := fmt.Sprintf("inspect the new body first: curb --inspect %[1]s; if the change is expected, re-pin with: curb --pin --vet %[1]s", meta.URL)
		return Verdict{
			Block:  true,
			Reason: fmt.Sprintf("body changed (pinned %s, got %s)", short(pinned), short(bodyHash(body))),
			Hint:   hint,
		}
	}
}

// Commit records the pin for a body vet has decided to emit. vet runs it only
// after the body clears every sieve (or --force overrides a block), never on a
// withheld body, so a pin always means "curb emitted this body". A first-use or
// --pin body is written here; an unchanged hash is a no-op; a mismatch without
// --pin cannot reach Commit (Evaluate blocks it) and is left untouched.
func (t tofuSieve) Commit(body []byte, meta SieveMeta) {
	if meta.URL == nil {
		return
	}
	key := tofuKey(meta.URL)
	cur := bodyHash(body)

	pinned, err := loadPin(t.path, key)
	if err != nil {
		// Evaluate already blocks on a read error; if --force carried us here
		// anyway, there is nothing safe to record.
		return
	}

	switch {
	case t.forcePin:
		switch {
		case pinned == "":
			t.warn("pinning %s for %s", short(cur), key)
		case pinned == cur:
			// no diff to report
		default:
			t.warn("re-pinning %s: %s -> %s", key, short(pinned), short(cur))
		}
		if err := savePin(t.path, key, cur); err != nil {
			t.warn("cannot write %s: %v", t.path, err)
		}
	case pinned == "":
		if err := savePin(t.path, key, cur); err != nil {
			t.warn("cannot write %s: %v", t.path, err)
			return
		}
		t.warn("pinned %s for %s", short(cur), key)
	}
	// pinned == cur (and not forcePin): already recorded, nothing to do.
}

// bodyHash is the hex SHA-256 used as a pin value.
func bodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// short returns the first 12 characters of a hex sum, or the whole string if it
// is shorter (the stored value is user-editable, so don't assume a length).
func short(s string) string {
	if len(s) < 12 {
		return s
	}
	return s[:12]
}

func (t tofuSieve) warn(format string, args ...any) {
	if t.stderr == nil {
		return
	}
	fmt.Fprintf(t.stderr, "curb: tofu: "+format+"\n", args...)
}

// tofuKey returns the canonical pin key for u: equivalent spellings
// (case-insensitive host, trailing dot, default :443, fragment, userinfo) share
// a single pin. The query string is kept verbatim, since a different query is a
// different resource.
func tofuKey(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	host = strings.TrimSuffix(host, ".")
	port := u.Port()
	if port != "" && port != "443" {
		host = net.JoinHostPort(host, port)
	}
	c := url.URL{
		Scheme:   u.Scheme,
		Host:     host,
		Path:     u.Path,
		RawQuery: u.RawQuery,
	}
	return c.String()
}

// encodeKey percent-encodes a pin key so it is a single whitespace-free token on
// disk, surviving the strings.Fields parse regardless of spaces in the URL.
func encodeKey(s string) string { return url.QueryEscape(s) }

// decodeKey reverses encodeKey.
func decodeKey(s string) (string, error) { return url.QueryUnescape(s) }

// loadPin returns the pinned hex SHA-256 for url, or "" if the file is missing
// or the URL isn't recorded. File format: one entry per line,
// "<percent-encoded-url> <hex>", with '#' comments and blank lines ignored.
func loadPin(path, url string) (string, error) {
	want := encodeKey(url)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == want {
			return parts[1], nil
		}
	}
	return "", sc.Err()
}

// savePin inserts or updates the pin for url. The on-disk key is
// percent-encoded ("<percent-encoded-url> <hex>"). Writes are atomic via temp
// file + rename within the same directory.
func savePin(path, url, hexsum string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	want := encodeKey(url)
	entry := want + " " + hexsum

	var lines []string
	replaced := false
	if existing, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(existing)
		for sc.Scan() {
			line := sc.Text()
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				parts := strings.Fields(trimmed)
				if len(parts) == 2 && parts[0] == want {
					lines = append(lines, entry)
					replaced = true
					continue
				}
			}
			lines = append(lines, line)
		}
		existing.Close()
		if err := sc.Err(); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if !replaced {
		lines = append(lines, entry)
	}

	tmp, err := os.CreateTemp(dir, ".known.tmp-*")
	if err != nil {
		return err
	}
	w := bufio.NewWriter(tmp)
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return err
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// defaultTofuPath resolves the pin-file location: $XDG_CONFIG_HOME/curb/known.txt,
// falling back to ~/.config/curb/known.txt.
func defaultTofuPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "curb", "known.txt"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "curb", "known.txt"), nil
}
