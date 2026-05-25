package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintVersion(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf)
	out := buf.String()
	if !strings.HasPrefix(out, "curb ") {
		t.Errorf("output should start with 'curb ', got %q", out)
	}
	if !strings.Contains(out, "built with go") {
		t.Errorf("output should include Go version line, got %q", out)
	}
}
