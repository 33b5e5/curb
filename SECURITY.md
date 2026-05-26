# Security Policy

curb is in early development. We welcome security reports and concerns.

## Reporting

Please report vulnerabilities privately via GitHub Security Advisories:

https://github.com/33b5e5/curb/security/advisories/new

We aim to acknowledge reports within a week. Timelines depend on severity 
and complexity; coordinated disclosure is appreciated.

## Supported versions

Only the most recent tagged release receives security updates.

## Scope

**In scope:**

- Non-HTTPS transport, TLS downgrade, or weakened cipher selection.
- Path traversal in download filenames or the TOFU pin file.
- Memory or process exploitation in the curb binary itself.

**Out of scope:**

- Sieve bypasses (`heuristic`, `nonempty`, `tofu`). These are friction
  layers and smell tests — false negatives are expected by design. See
  the [README](README.md) for honest framing on what sieves do and don't
  guarantee.
- Issues that require the attacker to already have the same user
  privileges as the curb invocation (e.g., the ability to write the
  user's own config files or replace the binary). Cross-user attacks
  and privilege-escalation paths remain in scope.

These lists are broad examples, not exhaustive boundaries. If you're
unsure whether something qualifies, please report it. We'd rather hear
about a non-issue than miss a real one.
