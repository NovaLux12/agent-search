package agentsearch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NovaLux12/agent-validate/pkg/agentvalidate"
)

// Query is the filter shape. All fields are AND-combined; an empty
// field means "no constraint on this dimension". A query with no
// constraints returns every card found.
type Query struct {
	// Capabilities: agent must declare ALL of these tags.
	Capabilities []string
	// NameSubstring: case-insensitive substring match on agent.name.
	NameSubstring string
	// HandleSubstring: case-insensitive substring match on agent.handle.
	HandleSubstring string
	// OwnerSubstring: case-insensitive substring match on owner.name.
	OwnerSubstring string
	// TrustLevel: exact match on trust.level.
	TrustLevel string
	// Protocols: agent must declare ALL of these protocols as enabled.
	Protocols []string
	// HasCardURL: endpoints.card must be a non-empty http(s) URL.
	HasCardURL bool
	// StaleThreshold: agent's updated_at must be more recent than
	// (now - threshold). Zero means no staleness check.
	StaleThreshold time.Duration
	// IncludeInvalid: include cards that failed schema validation
	// (marked valid=false in the result). Default false.
	IncludeInvalid bool
}

// Searcher walks a directory and applies a Query.
type Searcher struct {
	// MaxDepth limits recursion. 0 means no recursion (only the
	// root directory itself is scanned). -1 means unlimited.
	MaxDepth int
	// Now is the reference time used for staleness checks. Tests
	// inject a fixed value; production uses time.Now().
	Now func() time.Time
}

// NewSearcher returns a Searcher with default settings:
//   - MaxDepth = 16
//   - Now = time.Now
func NewSearcher() *Searcher {
	return &Searcher{
		MaxDepth: 16,
		Now:      time.Now,
	}
}

// Search walks root and applies query, returning every matching
// Agent. The returned slice is always non-nil (empty when no matches).
//
// I/O errors on individual files (permission denied, broken symlinks,
// unreadable JSON) are returned as a side-channel — the search
// continues with the next file rather than failing the whole run.
// The caller can decide whether to surface those errors.
func (s *Searcher) Search(ctx context.Context, root string, query Query) ([]Agent, []FileError) {
	var (
		matches []Agent
		errs    []FileError
	)

	depth := s.MaxDepth
	if depth < 0 {
		depth = -1 // fs.WalkDir doesn't accept -1; use huge value
	}
	if depth == 0 {
		// Only scan root, not its subdirectories.
		return s.scanDir(ctx, root, query)
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			errs = append(errs, FileError{Path: path, Err: walkErr})
			return nil // continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if !looksLikeAgentCard(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, FileError{Path: path, Err: err})
			return nil
		}

		// Schema validation: skip cards that don't pass unless the
		// query explicitly asks for them.
		schemaErrs, vErr := agentvalidate.Validate(ctx, data)
		valid := vErr == nil && len(schemaErrs) == 0

		a, parseErr := parseAgent(path, data)
		if parseErr != nil {
			if query.IncludeInvalid {
				a.Valid = false
				if matchesQuery(a, query) {
					matches = append(matches, a)
				}
			} else {
				errs = append(errs, FileError{Path: path, Err: parseErr})
			}
			return nil
		}
		a.Valid = valid

		if !valid && !query.IncludeInvalid {
			errs = append(errs, FileError{Path: path, Err: errors.New("schema validation failed")})
			return nil
		}

		if matchesQuery(a, query) {
			matches = append(matches, a)
		}
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		errs = append(errs, FileError{Path: root, Err: walkErr})
	}

	return matches, errs
}

// scanDir scans only the immediate directory (no recursion).
func (s *Searcher) scanDir(ctx context.Context, root string, query Query) ([]Agent, []FileError) {
	var (
		matches []Agent
		errs    []FileError
	)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, []FileError{{Path: root, Err: err}}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name())
		if !looksLikeAgentCard(path) {
			continue
		}
		if ctx.Err() != nil {
			return matches, errs
		}
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, FileError{Path: path, Err: err})
			continue
		}
		schemaErrs, vErr := agentvalidate.Validate(ctx, data)
		valid := vErr == nil && len(schemaErrs) == 0
		a, parseErr := parseAgent(path, data)
		if parseErr != nil {
			if query.IncludeInvalid {
				a.Valid = false
				if matchesQuery(a, query) {
					matches = append(matches, a)
				}
			} else {
				errs = append(errs, FileError{Path: path, Err: parseErr})
			}
			continue
		}
		a.Valid = valid
		if !valid && !query.IncludeInvalid {
			errs = append(errs, FileError{Path: path, Err: errors.New("schema validation failed")})
			continue
		}
		if matchesQuery(a, query) {
			matches = append(matches, a)
		}
	}
	return matches, errs
}

// looksLikeAgentCard returns true if a filename suggests it's an
// agent.json. We accept *.agent.json and agent.json (case-insensitive).
// Files that don't match are skipped without reading them.
func looksLikeAgentCard(path string) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	if lower == "agent.json" {
		return true
	}
	return strings.HasSuffix(lower, ".agent.json")
}

// matchesQuery checks whether a single agent matches the query.
// Centralised here so both scanDir and Search stay in sync.
func matchesQuery(a Agent, q Query) bool {
	for _, want := range q.Capabilities {
		if !a.HasCapability(want) {
			return false
		}
	}
	if q.NameSubstring != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(q.NameSubstring)) {
		return false
	}
	if q.HandleSubstring != "" && !strings.Contains(strings.ToLower(a.Handle), strings.ToLower(q.HandleSubstring)) {
		return false
	}
	if q.OwnerSubstring != "" && !strings.Contains(strings.ToLower(a.OwnerName), strings.ToLower(q.OwnerSubstring)) {
		return false
	}
	if q.TrustLevel != "" && !strings.EqualFold(a.TrustLevel, q.TrustLevel) {
		return false
	}
	for _, want := range q.Protocols {
		if !a.HasProtocol(want) {
			return false
		}
	}
	if q.HasCardURL && !strings.HasPrefix(a.CardURL, "http://") && !strings.HasPrefix(a.CardURL, "https://") {
		return false
	}
	if q.StaleThreshold > 0 {
		now := time.Now()
		if !a.IsStale(now, q.StaleThreshold) {
			return false
		}
	}
	return true
}

// FileError is a non-fatal I/O or parse error encountered during a
// search. The search continues with the next file; FileError lets
// callers surface what was skipped without aborting the run.
type FileError struct {
	Path string
	Err  error
}

// String renders the error in a grep-friendly form.
func (e FileError) String() string {
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}