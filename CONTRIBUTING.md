# Contributing to lazy-tool

## Table of contents

- [Project mindset](#project-mindset)
- [Good first contributions](#good-first-contributions)
- [Development setup](#development-setup)
- [Core commands](#core-commands)
- [Code style expectations](#code-style-expectations)
- [Documentation expectations](#documentation-expectations)
- [Benchmark claim policy](#benchmark-claim-policy)
- [Submitting a pull request](#submitting-a-pull-request)

## Project mindset

`lazy-tool` is a **local-first, local-only MCP discovery runtime**.

Please keep contributions aligned with that identity.

Good contributions usually:
- improve local usability
- improve runtime reliability
- improve search quality or explainability
- improve benchmark rigor
- improve docs and trust surfaces

Bad contributions usually:
- introduce cloud or platform assumptions
- add enterprise-grade abstractions with no local need
- expand the public MCP surface casually
- add configuration knobs without strong justification
- overbuild before profiling or evidence

## Good first contributions

Strong first contribution areas:

- improve error messages in CLI flows
- tighten docs around local MCP integration
- improve search explainability or inspect output
- add focused tests around runtime and search behavior
- improve benchmark reproducibility or result reporting

## Development setup

### Requirements

- Go 1.25+
- Python 3.11+ and [uv](https://docs.astral.sh/uv/) (or pip) for benchmark tooling
- optional: Docker and MCPJungle for the local benchmark environment
- optional: [Ollama](https://ollama.com/) for weak-model benchmarks

### Build

```bash
make build
```

### Run tests

```bash
make test
go test ./...
```

### Vet and lint

```bash
make vet
make lint
```

## Core commands

```bash
./bin/lazy-tool health
./bin/lazy-tool health --probe
./bin/lazy-tool reindex
./bin/lazy-tool sources --status
./bin/lazy-tool search "echo" --limit 5
./bin/lazy-tool inspect <proxy_tool_name>
./bin/lazy-tool serve
./bin/lazy-tool web --addr 127.0.0.1:8765
./bin/lazy-tool tui
```

## Code style expectations

Please prefer:

- small, reviewable diffs
- explicit naming
- simple local-first designs
- thin command handlers
- behavior tests over brittle implementation-detail tests

Please avoid:

- broad rewrites unless clearly justified
- speculative abstraction layers
- giant feature batches mixed with refactors
- cloud and platform drift

## Documentation expectations

If behavior changes, docs should usually change too.

At minimum, consider whether your change affects:

- `README.md`
- `benchmark/README.md`
- `docs/plugging-existing-mcps.md`
- `docs/README.md`

## Benchmark claim policy

If you add or change benchmark claims:

- keep them reproducible
- record the model used
- record repeat counts
- record the commit and date
- separate headline README claims from experimental results
- do not publish flaky end-to-end results as strong claims

The benchmark docs should remain more detailed than the root README.

## Submitting a pull request

A strong PR usually includes:

- a clear title
- a short problem statement
- what changed
- why the change is scoped correctly
- test and docs updates where relevant
- benchmark notes if benchmark behavior changed
