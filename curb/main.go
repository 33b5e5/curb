// Curb is a modern, HTTPS-only transport utility written in Go using only
// the standard library. It dispatches each response to one of three modes:
//
//   - inspect: stream textual payloads (JSON, HTML, XML, …) to stdout.
//   - download: save binaries to disk; streams to stdout on a pipe.
//   - script: pipe-guard. Buffer the body, run it through sieves
//     (nonempty, heuristic, tofu), emit only on pass.
//
// Mode is chosen from Content-Type by default, with a magic-byte sniff
// fallback. Override with --inspect, --download, or --script.
//
// See https://gocurb.dev for full documentation.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

type mode int

const (
	modeAuto     mode = iota
	modeInspect       // structured inspection (stream to stdout)
	modeDownload      // binary download
	modeScript        // pipe-guard
)

type config struct {
	outPath     string
	forcedMode  mode
	stdout      io.Writer
	stderr      io.Writer
	stdoutIsTTY bool
	stderrIsTTY bool

	// --script options.
	forcePin bool
	noPin    bool
	force    bool
	tofuPath string // override pin-file location; tests set this to a tempdir
}

func main() {
	var (
		outPath       string
		forceInspect  bool
		forceDownload bool
		forceScript   bool
		forcePin      bool
		noPin         bool
		force         bool
		ipv4Only      bool
		ipv6Only      bool
		showVersion   bool
	)
	flag.StringVar(&outPath, "o", "", "write payload to PATH (implies --download)")
	flag.BoolVar(&forceInspect, "inspect", false, "force inspection mode (stream to stdout)")
	flag.BoolVar(&forceDownload, "download", false, "force download mode")
	flag.BoolVar(&forceScript, "script", false, "force pipe-guard mode")
	flag.BoolVar(&forcePin, "pin", false, "record current script hash (overrides TOFU mismatch)")
	flag.BoolVar(&noPin, "no-pin", false, "skip the TOFU sieve for this invocation")
	flag.BoolVar(&force, "force", false, "bypass sieve blocks (still warns on stderr)")
	flag.BoolVar(&ipv4Only, "4", false, "force IPv4 resolution")
	flag.BoolVar(&ipv6Only, "6", false, "force IPv6 resolution")
	flag.BoolVar(&showVersion, "version", false, "print version info and exit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: curb [flags] <https-url>")
		flag.PrintDefaults()
	}
	flag.Parse()

	if showVersion {
		printVersion(os.Stdout)
		return
	}

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	forced, err := selectMode(forceInspect, forceDownload, forceScript, outPath != "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "curb:", err)
		os.Exit(2)
	}
	if err := validateScriptFlags(forcePin, noPin, force, forced); err != nil {
		fmt.Fprintln(os.Stderr, "curb:", err)
		os.Exit(2)
	}
	network, err := selectNetwork(ipv4Only, ipv6Only)
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
		stderrIsTTY: isTerminal(os.Stderr),
		forcePin:    forcePin,
		noPin:       noPin,
		force:       force,
	}
	if err := run(newClient(network), cfg, flag.Arg(0)); err != nil {
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

func validateScriptFlags(pin, noPin, force bool, forced mode) error {
	if pin && noPin {
		return errors.New("--pin and --no-pin are mutually exclusive")
	}
	if (pin || noPin || force) && forced != modeScript {
		return errors.New("--pin, --no-pin, and --force require --script")
	}
	return nil
}

// buildSieves assembles the script-mode sieve chain. Order matters: cheaper
// checks run first so we don't hash an empty body, etc.
func buildSieves(cfg config) ([]Sieve, error) {
	sieves := []Sieve{nonemptySieve{}, heuristicSieve{}}
	if cfg.noPin {
		return sieves, nil
	}
	path := cfg.tofuPath
	if path == "" {
		p, err := defaultTofuPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	sieves = append(sieves, tofuSieve{
		path:     path,
		forcePin: cfg.forcePin,
		stderr:   cfg.stderr,
	})
	return sieves, nil
}

func newClient(network string) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if network != "tcp" {
		// Mirror net/http's default dialer timings; only the network is forced.
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		transport.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		}
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}
}

// selectNetwork resolves -4 / -6 into the network string passed to net.Dialer.
// Returns "tcp" when neither is set (let the resolver pick).
func selectNetwork(v4, v6 bool) (string, error) {
	if v4 && v6 {
		return "", errors.New("-4 and -6 are mutually exclusive")
	}
	switch {
	case v4:
		return "tcp4", nil
	case v6:
		return "tcp6", nil
	default:
		return "tcp", nil
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
		sieves, err := buildSieves(cfg)
		if err != nil {
			return err
		}
		meta := SieveMeta{URL: u, Status: resp.StatusCode, Header: resp.Header}
		return script(body, meta, sieves, cfg)
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
