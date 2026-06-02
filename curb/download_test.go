package main

import (
	"net/http"
	"net/url"
	"testing"
)

func TestHasShellShebang(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"#!/bin/sh\necho hi", true},
		{"#!/usr/bin/env bash\n", true},
		{"#! /bin/sh", true},
		{"#!/usr/bin/env zsh\nx", true},
		{"#!/usr/bin/python3\nprint()", false}, // shebang, but not a shell
		{"echo hi\n", false},
		{"", false},
		{"#", false},
	}
	for _, c := range cases {
		if got := hasShellShebang([]byte(c.in)); got != c.want {
			t.Errorf("hasShellShebang(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSafeBasename(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"file.bin", "file.bin", false},
		{"../escape", "escape", false}, // filepath.Base strips ../
		{"/etc/passwd", "passwd", false},
		{"..", "", true},
		{".", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := safeBasename(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%q: err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("%q: got=%q want=%q", c.in, got, c.want)
		}
	}
}

func TestDeriveFilename(t *testing.T) {
	cases := []struct {
		name    string
		cd      string // Content-Disposition header; "" omits it
		rawURL  string
		want    string
		wantErr bool
	}{
		{"content-disposition wins over the URL", `attachment; filename="pkg.tar.gz"`, "https://x.example/ignored", "pkg.tar.gz", false},
		{"falls back to the URL path", "", "https://x.example/dl/app.bin", "app.bin", false},
		{"root path has no usable name", "", "https://x.example/", "", true},
		{"bare host has no usable name", "", "https://x.example", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			if c.cd != "" {
				resp.Header.Set("Content-Disposition", c.cd)
			}
			u, err := url.Parse(c.rawURL)
			if err != nil {
				t.Fatal(err)
			}
			got, err := deriveFilename(resp, u)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if err == nil && got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestIsTextual(t *testing.T) {
	textual := []string{
		"text/plain",                        // text/ prefix
		"application/json",                  // explicit allow-list entry
		"application/x-www-form-urlencoded", // less-obvious allow-list entry
		"application/foo+json",              // +json suffix (not itself listed)
		"application/atom+xml",              // +xml suffix
		"application/foo+yaml",              // +yaml suffix
	}
	binary := []string{
		"application/octet-stream", "image/png", "video/mp4",
		"application/zip", "application/x-tar", "",
	}
	for _, mt := range textual {
		if !isTextual(mt) {
			t.Errorf("%q: want textual", mt)
		}
	}
	for _, mt := range binary {
		if isTextual(mt) {
			t.Errorf("%q: want binary", mt)
		}
	}
}
