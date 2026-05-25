package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// tofuSieve implements trust-on-first-use pinning. It records SHA-256 of the
// script body keyed by URL in a text file. Subsequent fetches must match the
// pinned hash, or the sieve blocks.
type tofuSieve struct {
	path     string
	forcePin bool
	stderr   io.Writer
}

func (tofuSieve) Name() string { return "tofu" }

func (t tofuSieve) Evaluate(body []byte, meta SieveMeta) Verdict {
	if meta.URL == nil {
		return Verdict{}
	}
	key := meta.URL.String()
	sum := sha256.Sum256(body)
	cur := hex.EncodeToString(sum[:])

	pinned, err := loadPin(t.path, key)
	if err != nil {
		t.warn("cannot read %s: %v", t.path, err)
		return Verdict{}
	}

	switch {
	case t.forcePin:
		if err := savePin(t.path, key, cur); err != nil {
			t.warn("cannot write %s: %v", t.path, err)
		}
		return Verdict{}
	case pinned == "":
		if err := savePin(t.path, key, cur); err != nil {
			t.warn("cannot write %s: %v", t.path, err)
			return Verdict{}
		}
		t.warn("pinned %s for %s", cur[:12], key)
		return Verdict{}
	case pinned == cur:
		return Verdict{}
	default:
		hint := "if the change is expected, accept the new hash with --pin"
		if meta.URL != nil {
			hint = fmt.Sprintf("if expected: curb --pin --script %s", meta.URL)
		}
		return Verdict{
			Block:  true,
			Reason: fmt.Sprintf("script body changed (pinned %s, got %s)", pinned[:12], cur[:12]),
			Hint:   hint,
		}
	}
}

func (t tofuSieve) warn(format string, args ...any) {
	if t.stderr == nil {
		return
	}
	fmt.Fprintf(t.stderr, "curb: tofu: "+format+"\n", args...)
}

// loadPin returns the pinned hex SHA-256 for url, or "" if the file is missing
// or the URL isn't recorded. File format: one entry per line, "<url> <hex>",
// with '#' comments and blank lines ignored.
func loadPin(path, url string) (string, error) {
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
		if parts[0] == url {
			return parts[1], nil
		}
	}
	return "", sc.Err()
}

// savePin inserts or updates the pin for url. Writes are atomic via temp file
// + rename within the same directory.
func savePin(path, url, hexsum string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var lines []string
	replaced := false
	if existing, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(existing)
		for sc.Scan() {
			line := sc.Text()
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				parts := strings.Fields(trimmed)
				if len(parts) == 2 && parts[0] == url {
					lines = append(lines, url+" "+hexsum)
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
		lines = append(lines, url+" "+hexsum)
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
	return os.Rename(tmp.Name(), path)
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
