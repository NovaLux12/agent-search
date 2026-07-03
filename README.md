# agent-search

CLI to search and query across directories of `agent.json` identity cards. Find agents by capability tag, name pattern, handle, trust level, or `updated_at` freshness. Single static binary, zero runtime dependencies, uses [`NovaLux12/agent-validate`](../agent-validate) for schema-aware parsing.

```text
$ agent-search ./agents --capability code-generation --capability web-search
./agents/kai.agent.json  @kai@reflectt.ai  verified
./agents/nova-lux.agent.json  @nova@novalux12.dev  active

2 agents matching all of:
  capability=code-generation
  capability=web-search

$ agent-search ./agents --trust-level verified --json | jq '.[].handle'
"@kai@reflectt.ai"

$ agent-search ./agents --stale-threshold 30d --quiet
./agents/old-nova.agent.json
```

## Why this exists

The `reflectt/agent-identity-kit` v1 schema lets agents publish `agent.json` cards. If you're a directory operator (or just a curious developer with a folder of cards), you want to query them: "Which agents have `code-review` capability?", "Which agents claim `verified` trust?", "Which agents haven't been updated in 6 months?"

`agent-search` walks a directory tree, parses each `agent.json` it finds, validates against the embedded schema (so it can ignore malformed cards without crashing), and lets you filter with flag-driven queries. Output is human-readable text by default, JSON with `--json`, or just paths with `--quiet`.

## Install

```sh
# Linux x86_64
curl -L https://github.com/NovaLux12/agent-search/releases/latest/download/agent-search_linux_amd64.tar.gz \
  | tar xz -C /usr/local/bin agent-search

# macOS Apple Silicon
curl -L https://github.com/NovaLux12/agent-search/releases/latest/download/agent-search_darwin_arm64.tar.gz \
  | tar xz -C /usr/local/bin agent-search

# go install (any platform)
go install github.com/NovaLux12/agent-search/cmd/agent-search@latest

# Build from source
git clone https://github.com/NovaLux12/agent-search.git
cd agent-search
go build -ldflags="-s -w" -o agent-search ./cmd/agent-search
```

## Usage

```text
agent-search [flags] <directory>

Flags:
  --capability <tag>      filter: agent must declare this capability (repeatable; AND-combined)
  --name <substring>      filter: agent.name must contain this substring (case-insensitive)
  --handle <substring>    filter: agent.handle must contain this substring
  --owner <substring>     filter: owner.name must contain this substring
  --trust-level <level>   filter: trust.level must equal one of new|active|established|verified
  --protocol <name>       filter: protocols.<name> must be true (mcp|a2a|agent-card|http)
  --has-card-url          filter: endpoints.card must be a non-empty http(s) URL
  --stale-threshold <dur> filter: include only agents with updated_at older than this (e.g. 30d, 6mo, 1y; also Go stdlib 720h, 1h30m)
  --max-depth <n>         limit recursion depth (0 = no recursion, default 16)
  --json                  output results as a JSON array of matches
  --quiet                 output only file paths, one per line
  --include-invalid       include agent.json files that fail schema validation (marked invalid=true)
  --limit <n>             stop after N matches
  --require-match         exit 1 when no matches found (CI-friendly)
  --version               print version and exit
  --help                  show help
```

### Examples

```sh
# Find all agents that declare code-review capability
agent-search ./agents --capability code-review

# Find agents with verified trust that have a card URL declared
agent-search ./agents --trust-level verified --has-card-url

# Find agents by Kai (substring match on name)
agent-search ./agents --name "kai"

# Find stale agents (no updated_at in last 90 days)
agent-search ./agents --stale-threshold 90d

# Combine: agents with both code-generation AND web-search
agent-search ./agents --capability code-generation --capability web-search

# JSON output for downstream tooling
agent-search ./agents --capability code-review --json | jq 'length'

# CI: did anyone publish a card without MCP enabled?
agent-search ./production --protocol mcp --quiet
# exit 0 if any matches found, 1 if none
```

### Exit codes

| code | meaning |
| ---: | :------- |
|    0 | matches found (or no filter set — empty result is still exit 0) |
|    1 | no matches (only with `--require-match` flag, default is 0) |
|    2 | argument error |
|    3 | I/O error |

## Output formats

### Text (default)

Each match is one line: path, handle, trust level, with the matched fields highlighted. If no filter is set, every `agent.json` found is listed.

### JSON (`--json`)

```json
[
  {
    "path": "./agents/kai.agent.json",
    "valid": true,
    "agent": {
      "name": "Kai",
      "handle": "@kai@reflectt.ai",
      "description": "..."
    },
    "owner": {"name": "Reflectt Inc"},
    "trust": {"level": "verified"},
    "capabilities": ["code-generation", "web-search"],
    "protocols": {"mcp": true, "a2a": false, "agent-card": "1.0"},
    "endpoints": {"card": "https://reflectt.ai/.well-known/agent.json"},
    "updated_at": "2026-07-03T..."
  }
]
```

### Quiet (`--quiet`)

Just file paths, one per line. Suitable for piping into `xargs`.

## About malformed cards

By default, malformed `agent.json` files (failing schema validation) are skipped. The exit code stays 0 unless an I/O error prevented reading the directory at all.

If you want to see malformed cards in the results, pass `--include-invalid`. They'll be marked with `"valid": false` in JSON output, or with `(invalid)` after the path in text output. This is useful for cleaning up a directory: find all broken cards and fix or remove them.

## Related

- [`NovaLux12/agent-validate`](../agent-validate) — schema library used for parsing
- [`NovaLux12/agent-init`](../agent-init) — generate new agent.json cards
- [`NovaLux12/agentcard-mcp`](../agentcard-mcp) — MCP server for the same data
- [`reflectt/agent-identity-kit`](https://github.com/reflectt/agent-identity-kit) — the spec

## License

MIT.