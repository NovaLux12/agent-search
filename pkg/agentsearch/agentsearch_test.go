package agentsearch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// minimalCard is the smallest valid agent.json for the
// reflectt/agent-identity-kit v1 schema. Tests build on it.
const minimalCard = `{
  "version": "1.0",
  "agent": {"name": "TestAgent", "handle": "@test@example.com", "description": "x"},
  "owner": {"name": "Test Owner"},
  "endpoints": {"card": "https://example.com/.well-known/agent.json"},
  "updated_at": "2026-07-01T00:00:00Z"
}`

func writeCard(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestParseAgent_Minimal verifies parseAgent extracts the v1 fields.
func TestParseAgent_Minimal(t *testing.T) {
	a, err := parseAgent("/tmp/x.agent.json", []byte(minimalCard))
	if err != nil {
		t.Fatalf("parseAgent: %v", err)
	}
	if a.Name != "TestAgent" {
		t.Errorf("Name: got %q", a.Name)
	}
	if a.Handle != "@test@example.com" {
		t.Errorf("Handle: got %q", a.Handle)
	}
	if a.OwnerName != "Test Owner" {
		t.Errorf("OwnerName: got %q", a.OwnerName)
	}
	if a.CardURL != "https://example.com/.well-known/agent.json" {
		t.Errorf("CardURL: got %q", a.CardURL)
	}
	if a.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt not parsed")
	}
}

// TestParseAgent_BadJSON ensures parseAgent flags broken JSON
// rather than panicking.
func TestParseAgent_BadJSON(t *testing.T) {
	_, err := parseAgent("/tmp/x.agent.json", []byte(`{not json`))
	if err == nil {
		t.Fatalf("expected error on bad JSON")
	}
}

// TestParseAgent_Partial verifies parseAgent is forgiving of
// missing fields (returns zero values, not an error).
func TestParseAgent_Partial(t *testing.T) {
	_, err := parseAgent("/tmp/x.agent.json", []byte(`{"version":"1.0"}`))
	if err != nil {
		t.Fatalf("parseAgent partial: %v", err)
	}
	// Just check it doesn't blow up. The point is no error.
}

// TestHasCapability tests the case-insensitive capability match.
func TestHasCapability(t *testing.T) {
	a := Agent{Capabilities: []string{"code-generation", "Web-Search"}}
	if !a.HasCapability("code-generation") {
		t.Errorf("expected code-generation to match")
	}
	if !a.HasCapability("CODE-GENERATION") {
		t.Errorf("expected CODE-GENERATION (uppercase) to match case-insensitively")
	}
	if !a.HasCapability("web-search") {
		t.Errorf("expected web-search to match")
	}
	if a.HasCapability("missing") {
		t.Errorf("expected missing to not match")
	}
	if a.HasCapability("") {
		t.Errorf("expected empty capability to return false")
	}
}

// TestHasProtocol verifies the protocol-name switch.
func TestHasProtocol(t *testing.T) {
	a := Agent{Protocols: Protocols{MCP: true, A2A: false, HTTPAgent: true}}
	if !a.HasProtocol("mcp") {
		t.Errorf("expected mcp to be enabled")
	}
	if a.HasProtocol("a2a") {
		t.Errorf("expected a2a to be disabled")
	}
	if !a.HasProtocol("http") {
		t.Errorf("expected http to be enabled")
	}
	if a.HasProtocol("unknown") {
		t.Errorf("expected unknown protocol to return false")
	}
}

// TestIsStale covers the staleness logic: empty updated_at is stale,
// recent is not, old is.
func TestIsStale(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	thirtyDays := 30 * 24 * time.Hour

	// No updated_at — always stale.
	a := Agent{}
	if !a.IsStale(now, thirtyDays) {
		t.Errorf("expected agent with no updated_at to be stale")
	}

	// Recent — not stale.
	a = Agent{UpdatedAt: now.Add(-7 * 24 * time.Hour)}
	if a.IsStale(now, thirtyDays) {
		t.Errorf("expected 7-day-old agent to be fresh within 30d threshold")
	}

	// Old — stale.
	a = Agent{UpdatedAt: now.Add(-60 * 24 * time.Hour)}
	if !a.IsStale(now, thirtyDays) {
		t.Errorf("expected 60-day-old agent to be stale within 30d threshold")
	}
}

// TestSearch_NoFiltersReturnsAll: a query with no constraints
// returns every valid card in the directory.
func TestSearch_NoFiltersReturnsAll(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "a.agent.json", minimalCard)
	writeCard(t, dir, "b.agent.json", minimalCard)
	writeCard(t, dir, "c.agent.json", minimalCard)

	s := NewSearcher()
	matches, errs := s.Search(context.Background(), dir, Query{})
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(matches) != 3 {
		t.Errorf("expected 3 matches, got %d", len(matches))
	}
}

// TestSearch_CapabilityFilter: --capability only returns matching cards.
func TestSearch_CapabilityFilter(t *testing.T) {
	dir := t.TempDir()
	codeGen := `{
        "version": "1.0",
        "agent": {"name": "Coder", "handle": "@c@c.com", "description": "x"},
        "owner": {"name": "O"},
        "capabilities": ["code-generation"],
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	webSearch := `{
        "version": "1.0",
        "agent": {"name": "Searcher", "handle": "@s@s.com", "description": "x"},
        "owner": {"name": "O"},
        "capabilities": ["web-search"],
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	writeCard(t, dir, "coder.agent.json", codeGen)
	writeCard(t, dir, "searcher.agent.json", webSearch)

	s := NewSearcher()
	matches, _ := s.Search(context.Background(), dir, Query{Capabilities: []string{"code-generation"}})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if !strings.Contains(matches[0].Path, "coder.agent.json") {
		t.Errorf("expected coder path, got %s", matches[0].Path)
	}
}

// TestSearch_MultipleCapabilitiesAreANDCombimed: two capabilities
// in the query require the card to declare BOTH.
func TestSearch_MultipleCapabilitiesAreANDCombimed(t *testing.T) {
	dir := t.TempDir()
	both := `{
        "version": "1.0",
        "agent": {"name": "Both", "handle": "@b@b.com", "description": "x"},
        "owner": {"name": "O"},
        "capabilities": ["code-generation", "web-search"],
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	oneOnly := `{
        "version": "1.0",
        "agent": {"name": "One", "handle": "@o@o.com", "description": "x"},
        "owner": {"name": "O"},
        "capabilities": ["code-generation"],
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	writeCard(t, dir, "both.agent.json", both)
	writeCard(t, dir, "one.agent.json", oneOnly)

	s := NewSearcher()
	matches, _ := s.Search(context.Background(), dir, Query{
		Capabilities: []string{"code-generation", "web-search"},
	})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if !strings.Contains(matches[0].Path, "both.agent.json") {
		t.Errorf("expected both path, got %s", matches[0].Path)
	}
}

// TestSearch_NameSubstring tests the case-insensitive name match.
func TestSearch_NameSubstring(t *testing.T) {
	dir := t.TempDir()
	kai := strings.Replace(minimalCard, `"TestAgent"`, `"Kai"`, 1)
	other := strings.Replace(minimalCard, `"TestAgent"`, `"Nova"`, 1)
	writeCard(t, dir, "kai.agent.json", kai)
	writeCard(t, dir, "nova.agent.json", other)

	s := NewSearcher()
	matches, _ := s.Search(context.Background(), dir, Query{NameSubstring: "kai"})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
}

// TestSearch_TrustLevelFilter tests the trust level exact match.
func TestSearch_TrustLevelFilter(t *testing.T) {
	dir := t.TempDir()
	verified := `{
        "version": "1.0",
        "agent": {"name": "V", "handle": "@v@v.com", "description": "x"},
        "owner": {"name": "O"},
        "trust": {"level": "verified"},
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	active := `{
        "version": "1.0",
        "agent": {"name": "A", "handle": "@a@a.com", "description": "x"},
        "owner": {"name": "O"},
        "trust": {"level": "active"},
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	writeCard(t, dir, "v.agent.json", verified)
	writeCard(t, dir, "a.agent.json", active)

	s := NewSearcher()
	matches, _ := s.Search(context.Background(), dir, Query{TrustLevel: "verified"})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if !strings.Contains(matches[0].Path, "v.agent.json") {
		t.Errorf("expected v path, got %s", matches[0].Path)
	}
}

// TestSearch_SchemaInvalidSkipped: by default, broken cards are skipped.
func TestSearch_SchemaInvalidSkipped(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "good.agent.json", minimalCard)
	writeCard(t, dir, "broken.agent.json", `{"version":"1.0","agent":{"name":"x"}}`) // missing owner

	s := NewSearcher()
	matches, errs := s.Search(context.Background(), dir, Query{})
	if len(matches) != 1 {
		t.Errorf("expected 1 valid match, got %d", len(matches))
	}
	if len(errs) == 0 {
		t.Errorf("expected at least one error for broken card")
	}
}

// TestSearch_IncludeInvalidFlag returns broken cards when requested.
func TestSearch_IncludeInvalidFlag(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "broken.agent.json", `{"version":"1.0","agent":{"name":"x"}}`)

	s := NewSearcher()
	matches, _ := s.Search(context.Background(), dir, Query{IncludeInvalid: true})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (broken card), got %d", len(matches))
	}
	if matches[0].Valid {
		t.Errorf("expected valid=false on broken card")
	}
}

// TestSearch_StaleFilter: --stale-threshold excludes fresh cards.
func TestSearch_StaleFilter(t *testing.T) {
	dir := t.TempDir()
	fresh := `{
        "version": "1.0",
        "agent": {"name": "F", "handle": "@f@f.com", "description": "x"},
        "owner": {"name": "O"},
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-03T00:00:00Z"
    }`
	stale := `{
        "version": "1.0",
        "agent": {"name": "S", "handle": "@s@s.com", "description": "x"},
        "owner": {"name": "O"},
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2025-01-01T00:00:00Z"
    }`
	writeCard(t, dir, "fresh.agent.json", fresh)
	writeCard(t, dir, "stale.agent.json", stale)

	s := NewSearcher()
	// Stale threshold: 30 days from "now" (test time).
	matches, _ := s.Search(context.Background(), dir, Query{StaleThreshold: 30 * 24 * time.Hour})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (stale card), got %d", len(matches))
	}
	if !strings.Contains(matches[0].Path, "stale.agent.json") {
		t.Errorf("expected stale path, got %s", matches[0].Path)
	}
}

// TestSearch_HasCardURL: --has-card-url filters cards without endpoints.card.
func TestSearch_HasCardURL(t *testing.T) {
	dir := t.TempDir()
	withURL := `{
        "version": "1.0",
        "agent": {"name": "W", "handle": "@w@w.com", "description": "x"},
        "owner": {"name": "O"},
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	noURL := `{
        "version": "1.0",
        "agent": {"name": "N", "handle": "@n@n.com", "description": "x"},
        "owner": {"name": "O"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	writeCard(t, dir, "with.agent.json", withURL)
	writeCard(t, dir, "no.agent.json", noURL)

	s := NewSearcher()
	matches, _ := s.Search(context.Background(), dir, Query{HasCardURL: true})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (with URL), got %d", len(matches))
	}
}

// TestSearch_ProtocolFilter: --protocol=mcp filters cards with mcp enabled.
func TestSearch_ProtocolFilter(t *testing.T) {
	dir := t.TempDir()
	withMCP := `{
        "version": "1.0",
        "agent": {"name": "M", "handle": "@m@m.com", "description": "x"},
        "owner": {"name": "O"},
        "protocols": {"mcp": true, "agent-card": "1.0"},
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	noMCP := `{
        "version": "1.0",
        "agent": {"name": "N", "handle": "@n@n.com", "description": "x"},
        "owner": {"name": "O"},
        "protocols": {"agent-card": "1.0"},
        "endpoints": {"card": "https://example.com/.well-known/agent.json"},
        "updated_at": "2026-07-01T00:00:00Z"
    }`
	writeCard(t, dir, "with.agent.json", withMCP)
	writeCard(t, dir, "no.agent.json", noMCP)

	s := NewSearcher()
	matches, _ := s.Search(context.Background(), dir, Query{Protocols: []string{"mcp"}})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (with MCP), got %d", len(matches))
	}
}

// TestSearch_MaxDepthZero: --max-depth=0 means no recursion.
func TestSearch_MaxDepthZero(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	writeCard(t, root, "top.agent.json", minimalCard)
	writeCard(t, sub, "deep.agent.json", minimalCard)

	s := &Searcher{MaxDepth: 0, Now: time.Now}
	matches, _ := s.Search(context.Background(), root, Query{})
	if len(matches) != 1 {
		t.Errorf("expected 1 match (root only), got %d", len(matches))
	}
}

// TestSearch_SkipsNonCardFiles: only files matching the agent.json
// naming convention are scanned.
func TestSearch_SkipsNonCardFiles(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "real.agent.json", minimalCard)
	writeCard(t, dir, "config.json", `{"some":"other","data":"here"}`)
	writeCard(t, dir, "readme.md", `# README`)
	writeCard(t, dir, "agent.json", minimalCard) // exact match too

	s := NewSearcher()
	matches, _ := s.Search(context.Background(), dir, Query{})
	if len(matches) != 2 {
		t.Errorf("expected 2 matches (agent.json + .agent.json), got %d", len(matches))
	}
}

// TestAgentSearchJSON confirms the Agent struct serializes cleanly.
func TestAgentSearchJSON(t *testing.T) {
	a := Agent{
		Path:         "/tmp/x.agent.json",
		Valid:        true,
		Name:         "X",
		Handle:       "@x@x.com",
		Capabilities: []string{"code-review"},
		Protocols:    Protocols{MCP: true},
		UpdatedAt:    time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"path":"/tmp/x.agent.json"`) {
		t.Errorf("JSON missing path: %s", b)
	}
	if !strings.Contains(string(b), `"capabilities":["code-review"]`) {
		t.Errorf("JSON missing capabilities: %s", b)
	}
}

// TestFileErrorString verifies the error-rendering format.
func TestFileErrorString(t *testing.T) {
	fe := FileError{Path: "/tmp/x", Err: &testErr{"boom"}}
	if !strings.Contains(fe.String(), "/tmp/x") {
		t.Errorf("missing path: %s", fe.String())
	}
	if !strings.Contains(fe.String(), "boom") {
		t.Errorf("missing error message: %s", fe.String())
	}
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }