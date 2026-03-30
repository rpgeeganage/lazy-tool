# Documentation

## Table of contents

- [Start here](#start-here)
- [Integration docs](#integration-docs)
- [Benchmark docs](#benchmark-docs)
- [Maintainer docs](#maintainer-docs)

## Start here

If you are new to the project, start with:

- [repository README](../README.md)

That page covers:
- what the project is and why it exists
- serving modes (search, direct, hybrid) and HTTP transport
- auto-import from IDE configs (Claude Desktop, Cursor, VS Code)
- quick start, CLI reference, configuration
- response cache and invocation-based search learning
- benchmark highlights and multi-provider results
- project scope and limitations

## Integration docs

- [Plugging existing MCPs into lazy-tool](plugging-existing-mcps.md)

Use that doc when you want to point `lazy-tool` at MCP servers or MCP gateways you already run locally.

**Fastest path**: if your MCP servers are already configured in Claude Desktop, Cursor, or VS Code, use `lazy-tool import --write` to auto-generate a config.

## Benchmark docs

- [benchmark/README.md](../benchmark/README.md) — methodology, tasks, and multi-provider suite
- [benchmark/mcpjungle-dev/README.md](../benchmark/mcpjungle-dev/README.md) — local MCPJungle dev setup
- [benchmark-charts.html](benchmark-charts.html) — Chart.js bar/line views of README headline numbers (open in a browser)

Use these when you want to:
- reproduce headline benchmark claims
- run the local MCPJungle benchmark environment
- run multi-provider benchmarks (Groq tested; Anthropic/OpenAI harness ready)
- validate README-worthy numbers

## Maintainer docs

Recommended reading order:

1. [repository README](../README.md)
2. [plugging-existing-mcps.md](plugging-existing-mcps.md)
3. [benchmark/README.md](../benchmark/README.md)
