package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: curb <https-url>")
		os.Exit(2)
	}
	if err := run(newClient(), os.Stdout, os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "curb:", err)
		os.Exit(1)
	}
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

func run(client *http.Client, out io.Writer, raw string) error {
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
	_, err = io.Copy(out, resp.Body)
	return err
}
