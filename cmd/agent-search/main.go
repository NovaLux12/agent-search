// Command agent-search queries a directory of agent.json identity
// cards. It is the discovery half of the agent-identity-kit
// toolchain: agent-init creates cards, agent-validate checks them,
// agentcard-mcp serves them, agent-search finds them.
//
// Usage:
//
//	agent-search [flags] <directory>
//
// Exit codes:
//
//	0  matches found (or no filter set — empty result is still exit 0)
//	1  no matches (only with --require-match, default is exit 0)
//	2  argument error
//	3  I/O error
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/NovaLux12/agent-search/pkg/agentsearch"
)

// Version is the agent-search release tag. Bump in lockstep with
// CHANGELOG.md.
const Version = "0.1.0"

// durationFlag is a flag.Value that accepts the standard Go duration
// format ("30s", "5m", "2h") plus human-friendly suffixes for days,
// weeks, months, and years. The user-facing docs claim "30d, 6mo, 1y"
// work, and we want to honor that without making them write "720h".
//
// Conversions:
//
//	d  = 24h
//	w  = 7d  = 168h
//	mo = 30d = 720h   (approximate; Go has no native month)
//	y  = 365d = 8760h (approximate)
//
// Numeric forms are passed through to time.ParseDuration so the full
// standard library syntax (e.g. "1h30m") keeps working.
type durationFlag struct {
	target *time.Duration
}

func (d *durationFlag) String() string {
	if d.target == nil {
		return "0s"
	}
	return d.target.String()
}

func (d *durationFlag) Set(s string) error {
	if d.target == nil {
		return fmt.Errorf("durationFlag: nil target")
	}
	parsed, err := parseHumanDuration(s)
	if err != nil {
		return err
	}
	*d.target = parsed
	return nil
}

// parseHumanDuration parses a duration string with optional suffixes:
// "30s", "5m", "2h", "30d", "4w", "6mo", "1y". The order of suffix
// checks is longest-first to avoid "mo" being eaten by "m".
func parseHumanDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	suffixes := []struct {
		suf    string
		mult   time.Duration
		minLen int // minimum token length including suffix
	}{
		{"mo", 30 * 24 * time.Hour, 3},
		{"y", 365 * 24 * time.Hour, 2},
		{"w", 7 * 24 * time.Hour, 2},
		{"d", 24 * time.Hour, 2},
		{"h", time.Hour, 2},
		{"m", time.Minute, 2},
		{"s", time.Second, 2},
	}
	for _, suf := range suffixes {
		if len(s) >= suf.minLen && strings.HasSuffix(s, suf.suf) {
			numStr := s[:len(s)-len(suf.suf)]
			n, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: %w", s, err)
			}
			return time.Duration(n * float64(suf.mult)), nil
		}
	}
	// Fall through to Go stdlib (handles composite like "1h30m" if
	// the user wants that — note: our suffix logic above would catch
	// the trailing "m" first, so this branch is only reached if no
	// recognised suffix matches, in which case ParseDuration will
	// reject the input).
	return time.ParseDuration(s)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point. It accepts the args, stdout, and
// stderr streams so tests can inject captures.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent-search", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		capabilities multiFlag
		protocols    multiFlag
		nameSub      = fs.String("name", "", "filter: agent.name must contain this substring (case-insensitive)")
		handleSub    = fs.String("handle", "", "filter: agent.handle must contain this substring (case-insensitive)")
		ownerSub     = fs.String("owner", "", "filter: owner.name must contain this substring (case-insensitive)")
		trustLevel   = fs.String("trust-level", "", "filter: trust.level must equal one of new|active|established|verified")
		hasCardURL   = fs.Bool("has-card-url", false, "filter: endpoints.card must be a non-empty http(s) URL")
		stale        time.Duration
		maxDepth     = fs.Int("max-depth", 16, "limit recursion depth (0 = no recursion)")
		jsonOut      = fs.Bool("json", false, "output results as a JSON array")
		quiet        = fs.Bool("quiet", false, "output only file paths, one per line")
		includeInv   = fs.Bool("include-invalid", false, "include agent.json files that fail schema validation")
		limit        = fs.Int("limit", 0, "stop after N matches (0 = unlimited)")
		requireMatch = fs.Bool("require-match", false, "exit 1 when no matches found")
		showVersion  = fs.Bool("version", false, "print version and exit")
	)
	fs.Var(&capabilities, "capability", "filter: agent must declare this capability (repeatable; AND-combined)")
	fs.Var(&protocols, "protocol", "filter: protocols.<name> must be true (repeatable; mcp|a2a|agent-card|http)")
	fs.Var(&durationFlag{target: &stale}, "stale-threshold", "filter: include only agents with updated_at older than this (e.g. 30d, 6mo, 1y; also accepts Go stdlib 720h, 1h30m, etc.)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, `agent-search %s — query a directory of agent.json identity cards

Usage:
  agent-search [flags] <directory>

Flags:
`, Version)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, `
Examples:
  agent-search ./agents --capability code-review
  agent-search ./agents --trust-level verified --has-card-url
  agent-search ./agents --capability code-generation --capability web-search
  agent-search ./agents --stale-threshold 90d
  agent-search ./agents --capability code-review --json | jq 'length'

Exit codes:
  0  matches found (or no filter set; empty result is still exit 0)
  1  no matches (only with --require-match)
  2  argument error
  3  I/O error
`)
	}

	// Go's stdlib flag package stops parsing at the first non-flag
	// argument. To make this CLI friendlier (so the directory can
	// appear anywhere), we reorder: pull the single non-flag argument
	// to the end before parsing.
	args = reorderArgs(args)

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		fmt.Fprintf(stdout, "agent-search %s\n", Version)
		return 0
	}

	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: expected exactly one positional argument (directory)")
		fs.Usage()
		return 2
	}
	root := fs.Arg(0)

	// Validate trust-level value if provided.
	if *trustLevel != "" {
		switch strings.ToLower(*trustLevel) {
		case "new", "active", "established", "verified":
		default:
			fmt.Fprintf(stderr, "error: --trust-level must be one of new, active, established, verified (got %q)\n", *trustLevel)
			return 2
		}
	}

	// Build the query.
	q := agentsearch.Query{
		Capabilities:    capabilities,
		NameSubstring:   *nameSub,
		HandleSubstring: *handleSub,
		OwnerSubstring:  *ownerSub,
		TrustLevel:      *trustLevel,
		Protocols:       protocols,
		HasCardURL:      *hasCardURL,
		IncludeInvalid:  *includeInv,
	}
	if stale > 0 {
		q.StaleThreshold = stale
	}

	// Set up context with signal handling for Ctrl-C.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	searcher := &agentsearch.Searcher{
		MaxDepth: *maxDepth,
		Now:      time.Now,
	}
	matches, errs := searcher.Search(ctx, root, q)

	// Apply limit (cap the slice; cap at len if limit==0).
	if *limit > 0 && len(matches) > *limit {
		matches = matches[:*limit]
	}

	// Render output.
	if *jsonOut {
		b, err := json.MarshalIndent(matches, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: could not marshal JSON: %v\n", err)
			return 3
		}
		fmt.Fprintln(stdout, string(b))
	} else if *quiet {
		for _, m := range matches {
			fmt.Fprintln(stdout, m.Path)
		}
	} else {
		// Text mode: one line per match.
		if len(matches) == 0 {
			fmt.Fprintln(stdout, "(no matches)")
		} else {
			fmt.Fprintf(stdout, "%d match(es):\n", len(matches))
			for _, m := range matches {
				renderAgentLine(stdout, m)
			}
		}
	}

	// Surface file errors to stderr.
	for _, fe := range errs {
		fmt.Fprintf(stderr, "warning: %s\n", fe.String())
	}

	if len(matches) == 0 && *requireMatch {
		return 1
	}
	return 0
}

// renderAgentLine writes one match line in text mode. The shape is:
//
//	path  handle  trust_level  capabilities
//
// with (invalid) appended when the card failed schema validation.
// Long handles/trust levels are truncated for readability; the
// caller can use --json for the full data.
func renderAgentLine(w io.Writer, m agentsearch.Agent) {
	handle := m.Handle
	if handle == "" {
		handle = "(no handle)"
	}
	trust := m.TrustLevel
	if trust == "" {
		trust = "—"
	}
	caps := strings.Join(m.Capabilities, ",")
	if caps == "" {
		caps = "—"
	}
	invalid := ""
	if !m.Valid {
		invalid = " (invalid)"
	}
	fmt.Fprintf(w, "  %s  %s  %s  [%s]%s\n", m.Path, handle, trust, caps, invalid)
}

// reorderArgs moves every positional (the directory) to the end so
// Go's stdlib flag.Parse can read all flags first.
//
// Go's stdlib flag.Parse stops at the FIRST non-flag argument; any
// subsequent tokens that look like flags (e.g. "--capability") are
// pushed into positional args. So without reordering,
// "agent-search ./dir --capability code-review" sees "./dir" as the
// directory but never reads --capability, and --capability + "code-review"
// land in positional args, triggering "expected exactly one positional".
//
// We solve this by scanning args, classifying each token as either
// a flag, a flag value, or a positional. We then emit the flags in
// their original order and append the positionals at the end. The
// expected positional shape is exactly one directory, so this is
// straightforward.
//
// The set of bool flags is hard-coded here. If you add a new bool
// flag, add it to this set so reorderArgs treats it correctly.
func reorderArgs(args []string) []string {
	boolFlags := map[string]bool{
		"json":            true,
		"quiet":           true,
		"include-invalid": true,
		"has-card-url":    true,
		"require-match":   true,
		"version":         true,
	}

	var (
		flags       []string
		positionals []string
	)
	i := 0
	for i < len(args) {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			// Non-flag token: positional.
			positionals = append(positionals, a)
			i++
			continue
		}
		// Strip leading dashes to get the flag name.
		name := strings.TrimLeft(a, "-")
		// If the flag takes a value AND the next token is not itself
		// a flag, consume the next token as that value.
		if !boolFlags[name] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flags = append(flags, a, args[i+1])
			i += 2
			continue
		}
		flags = append(flags, a)
		i++
	}
	return append(flags, positionals...)
}

// values. We can't use the same name for a String flag and a
// repeatable flag, so this small helper makes the syntax readable.
type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}
