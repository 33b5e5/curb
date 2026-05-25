package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

type mode int

const (
	modeAuto     mode = iota
	modeInspect       // B — structured inspection (stream to stdout)
	modeDownload      // C — binary download
	modeScript        // A — pipe-guard (not yet implemented)
)

type config struct {
	outPath     string
	forcedMode  mode
	stdout      io.Writer
	stderr      io.Writer
	stdoutIsTTY bool
}

func main() {
	var (
		outPath       string
		forceInspect  bool
		forceDownload bool
		forceScript   bool
	)
	flag.StringVar(&outPath, "o", "", "write payload to PATH (implies --download)")
	flag.BoolVar(&forceInspect, "inspect", false, "force inspection mode (stream to stdout)")
	flag.BoolVar(&forceDownload, "download", false, "force download mode")
	flag.BoolVar(&forceScript, "script", false, "force pipe-guard mode (not yet implemented)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: curb [flags] <https-url>")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	forced, err := selectMode(forceInspect, forceDownload, forceScript, outPath != "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "curb:", err)
		os.Exit(2)
	}

	cfg := config{
		outPath:     outPath,
		forcedMode:  forced,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		stdoutIsTTY: isTerminal(os.Stdout),
	}
	if err := run(newClient(), cfg, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "curb:", err)
		os.Exit(1)
	}
}

func selectMode(inspect, download, script, hasOut bool) (mode, error) {
	count := 0
	pick := modeAuto
	if inspect {
		count++
		pick = modeInspect
	}
	if download {
		count++
		pick = modeDownload
	}
	if script {
		count++
		pick = modeScript
	}
	if count > 1 {
		return modeAuto, errors.New("--inspect, --download, --script are mutually exclusive")
	}
	if hasOut {
		if pick == modeInspect || pick == modeScript {
			return modeAuto, errors.New("-o is incompatible with --inspect and --script")
		}
		pick = modeDownload
	}
	return pick, nil
}

func newClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: checkRedirect,
	}
}

var checkRedirect = func(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("redirect to non-https URL refused: %s", req.URL.String())
	}
	return nil
}

func run(client *http.Client, cfg config, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return fmt.Errorf("only https:// URLs are supported, got %q", u.Scheme)
	}
	resp, err := client.Get(u.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	m, body, err := resolveMode(cfg.forcedMode, resp)
	if err != nil {
		return err
	}
	switch m {
	case modeInspect:
		_, err := io.Copy(cfg.stdout, body)
		return err
	case modeDownload:
		return download(body, resp, u, cfg)
	case modeScript:
		return errors.New("Mode A (--script) is not yet implemented")
	default:
		return fmt.Errorf("internal: unknown mode %d", m)
	}
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
