package memory

import (
	"context"
	"strings"
	"sync"
)

// Store is a pluggable memory layer with L0–L3 tiers for the agent.
// L0: live conversation (not stored here). L1: session. L2: project. L3: global.
type Store interface {
	Append(ctx context.Context, sessionID, kind, content string) error
	Retrieve(ctx context.Context, sessionID, query string) ([]string, error)
	AppendLevel(ctx context.Context, sessionID, projectID string, level int, kind, content string) error
	RetrieveLevels(ctx context.Context, sessionID, projectID, query string) ([]string, error)
}

// InMemoryStore keeps rows per session, optional project bucket, and global.
type InMemoryStore struct {
	mu         sync.RWMutex
	rows       map[string][]memoryRow // sessionID -> L1-ish
	projRows   map[string][]memoryRow // projectID -> L2
	globalRows []memoryRow            // L3
}

type memoryRow struct {
	Kind    string
	Content string
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		rows:     make(map[string][]memoryRow),
		projRows: make(map[string][]memoryRow),
	}
}

func (s *InMemoryStore) Append(ctx context.Context, sessionID, kind, content string) error {
	return s.AppendLevel(ctx, sessionID, "", 1, kind, content)
}

func (s *InMemoryStore) AppendLevel(ctx context.Context, sessionID, projectID string, level int, kind, content string) error {
	_ = ctx
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	row := memoryRow{Kind: kind, Content: content}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch level {
	case 3:
		s.globalRows = append(s.globalRows, row)
		if len(s.globalRows) > 200 {
			s.globalRows = s.globalRows[len(s.globalRows)-200:]
		}
	case 2:
		if projectID == "" {
			return nil
		}
		s.projRows[projectID] = append(s.projRows[projectID], row)
		if len(s.projRows[projectID]) > 100 {
			s.projRows[projectID] = s.projRows[projectID][len(s.projRows[projectID])-100:]
		}
	default: // L1 / L0-style session scratch
		if sessionID == "" {
			return nil
		}
		s.rows[sessionID] = append(s.rows[sessionID], row)
		if len(s.rows[sessionID]) > 150 {
			s.rows[sessionID] = s.rows[sessionID][len(s.rows[sessionID])-150:]
		}
	}
	return nil
}

func (s *InMemoryStore) Retrieve(ctx context.Context, sessionID, query string) ([]string, error) {
	return s.RetrieveLevels(ctx, sessionID, "", query)
}

func (s *InMemoryStore) RetrieveLevels(ctx context.Context, sessionID, projectID, query string) ([]string, error) {
	_ = ctx
	q := strings.ToLower(strings.TrimSpace(query))
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	add := func(label string, rows []memoryRow) {
		for _, r := range rows {
			if q == "" || strings.Contains(strings.ToLower(r.Content), q) {
				out = append(out, label+" ["+r.Kind+"] "+r.Content)
			}
		}
	}
	add("[L3]", s.globalRows)
	if projectID != "" {
		add("[L2]", s.projRows[projectID])
	}
	if sessionID != "" {
		add("[L1]", s.rows[sessionID])
	}
	if len(out) > 40 {
		out = out[len(out)-40:]
	}
	return out, nil
}

// NoopStore implements Store with no persistence.
type NoopStore struct{}

func (NoopStore) Append(context.Context, string, string, string) error { return nil }
func (NoopStore) Retrieve(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (NoopStore) AppendLevel(context.Context, string, string, int, string, string) error {
	return nil
}
func (NoopStore) RetrieveLevels(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}
