package memory

import (
	"context"
	"strings"
	"sync"
)

// Store is the pluggable memory layer. The seam is deliberately narrow: a plugin can replace the
// storage backend without touching the agent.
//
// Layers: L0 conversation (raw), L1 atom, L2 scenario, L3 persona.
type Store interface {
	Append(ctx context.Context, sessionID, kind, content string) error
	Retrieve(ctx context.Context, sessionID, query string) ([]string, error)
	AppendLevel(ctx context.Context, sessionID, projectID string, level int, kind, content string) error
	RetrieveLevels(ctx context.Context, sessionID, projectID, query string) ([]string, error)
}

// LayerCap is the per-layer item limit. A layer that grows without a cap eventually pushes the
// rest of the context out, which is the failure mode this design is trying to prevent.
type LayerCap struct {
	L0 int
	L1 int
	L2 int
	L3 int
}

func DefaultLayerCap() LayerCap {
	return LayerCap{L0: 400, L1: 150, L2: 100, L3: 200}
}

func (c LayerCap) forLayer(layer Layer) int {
	switch layer {
	case LayerConversation:
		return c.L0
	case LayerScenario:
		return c.L2
	case LayerPersona:
		return c.L3
	default:
		return c.L1
	}
}

// ForLayer is the exported form: the layer census needs the cap next to the count, otherwise a
// full layer looks like a normal layer rather than one that is pushing the context out.
func (c LayerCap) ForLayer(layer Layer) int { return c.forLayer(layer) }

// SetLayer lowers one layer cap. Callers are expected to only lower a cap: a patch layer that could
// raise it would let a layer grow past the budget the cap exists to protect.
func (c *LayerCap) SetLayer(layer Layer, n int) {
	if n <= 0 {
		return
	}
	switch layer {
	case LayerConversation:
		c.L0 = n
	case LayerScenario:
		c.L2 = n
	case LayerPersona:
		c.L3 = n
	default:
		c.L1 = n
	}
}

// InMemoryStore keeps rows per session, an optional project bucket, and the global layer.
type InMemoryStore struct {
	mu       sync.RWMutex
	rows     map[string][]Row
	projRows map[string][]Row
	global   []Row
	// Cap is the per-layer limit this store enforces when trimming. It is exported so the boot
	// sequence can apply the memory layer: a cap the census reports but the store never applies is
	// the same declared-but-unconsumed failure the plugin seam is meant to prevent.
	Cap LayerCap
	Ret *Retriever
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		rows:     map[string][]Row{},
		projRows: map[string][]Row{},
		Cap:      DefaultLayerCap(),
		Ret:      NewRetriever(nil),
	}
}

func (s *InMemoryStore) Append(ctx context.Context, sessionID, kind, content string) error {
	return s.AppendLevel(ctx, sessionID, "", int(LayerAtom), kind, content)
}

func (s *InMemoryStore) AppendLevel(ctx context.Context, sessionID, projectID string, level int, kind, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	layer := Layer(level)
	if layer < LayerConversation || layer > LayerPersona {
		return nil
	}
	row := Row{Layer: layer, SessionID: sessionID, ProjectID: projectID, Kind: kind, Content: content}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch layer {
	case LayerPersona:
		s.global = append(s.global, row)
		s.global = s.trim(s.global, s.Cap.ForLayer(layer))
	case LayerScenario:
		if projectID == "" {
			return nil
		}
		s.projRows[projectID] = append(s.projRows[projectID], row)
		s.projRows[projectID] = s.trim(s.projRows[projectID], s.Cap.ForLayer(layer))
	default:
		if sessionID == "" {
			return nil
		}
		s.rows[sessionID] = append(s.rows[sessionID], row)
		s.rows[sessionID] = s.trim(s.rows[sessionID], s.Cap.ForLayer(layer))
	}
	return nil
}

func (s *InMemoryStore) trim(target []Row, limit int) []Row {
	if limit <= 0 || len(target) <= limit {
		return target
	}
	return target[len(target)-limit:]
}

func (s *InMemoryStore) Retrieve(ctx context.Context, sessionID, query string) ([]string, error) {
	return s.RetrieveLevels(ctx, sessionID, "", query)
}

// Rows returns the stored rows, newest last, so a pipeline can merge a layer with what is
// already there.
func (s *InMemoryStore) Rows(ctx context.Context, sessionID, projectID string, limit int) ([]Row, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	var pool []Row
	pool = append(pool, s.global...)
	if projectID != "" {
		pool = append(pool, s.projRows[projectID]...)
	}
	if sessionID != "" {
		pool = append(pool, s.rows[sessionID]...)
	}
	if limit > 0 && len(pool) > limit {
		pool = pool[len(pool)-limit:]
	}
	return pool, nil
}

func (s *InMemoryStore) RetrieveLevels(ctx context.Context, sessionID, projectID, query string) ([]string, error) {
	s.mu.RLock()
	var pool []Row
	pool = append(pool, s.global...)
	if projectID != "" {
		pool = append(pool, s.projRows[projectID]...)
	}
	if sessionID != "" {
		pool = append(pool, s.rows[sessionID]...)
	}
	s.mu.RUnlock()
	return s.Ret.Search(ctx, pool, query)
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
