# Security Policy

## Supported versions

The project is pre-1.0. Security fixes land on `main` and are included in the next tagged release.

| Version | Supported |
| --- | --- |
| `main` | Yes |
| Latest release | Yes |
| Older releases | Best effort only |

## Reporting a vulnerability

**Please do not open a public issue for a security problem.**

Use GitHub's private vulnerability reporting:

<https://github.com/kusalkalinga00/go-base-web-scraper/security/advisories/new>

If that is unavailable, email **kusalkalingainfo@gmail.com** with `SECURITY` in the subject.

Include as much of the following as you can:

- affected version, commit, or image tag;
- the deployment shape (Docker Compose, bare binary, proxy enabled, `API_KEY` set or not);
- a reproduction or proof of concept, including the exact request if it is an HTTP issue;
- the impact you believe it has;
- any suggested fix or mitigation.

Do not include live credentials or third-party data in the report. If a secret is involved, assume it is compromised and rotate it.

## What to expect

- Acknowledgement within 7 days, best effort.
- An assessment and a remediation plan after triage.
- Credit in the release notes if you want it.
- Coordinated disclosure: we will agree on a timeline before details are published. Please give us a reasonable window to ship a fix.

## Scope

In scope:

- SSRF guard bypasses, including redirects and DNS tricks that reach internal services or cloud metadata;
- authentication or authorization bypass (`API_KEY`, health bypass, rate-limit bypass);
- path traversal or arbitrary file read/write through PDF image serving, cleanup, or storage paths;
- remote code execution, command injection, or unsafe deserialization;
- denial of service that is cheap for an attacker and expensive for the service (per-request crashes, unbounded allocation);
- exposure of secrets, tokens, or private data through responses, logs, or repository history;
- supply-chain issues in the build or container image.

Out of scope:

- reports that scraping a site violates that site's terms — that is a deployment decision, not a code vulnerability;
- rate limits or blocks applied by third-party websites;
- missing hardening that is already documented in the README's [Known limits](../README.md#known-limits);
- dependency version bumps without a demonstrated vulnerable code path;
- social engineering, physical access, or attacks that require an already-compromised host.

## Deployment hardening

A public deployment should at minimum:

- set `API_KEY` and terminate TLS in front of the service;
- restrict egress to the sites you intend to scrape, or route browser traffic through a controlled proxy;
- keep `POST /api/cleanup` unreachable from untrusted networks;
- run with a low `MAX_WORKERS` and container memory limits;
- rotate proxy and database credentials, and never commit them;
- enable Dependabot alerts and secret scanning in the repository settings.

## Safe harbour

We will not pursue legal action against researchers who report issues in good faith, avoid privacy violations and service disruption, and give us reasonable time to fix the problem before publishing. Test only against your own deployment.
