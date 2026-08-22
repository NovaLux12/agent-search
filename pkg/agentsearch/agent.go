// Package agentsearch provides search and query over directories of
// agent.json identity cards. The package is library-first: callers
// can construct a Searcher, run a Query, and iterate over the
// results. The cmd/agent-search CLI is a thin wrapper around it.
package agentsearch

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Agent is the parsed shape of an agent.json file. It mirrors the
// schema's top-level fields so the search engine can filter on any
// of them. Fields that aren't relevant to search (avatar URL,
// homepage, social links, attestations) are intentionally omitted —
// the goal is a flat record that can be filtered and serialized,
// not a faithful copy of the full card.
//
// Time is parsed into a time.Time so freshness checks can compare
// against the current time. UpdatedAt is the only timestamp used
// for staleness checks.
type Agent struct {
	Path         string    `json:"path"`
	Valid        bool      `json:"valid"`
	Name         string    `json:"name,omitempty"`
	Handle       string    `json:"handle,omitempty"`
	Description  string    `json:"description,omitempty"`
	OwnerName    string    `json:"owner_name,omitempty"`
	OwnerURL     string    `json:"owner_url,omitempty"`
	TrustLevel   string    `json:"trust_level,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	Protocols    Protocols `json:"protocols"`
	CardURL      string    `json:"card_url,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
	UpdatedAtRaw string    `json:"-"`
}

// Protocols is a flat list of which protocols the card declares as
// enabled. Each field maps to the v1 schema's protocols block.
type Protocols struct {
	MCP       bool `json:"mcp,omitempty"`
	A2A       bool `json:"a2a,omitempty"`
	HTTPAgent bool `json:"http,omitempty"`
}

// parseAgent reads raw JSON bytes and returns an Agent record. It
// does NOT run the schema validator — that's the caller's choice
// (some callers want to skip invalid cards, others want them marked
// valid=false).
//
// The parser is forgiving: missing fields stay at their zero values
// rather than producing an error. This keeps the search engine
// usable on partially-malformed cards.
func parseAgent(path string, data []byte) (Agent, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Agent{Path: path, Valid: false}, fmt.Errorf("not valid JSON: %w", err)
	}

	a := Agent{Path: path, Valid: true}

	if agent, ok := raw["agent"].(map[string]any); ok {
		a.Name, _ = agent["name"].(string)
		a.Handle, _ = agent["handle"].(string)
		a.Description, _ = agent["description"].(string)
	}
	if owner, ok := raw["owner"].(map[string]any); ok {
		a.OwnerName, _ = owner["name"].(string)
		a.OwnerURL, _ = owner["url"].(string)
	}
	if trust, ok := raw["trust"].(map[string]any); ok {
		a.TrustLevel, _ = trust["level"].(string)
	}
	if caps, ok := raw["capabilities"].([]any); ok {
		for _, c := range caps {
			if s, ok := c.(string); ok {
				a.Capabilities = append(a.Capabilities, s)
			}
		}
	}
	if protos, ok := raw["protocols"].(map[string]any); ok {
		a.Protocols.MCP, _ = protos["mcp"].(bool)
		a.Protocols.A2A, _ = protos["a2a"].(bool)
		a.Protocols.HTTPAgent, _ = protos["http"].(bool)
	}
	if endpoints, ok := raw["endpoints"].(map[string]any); ok {
		a.CardURL, _ = endpoints["card"].(string)
	}
	if updatedAt, ok := raw["updated_at"].(string); ok && updatedAt != "" {
		a.UpdatedAtRaw = updatedAt
		// RFC3339 is the canonical form; fall back to date-only if
		// the card uses a shorter timestamp.
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			a.UpdatedAt = t
		} else if t, err := time.Parse("2006-01-02", updatedAt); err == nil {
			a.UpdatedAt = t
		}
	}
	return a, nil
}

// HasCapability reports whether the agent declares the given
// capability tag. Empty strings always return false.
func (a Agent) HasCapability(tag string) bool {
	if tag == "" {
		return false
	}
	tag = strings.ToLower(tag)
	for _, c := range a.Capabilities {
		if strings.ToLower(c) == tag {
			return true
		}
	}
	return false
}

// HasProtocol returns whether the named protocol is enabled. The
// supported names match the v1 schema: "mcp", "a2a", "agent-card",
// "http". Unknown names return false.
func (a Agent) HasProtocol(name string) bool {
	switch name {
	case "mcp":
		return a.Protocols.MCP
	case "a2a":
		return a.Protocols.A2A
	case "http":
		return a.Protocols.HTTPAgent
	default:
		return false
	}
}

// IsStale reports whether the agent's updated_at is older than the
// given threshold relative to now. Agents without updated_at are
// treated as stale (better to flag and let the operator decide than
// to silently pass them).
func (a Agent) IsStale(now time.Time, threshold time.Duration) bool {
	if a.UpdatedAt.IsZero() {
		return true
	}
	return now.Sub(a.UpdatedAt) > threshold
}
