# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's
[Report a vulnerability](https://github.com/ZsombiiHUN/gamepanel/security/advisories/new)
form (Security → Advisories). Do not open a public issue, pull request or
discussion about a vulnerability before it is fixed.

Include what you can of:

- `panel version` and `daemon version` output
- the steps to reproduce, or a proof of concept
- what an attacker gains, and what access they need first

## What happens next

- We acknowledge the report within 3 days.
- We confirm or rule out the issue, and tell you which, within 14 days.
- We aim to release a fix within 90 days of the report, sooner for severe issues.
- We publish a GitHub security advisory (with a CVE where it applies) when the
  fix ships, and credit you unless you ask us not to.

This is coordinated disclosure: please give us until the fix is released, or 90
days, whichever comes first, before you disclose the vulnerability publicly. If
you need longer or shorter, say so in the report and we will agree a date.

## Supported versions

GamePanel is pre-1.0. Fixes go into `main` and ship in the next release, and
only the newest release is supported.

## Scope

In scope:

- file-access escapes (symlinks, path traversal) out of a server's directory
- authentication, session and permission bugs in the panel or daemon API
- container escape or privilege escalation through the daemon
- command injection through eggs, startup commands or variables
- release integrity: binaries, images or install.sh not matching the source

Out of scope: denial of service by an authenticated administrator, issues that
need root on the host already, and vulnerabilities in the game servers
themselves.

## Verifying releases

From v0.1.1 on, release checksums are signed with [Sigstore](https://www.sigstore.dev/) and
the binaries carry GitHub build provenance. See
[Verifying a release](https://zsombiihun.github.io/gamepanel/panel/installing#verifying-a-release).
