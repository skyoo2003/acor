# Security Policy

## Supported Versions

ACOR is pre-1.0. Security patches go to the **latest released minor line only**,
shipped as a new patch release. There is no long-term support branch, and earlier
minor lines are not backported to.

| Version                | Supported |
| ---------------------- | --------- |
| Latest minor line      | ✅        |
| Any earlier minor line | ❌        |

See the [latest release](https://github.com/skyoo2003/acor/releases/latest) for
which version that is, and upgrade to it before reporting a vulnerability so the
report is against supported code.

The whole `v1.x` line — `v1.0.0` through `v1.4.1` — was published in error and is
**retracted**. `v1.4.1` exists only to carry the retractions and retracts itself,
so it is no safer than the rest. None of them receive fixes, and `go get` will
not select them. If you pinned any v1 version, move to the latest v0.x release.

The experimental `acor/server` module publishes no tags of its own; fixes for it
land on `main` and are consumed from there.

## Reporting a Vulnerability

If you discover a security vulnerability within ACOR, please report it responsibly.

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report them privately through [GitHub Security Advisories][security-advisories].

When reporting, please include:

- A description of the vulnerability
- Steps to reproduce the issue
- Potential impact of the vulnerability
- Any suggested fixes (if applicable)

## Response Timeline

We aim to acknowledge vulnerability reports within 48 hours and provide a timeline for fixes based on severity:

- **Critical**: Fix within 7 days
- **High**: Fix within 30 days
- **Medium**: Fix within 90 days
- **Low**: Fix in next release

## Disclosure Policy

We follow a coordinated disclosure process and will work with reporters to:

1. Confirm the vulnerability
2. Develop and test a fix
3. Release a patched version
4. Credit reporters (unless anonymity is requested)

## License

ACOR is licensed under the [Apache License 2.0][apache-license].

[apache-license]: LICENSE

[security-advisories]: https://github.com/skyoo2003/acor/security/advisories/new
