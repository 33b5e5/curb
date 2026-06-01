package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func resolveMode(forced mode, resp *http.Response) (mode, io.ReadCloser, error) {
	if forced != modeAuto {
		return forced, resp.Body, nil
	}
	ct := resp.Header.Get("Content-Type")
	mt, _, _ := mime.ParseMediaType(ct)
	if mt != "" && mt != "application/octet-stream" {
		return classify(mt), resp.Body, nil
	}
	buf := make([]byte, 512)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, nil, err
	}
	buf = buf[:n]
	sniffed, _, _ := mime.ParseMediaType(http.DetectContentType(buf))
	body := struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(buf), resp.Body), resp.Body}
	return classify(sniffed), body, nil
}

func classify(mt string) mode {
	if isTextual(mt) {
		return modeInspect
	}
	return modeDownload
}

func isTextual(mt string) bool {
	if strings.HasPrefix(mt, "text/") {
		return true
	}
	if strings.HasSuffix(mt, "+json") || strings.HasSuffix(mt, "+xml") || strings.HasSuffix(mt, "+yaml") {
		return true
	}
	switch mt {
	case "application/json", "application/xml",
		"application/yaml", "application/x-yaml",
		"application/javascript", "application/x-javascript",
		"application/ld+json", "application/x-www-form-urlencoded":
		return true
	}
	return false
}

// shellHint is the one-line stderr nudge emitted when a shell-shaped body is
// streamed to a pipe without --vet. It is advisory only: the body still streams
// unchanged. See issue #4 for the design rationale (hint, never auto-vet).
const shellHint = "curb: looks like a shell script piped to another process; consider --vet to validate before piping"

// shellContentTypes are the media types that mark a body as a shell script by
// declaration, before we fall back to sniffing for a shebang.
var shellContentTypes = map[string]bool{
	"text/x-shellscript":        true,
	"application/x-sh":          true,
	"application/x-shellscript": true,
}

// shebangPeek is how many leading bytes we read to look for a shell shebang;
// comfortably covers "#!/usr/bin/env bash" and similar.
const shebangPeek = 64

// looksShellShaped reports whether body looks like a shell script, by
// Content-Type or by a shell shebang on the first line. It only knows "this is
// the shape of thing that often gets piped to sh," not that anything is. To
// check the shebang it may read the first few bytes of body, so it returns a
// reader that replays them; callers must use the returned reader, not the
// original.
func looksShellShaped(body io.Reader, ct string) (bool, io.Reader) {
	if mt, _, _ := mime.ParseMediaType(ct); shellContentTypes[mt] {
		return true, body
	}
	peek := make([]byte, shebangPeek)
	n, _ := io.ReadFull(body, peek)
	peek = peek[:n]
	rewound := io.MultiReader(bytes.NewReader(peek), body)
	return hasShellShebang(peek), rewound
}

// hasShellShebang reports whether b begins with a "#!" interpreter line that
// names a shell. The substring "sh" on the shebang line covers sh, bash, dash,
// zsh, ksh, and friends without enumerating them.
func hasShellShebang(b []byte) bool {
	if !bytes.HasPrefix(b, []byte("#!")) {
		return false
	}
	line := b
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		line = b[:i]
	}
	return bytes.Contains(line, []byte("sh"))
}

func download(body io.Reader, resp *http.Response, u *url.URL, cfg config) error {
	if cfg.outPath != "" {
		return saveTo(body, cfg.outPath, resp.ContentLength, true, cfg)
	}
	if !cfg.stdoutIsTTY {
		return stream(body, resp.ContentLength, cfg)
	}
	name, err := deriveFilename(resp, u)
	if err != nil {
		return err
	}
	return saveTo(body, name, resp.ContentLength, false, cfg)
}

// saveTo writes body to dst via a temp file in the same directory, renaming
// into place only after a clean copy. A failed transfer therefore leaves dst
// untouched: no truncated artifact, and any pre-existing file survives until
// the new one is complete. This mirrors the temp-file + rename in savePin.
func saveTo(body io.Reader, dst string, total int64, allowOverwrite bool, cfg config) error {
	if !allowOverwrite {
		// O_EXCL on the final path used to enforce this atomically; with the
		// temp-file approach we check up front and rely on the small race
		// window being benign for a single-user fetch.
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("%s already exists; pass -o PATH to overwrite", dst)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".curb.tmp-*")
	if err != nil {
		return err
	}
	// Remove the temp file on every failure path; a successful rename makes
	// this a harmless no-op.
	defer os.Remove(tmp.Name())

	start := time.Now()
	src, bar := withProgress(body, "curb: downloading "+dst, total, cfg)
	n, copyErr := io.Copy(tmp, src)
	if bar != nil {
		bar.finish()
	}
	closeErr := tmp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	// CreateTemp makes the file 0600; match the 0644 the direct open used.
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return err
	}
	fmt.Fprintf(cfg.stderr, "curb: saved %s (%s in %s)\n", dst, humanBytes(n), time.Since(start).Round(time.Millisecond))
	return nil
}

func stream(body io.Reader, total int64, cfg config) error {
	start := time.Now()
	src, bar := withProgress(body, "curb: streaming", total, cfg)
	n, err := io.Copy(cfg.stdout, src)
	if bar != nil {
		bar.finish()
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(cfg.stderr, "curb: %s in %s\n", humanBytes(n), time.Since(start).Round(time.Millisecond))
	return nil
}

// withProgress wraps body with a progress bar when stderr is a TTY.
// Returns the original reader and a nil bar otherwise so callers stay simple.
func withProgress(body io.Reader, label string, total int64, cfg config) (io.Reader, *progressBar) {
	if !cfg.stderrIsTTY {
		return body, nil
	}
	bar := newProgressBar(cfg.stderr, label, total)
	return bar.wrap(body), bar
}

func deriveFilename(resp *http.Response, u *url.URL) (string, error) {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn, ok := params["filename"]; ok && fn != "" {
				return safeBasename(fn)
			}
		}
	}
	base := path.Base(u.Path)
	if base == "" || base == "/" || base == "." {
		return "", errors.New("could not derive filename from URL; pass -o PATH")
	}
	return safeBasename(base)
}

func safeBasename(name string) (string, error) {
	cleaned := filepath.Base(name)
	if cleaned == "" || cleaned == "." || cleaned == ".." || cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("server-supplied filename %q is unsafe; pass -o PATH", name)
	}
	return cleaned, nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
