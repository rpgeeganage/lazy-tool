# MCPJungle local dev helpers

## Table of contents

- [What this folder is for](#what-this-folder-is-for)
- [Install and start MCPJungle](#install-and-start-mcpjungle)
- [Register sample servers](#register-sample-servers)
- [Recommended flow](#recommended-flow)
- [Troubleshooting](#troubleshooting)

## What this folder is for

This folder contains local helper files for running the benchmark environment with MCPJungle.

Use it when you want a reproducible local setup for:

- `lazy-tool-x reindex`
- discovery and search validation
- benchmark runs against a merged local MCP gateway

For the main benchmark methodology, see [../README.md](../README.md).

## Install and start MCPJungle

This repo expects MCPJungle's HTTP gateway at `http://127.0.0.1:8080/mcp`.

Install MCPJungle using the official docs:

- GitHub: [mcpjungle/MCPJungle](https://github.com/mcpjungle/MCPJungle)

Start MCPJungle locally (development mode):

```bash
mcpjungle start --port 8080
```

Quick check (in a second terminal):

```bash
curl -sS http://127.0.0.1:8080/mcp >/dev/null && echo "MCPJungle is up"
```

## Register sample servers

Use the included helper script:

```bash
cd benchmark/mcpjungle-dev
chmod +x register-samples.sh
./register-samples.sh
```

If your environment differs, edit the JSON configs in this folder first.

## Recommended flow

```mermaid
flowchart TD
    A[Register sample MCPs] --> B[Run lazy-tool-x reindex]
    B --> C[Validate catalog searches]
    C --> D[Run benchmark suite]
```

### 1. Register the sample MCPs

```bash
cd benchmark/mcpjungle-dev
./register-samples.sh
```

### 2. Reindex lazy-tool-x

```bash
export LAZY_TOOL_CONFIG=$PWD/../configs/mcpjungle-lazy-tool.yaml
cd ../.. && make build && ./bin/lazy-tool-x reindex
```

### 3. Validate search

```bash
./bin/lazy-tool-x search "echo" --limit 10
./bin/lazy-tool-x search "prompt" --limit 10
./bin/lazy-tool-x search "resource" --limit 10
```

## Troubleshooting

### `mcpjungle register` fails

Make sure you are using the JSON config files and not an empty command invocation.

### lazy-tool-x search returns nothing

Usually:
- sample MCPs were not registered
- `reindex` was skipped
- the benchmark config points at the wrong MCPJungle URL
