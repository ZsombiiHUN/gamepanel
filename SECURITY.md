# Security policy

Please report vulnerabilities privately through GitHub's
**Security → Report a vulnerability** form on this repository. Do not open a
public issue.

We aim to acknowledge reports within 3 days. This project is pre-1.0: fixes go into
`main` and ship in the next release, and only the newest release is
supported. Please include `panel version` and `daemon version` output.

In scope: file-access escapes (symlinks, traversal), authentication and session
bugs, container escape or privilege issues, command injection via eggs.
