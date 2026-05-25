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

func download(body io.Reader, resp *http.Response, u *url.URL, cfg config) error {
	if cfg.outPath != "" {
		return saveTo(body, cfg.outPath, true, cfg.stderr)
	}
	if !cfg.stdoutIsTTY {
		return stream(body, cfg.stdout, cfg.stderr)
	}
	name, err := deriveFilename(resp, u)
	if err != nil {
		return err
	}
	return saveTo(body, name, false, cfg.stderr)
}

func saveTo(body io.Reader, dst string, allowOverwrite bool, stderr io.Writer) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !allowOverwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(dst, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; pass -o PATH to overwrite", dst)
		}
		return err
	}
	start := time.Now()
	n, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Fprintf(stderr, "curb: saved %s (%s in %s)\n", dst, humanBytes(n), time.Since(start).Round(time.Millisecond))
	return nil
}

func stream(body io.Reader, stdout, stderr io.Writer) error {
	start := time.Now()
	n, err := io.Copy(stdout, body)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "curb: %s in %s\n", humanBytes(n), time.Since(start).Round(time.Millisecond))
	return nil
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
