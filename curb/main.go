// Curb is a modern, HTTPS-only transport utility written in Go using only
// the standard library. It picks one of three modes per response:
//
//   - inspect: stream textual payloads (JSON, HTML, XML, …) to stdout.
//   - download: save binaries to disk; streams to stdout on a pipe.
//   - vet: buffer the body, run it through sieves (nonempty, heuristic, tofu),
//     emit only on pass.
//
// Mode is chosen from Content-Type by default, with a magic-byte sniff
// fallback. Override with --inspect, --download, or --vet.
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
	modeInspect       // stream to stdout
	modeDownload      // save to disk
	modeVet           // buffer and validate before emitting
)

type config struct {
	outPath     string
	forcedMode  mode
	stdout      io.Writer
	stderr      io.Writer
	stdoutIsTTY bool
	stderrIsTTY bool

	// timeout is the overall request deadline; 0 means no deadline.
	timeout time.Duration

	// --vet options.
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
		forceVet      bool
		forcePin      bool
		noPin         bool
		force         bool
		ipv4Only      bool
		ipv6Only      bool
		showVersion   bool
		timeout       time.Duration
	)
	flag.StringVar(&outPath, "o", "", "write payload to PATH (implies --download)")
	flag.BoolVar(&forceInspect, "inspect", false, "force inspection mode (stream to stdout)")
	flag.BoolVar(&forceDownload, "download", false, "force download mode")
	flag.BoolVar(&forceVet, "vet", false, "force vet mode (buffer and validate before piping)")
	flag.BoolVar(&forcePin, "pin", false, "record current body hash (overrides TOFU mismatch)")
	flag.BoolVar(&noPin, "no-pin", false, "skip the TOFU sieve for this invocation")
	flag.BoolVar(&force, "force", false, "bypass sieve blocks (still warns on stderr)")
	flag.BoolVar(&ipv4Only, "4", false, "force IPv4 resolution")
	flag.BoolVar(&ipv6Only, "6", false, "force IPv6 resolution")
	flag.BoolVar(&showVersion, "version", false, "print version info and exit")
	flag.DurationVar(&timeout, "timeout", 0, "overall request deadline (e.g. 45s, 2m), 0 to disable; default 60s in --vet, none otherwise")
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

	forced, err := selectMode(forceInspect, forceDownload, forceVet, outPath != "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "curb:", err)
		os.Exit(2)
	}
	if err := validateVetFlags(forcePin, noPin, force, forced); err != nil {
		fmt.Fprintln(os.Stderr, "curb:", err)
		os.Exit(2)
	}
	network, err := selectNetwork(ipv4Only, ipv6Only)
	if err != nil {
		fmt.Fprintln(os.Stderr, "curb:", err)
		os.Exit(2)
	}
	if timeout < 0 {
		fmt.Fprintln(os.Stderr, "curb: --timeout must not be negative")
		os.Exit(2)
	}
	timeoutSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "timeout" {
			timeoutSet = true
		}
	})

	cfg := config{
		outPath:     outPath,
		forcedMode:  forced,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		stdoutIsTTY: isTerminal(os.Stdout),
		stderrIsTTY: isTerminal(os.Stderr),
		timeout:     resolveTimeout(timeout, timeoutSet, forced),
		forcePin:    forcePin,
		noPin:       noPin,
		force:       force,
	}
	if err := run(newClient(network), cfg, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "curb:", err)
		os.Exit(1)
	}
}

func selectMode(inspect, download, vet, hasOut bool) (mode, error) {
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
	if vet {
		count++
		pick = modeVet
	}
	if count > 1 {
		return modeAuto, errors.New("--inspect, --download, --vet are mutually exclusive")
	}
	if hasOut {
		if pick == modeInspect || pick == modeVet {
			return modeAuto, errors.New("-o is incompatible with --inspect and --vet")
		}
		pick = modeDownload
	}
	return pick, nil
}

func validateVetFlags(pin, noPin, force bool, forced mode) error {
	if pin && noPin {
		return errors.New("--pin and --no-pin are mutually exclusive")
	}
	if (pin || noPin || force) && forced != modeVet {
		return errors.New("--pin, --no-pin, and --force require --vet")
	}
	return nil
}

// buildSieves assembles the vet-mode sieve chain. Order matters: cheaper
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

// Transport-level stall timeouts bound how long a slow or hostile server can
// hold a connection open at each phase (connect, TLS handshake, waiting for
// response headers) without capping total transfer time, so they are safe for
// streaming inspect/download. The overall body deadline is separate; see
// resolveTimeout.
const (
	dialTimeout           = 30 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	responseHeaderTimeout = 30 * time.Second
	idleConnTimeout       = 90 * time.Second
)

func newClient(network string) *http.Client {
	// Dial through a dialer with a connect timeout. With DialContext unset,
	// http.Transport falls back to a zero net.Dialer that never times out, so we
	// always supply our own. The network string is all that varies: "tcp" lets
	// the resolver choose the IP version, -4/-6 force tcp4/tcp6; the timeout is
	// the same in every case.
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		IdleConnTimeout:       idleConnTimeout,
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}
}

// defaultVetTimeout is the overall request deadline applied in vet mode when the
// user hasn't set --timeout. vet buffers an attacker-influenceable body (capped
// by maxVetBytes), so an endless trickle should fail rather than hang. Streaming
// modes stay uncapped by default so long or open-ended transfers aren't cut off.
// It is set above the 30s response-header timeout so the body-read phase gets its
// own headroom, and kept generous since the cost of cutting off a slow but valid
// fetch outweighs making a (Ctrl-C-able, size-capped) hostile trickle wait longer.
const defaultVetTimeout = 60 * time.Second

// resolveTimeout picks the overall request deadline (0 means no deadline). An
// explicit --timeout wins for every mode; otherwise only vet mode gets a
// default, since inspect and download stream and may run arbitrarily long by
// design.
func resolveTimeout(flagVal time.Duration, set bool, m mode) time.Duration {
	if set {
		return flagVal
	}
	if m == modeVet {
		return defaultVetTimeout
	}
	return 0
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
	ctx := context.Background()
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	m, rc, err := resolveMode(cfg.forcedMode, resp)
	if err != nil {
		return err
	}
	// resp.Body is closed via the deferred Close above; downstream only reads, so
	// an io.Reader is all the consumers need.
	var body io.Reader = rc
	// Nudge toward --vet when a shell-shaped body is streaming to a pipe. Gated
	// to the cases where the body actually goes to stdout: not vet (which is the
	// thing we'd be suggesting), no -o (that writes a file), and stdout not a TTY
	// (a human reading it isn't piping to sh). Stateless and advisory; the body
	// is unchanged. See issue #4.
	if m != modeVet && cfg.outPath == "" && !cfg.stdoutIsTTY {
		var shaped bool
		if shaped, body = looksShellShaped(body, resp.Header.Get("Content-Type")); shaped {
			fmt.Fprintln(cfg.stderr, shellHint)
		}
	}
	switch m {
	case modeInspect:
		_, err := io.Copy(cfg.stdout, body)
		return err
	case modeDownload:
		return download(body, resp, u, cfg)
	case modeVet:
		sieves, err := buildSieves(cfg)
		if err != nil {
			return err
		}
		meta := SieveMeta{URL: u, Status: resp.StatusCode, Header: resp.Header}
		return vet(body, meta, sieves, cfg)
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
