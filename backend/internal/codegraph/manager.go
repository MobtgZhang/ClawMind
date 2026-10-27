package codegraph

import (
	"sync"
	"time"
)

// Manager holds one index per workspace root and refreshes it incrementally. An index is only
// worth keeping if the next turn can reuse it: rebuilding from scratch per message is what the
// manager exists to avoid.
type Manager struct {
	mu      sync.Mutex
	limits  Limits
	ttl     time.Duration
	indexes map[string]*Index
}

func NewManager(limits Limits, ttl time.Duration) *Manager {
	if limits.MaxFiles <= 0 {
		limits = DefaultLimits()
	}
	return &Manager{limits: limits, ttl: ttl, indexes: map[string]*Index{}}
}

// Get returns the index for a root, refreshing it when the freshness window has passed.
func (m *Manager) Get(root string) *Index {
	if m == nil || root == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	ix, ok := m.indexes[root]
	if !ok {
		ix = New(root, m.limits)
		m.indexes[root] = ix
		ix.Refresh(root)
		return ix
	}
	if m.ttl <= 0 || time.Since(ix.builtAt) >= m.ttl {
		ix.Refresh(root)
	}
	return ix
}

// Refresh forces a rebuild for a root, used after a change the model made to the workspace shape.
func (m *Manager) Refresh(root string) *Index {
	if m == nil || root == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ix, ok := m.indexes[root]
	if !ok {
		ix = New(root, m.limits)
		m.indexes[root] = ix
	}
	ix.Refresh(root)
	return ix
}
