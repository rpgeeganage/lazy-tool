# Security Policy

## Table of contents

- [Reporting a vulnerability](#reporting-a-vulnerability)
- [What to include](#what-to-include)
- [What to expect](#what-to-expect)
- [Scope notes](#scope-notes)

## Reporting a vulnerability

Please report security issues privately first.

Recommended approach:
- open a private channel if available
- or contact the maintainer directly before opening a public issue

Do **not** publish weaponized details first if the issue could harm users.

## What to include

Please include:

- affected version or commit
- reproduction steps
- whether the issue affects:
  - local MCP source execution
  - proxy invocation
  - config loading
  - benchmark tooling
  - Web UI
- severity estimate if you have one
- whether the issue requires local access or a malicious upstream MCP

## What to expect

A good-faith report should receive:
- acknowledgement
- triage
- a fix or mitigation plan if confirmed

## Scope notes

`lazy-tool-x` is intentionally local-first and local-only.

That means many risks are about:
- executing configured local stdio commands
- trusting local HTTP MCP endpoints
- exposing the Web UI beyond localhost
- benchmark and dev helper misuse

Please keep threat assumptions realistic to the current local-only scope.
