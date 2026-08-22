package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cliMinimalCard = `{
  "version": "1.0",
  "agent": {"name": "TestAgent", "handle": "@test@example.com", "description": "x"},
  "owner": {"name": "Test Owner"},
  "capabilities": ["code-generation"],
  "protocols": {"mcp": true, "agent-card": "1.0"},
  "endpoints": {"card": "https://example.com/.well-known/agent.json"},
  "updated_at": "2026-07-01T00:00:00Z"
}`

// findRepoRoot walks up until it finds go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for cur := wd; cur != "/"; cur = filepath.Dir(cur) {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
	}
	t.Fatalf("no go.mod above %s", wd)
	return ""
}

var sharedBin string

func buildCLI(t *testing.T) string {
	t.Helper()
	if sharedBin != "" {
		if _, err := os.Stat(sharedBin); err == nil {
			return sharedBin
		}
	}
	repoRoot := findRepoRoot(t)
	sharedBin = filepath.Join(t.TempDir(), "agent-search")
	cmd := exec.Command("go", "build", "-o", sharedBin, "./cmd/agent-search")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return sharedBin
}

func TestCLIBuilds(t *testing.T) {
	bin := buildCLI(t)
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "agent-search ") {
		t.Errorf("--version output unexpected: %q", out)
	}
}

func TestCLIPrintsHelpOnNoArgs(t *testing.T) {
	bin := buildCLI(t)
	// No args -> should print usage to stderr and exit 2.
	cmd := exec.Command(bin)
	if err := cmd.Run(); err == nil {
		t.Errorf("expected non-zero exit on no args")
	}
}

func TestCLIRequiresDirectory(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "--capability", "code-generation")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, got 0; output: %s", out)
	}
	if !strings.Contains(string(out), "expected exactly one positional argument") {
		t.Errorf("missing argument error message: %s", out)
	}
}

func TestCLICapabilityFilter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.agent.json"), []byte(cliMinimalCard), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.json"), []byte(`{"some":"other"}`), 0o644); err != nil {
		t.Fatalf("write other: %v", err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, "--capability", "code-generation", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "1 match") {
		t.Errorf("expected '1 match' in output, got: %s", out)
	}
	if !strings.Contains(string(out), "x.agent.json") {
		t.Errorf("expected x.agent.json in output, got: %s", out)
	}
}

func TestCLIQuietOutputsPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.agent.json"), []byte(cliMinimalCard), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "y.agent.json"), []byte(cliMinimalCard), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, "--quiet", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines in --quiet output, got %d: %s", len(lines), out)
	}
}

func TestCLIJSONOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.agent.json"), []byte(cliMinimalCard), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, "--json", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli failed: %v\n%s", err, out)
	}
	var matches []map[string]any
	if err := json.Unmarshal(out, &matches); err != nil {
		t.Fatalf("--json output not valid JSON: %v\n%s", err, out)
	}
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
	if matches[0]["name"] != "TestAgent" {
		t.Errorf("expected name=TestAgent, got %v", matches[0]["name"])
	}
}

func TestCLIRequireMatchFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.agent.json"), []byte(cliMinimalCard), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	bin := buildCLI(t)
	// Without --require-match, empty result is exit 0.
	cmd := exec.Command(bin, "--capability", "nonexistent", dir)
	if err := cmd.Run(); err != nil {
		t.Errorf("expected exit 0 for empty result without --require-match, got: %v", err)
	}

	// With --require-match, empty result is exit 1.
	cmd = exec.Command(bin, "--capability", "nonexistent", "--require-match", dir)
	if err := cmd.Run(); err == nil {
		t.Errorf("expected exit 1 for empty result with --require-match, got 0")
	}
}

func TestCLIInvalidTrustLevel(t *testing.T) {
	dir := t.TempDir()
	bin := buildCLI(t)
	cmd := exec.Command(bin, "--trust-level", "bogus", dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, got 0")
	}
	if !strings.Contains(string(out), "trust-level") {
		t.Errorf("missing trust-level error message: %s", out)
	}
}

// TestCLIArgsDirFirst: when the directory is the FIRST positional
// (i.e. user writes "./dir --flag val" rather than "--flag val ./dir"),
// the CLI must still parse flags correctly. This is a regression
// test for the reorderArgs bug where flag values were confused with
// the directory positional.
func TestCLIArgsDirFirst(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.agent.json"), []byte(cliMinimalCard), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, dir, "--capability", "code-generation")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "1 match") {
		t.Errorf("expected '1 match' in output, got: %s", out)
	}
}

// TestCLIArgsDirFirstMultipleFlags: same as above but with multiple
// flag-value pairs after the directory positional. This exercises the
// full reorderArgs path.
func TestCLIArgsDirFirstMultipleFlags(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.agent.json"), []byte(cliMinimalCard), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, dir, "--capability", "code-generation", "--quiet")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "x.agent.json") {
		t.Errorf("expected x.agent.json in --quiet output, got: %s", out)
	}
	// --quiet should produce exactly one line (no "N match(es):" header).
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line in --quiet output, got %d: %s", len(lines), out)
	}
}

// TestCLIArgsDirFirstWithTrustLevel: --trust-level takes an enum value;
// verify that the value isn't mistaken for the directory positional.
func TestCLIArgsDirFirstWithTrustLevel(t *testing.T) {
	dir := t.TempDir()
	verifiedCard := strings.Replace(cliMinimalCard, `"TestAgent"`, `"V"`, 1)
	verifiedCard = strings.Replace(verifiedCard, `@test@example.com`, `@v@v.com`, 1)
	if err := os.WriteFile(filepath.Join(dir, "v.agent.json"), []byte(verifiedCard), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// No trust block in cliMinimalCard; add one.
	full := `{
        "version": "1.0",
        "agent": {"name": "V", "handle": "@v@v.com", "description": "x"},
        "owner": {"name": "Test Owner"},
        "trust": {"level": "verified"},
        "capabilities": ["code-generation"],
        "protocols": {"mcp": true, "agent-card": "1.0"},
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	if err := os.WriteFile(filepath.Join(dir, "v.agent.json"), []byte(full), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	bin := buildCLI(t)
	cmd := exec.Command(bin, dir, "--trust-level", "verified", "--quiet")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "v.agent.json") {
		t.Errorf("expected v.agent.json in output, got: %s", out)
	}
}

// TestReorderArgs covers the reorderArgs function directly.
func TestReorderArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"no args", []string{}, []string{}},
		{"dir only", []string{"./dir"}, []string{"./dir"}},
		{"flag then dir", []string{"--json", "./dir"}, []string{"--json", "./dir"}},
		{"dir then bool flag", []string{"./dir", "--json"}, []string{"--json", "./dir"}},
		{"dir then value flag", []string{"./dir", "--capability", "foo"}, []string{"--capability", "foo", "./dir"}},
		{"value flag then dir", []string{"--capability", "foo", "./dir"}, []string{"--capability", "foo", "./dir"}},
		{"value then bool then dir", []string{"--capability", "foo", "--json", "./dir"}, []string{"--capability", "foo", "--json", "./dir"}},
		{"dir then multiple flags", []string{"./dir", "--cap", "foo", "--trust-level", "verified"}, []string{"--cap", "foo", "--trust-level", "verified", "./dir"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reorderArgs(tt.in)
			if !equalStringSlice(got, tt.want) {
				t.Errorf("reorderArgs(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParseHumanDuration verifies the duration flag accepts the
// human-friendly suffixes we advertise in docs.
func TestParseHumanDuration(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"30s", 30 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"2h", 2 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"4w", 4 * 7 * 24 * time.Hour, false},
		{"6mo", 6 * 30 * 24 * time.Hour, false},
		{"1y", 365 * 24 * time.Hour, false},
		{"1.5d", 36 * time.Hour, false},
		{"0s", 0, false},
		{"", 0, true},
		{"abc", 0, true},
		{"30x", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseHumanDuration(tt.in)
			if tt.err {
				if err == nil {
					t.Errorf("parseHumanDuration(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Errorf("parseHumanDuration(%q) error: %v", tt.in, err)
				return
			}
			if got != tt.want {
				t.Errorf("parseHumanDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// Compile-time check that the searcher and CLI package can be used together.
var _ = context.Background
