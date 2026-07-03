# Changelog

All notable changes to agent-search are documented here. Dates are
YYYY-MM-DD. Versions follow [Semantic Versioning](https://semver.org/).

## [0.1.0] — 2026-07-04

Initial release.

### Features
- Walk a directory tree (recursion depth 0–16, configurable) and
  find `agent.json` / `*.agent.json` files matching a `Query`.
- Filters: `--capability` (repeatable, AND-combined), `--name`,
  `--handle`, `--owner` (case-insensitive substring), `--trust-level`
  (exact), `--protocol` (repeatable), `--has-card-url`, `--stale-threshold`.
- Output modes: text (default), `--json`, `--quiet` (paths only).
- Schema validation: cards failing the `reflectt/agent-identity-kit`
  v1 schema are skipped (with a stderr warning) unless
  `--include-invalid` is passed.
- Exit codes: 0 (matches or empty without `--require-match`), 1
  (`--require-match` and no matches), 2 (argument error), 3 (I/O).

### Technical
- Single static binary, zero runtime dependencies.
- Built against `NovaLux12/agent-validate v0.2.0` for schema-aware
  parsing (uses the embedded schema).
- Go 1.26, no third-party runtime deps; only `agent-validate` +
  `qri-io/jsonschema` (transitive).

### Notable bug fixes vs the unmerged `initial-scaffold` branch
- `reorderArgs` rewritten to correctly handle dir-first argument
  ordering (`agent-search ./dir --capability foo` now parses flags
  correctly). Old logic confused flag values with the directory
  positional.
- `--stale-threshold` now accepts human-friendly suffixes (`30d`,
  `6mo`, `1y`) on top of Go stdlib durations (`720h`, `1h30m`).
  Old version rejected `30d` because Go's `flag.Duration` only
  understands ns/μs/ms/s/m/h.
- Removed `--exclude-stale`: the flag had confused semantics
  (docstring said one thing, implementation did another, README
  said a third). v0.2.0 will add a proper `--max-age` if needed.