package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EmbedFunc optionally embeds text for semantic memory retrieval.
type EmbedFunc func(ctx context.Context, text string) ([]float32, error)

// SQLiteStore persists L0–L3 memory in SQLite (tables agent_memory, agent_memory_embedding).
type SQLiteStore struct {
	db *sql.DB
	// Embed optionally adds the vector half of the hybrid retrieval. Without it the store still
	// retrieves with BM25, so the hybrid path is not dependent on a provider being configured.
	Embed EmbedFunc
	// Ret is the hybrid retriever: bootstrap layers plus BM25 + vector + RRF fallback.
	Ret *Retriever
	// Cap is the per-layer item limit.
	Cap LayerCap
}

// NewSQLiteStore creates a store backed by the given DB (same file as the main app DB).
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db, Ret: NewRetriever(nil), Cap: DefaultLayerCap()}
}

// EnsureSchema creates the memory tables. It is idempotent so a plugin that owns a memory seam
// can create its own storage without reaching into the app migration.
func (s *SQLiteStore) EnsureSchema() error {
	if s.db == nil {
		return fmt.Errorf("memory: no database")
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS agent_memory (
  id TEXT PRIMARY KEY,
  level INTEGER NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  project_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at INTEGER NOT NULL
);`,
		`CREATE INDEX IF NOT EXISTS idx_agent_memory_session_level ON agent_memory(session_id, level, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_agent_memory_project_level ON agent_memory(project_id, level, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_agent_memory_level_time ON agent_memory(level, created_at);`,
		`CREATE TABLE IF NOT EXISTS agent_memory_embedding (
  memory_id TEXT PRIMARY KEY REFERENCES agent_memory(id) ON DELETE CASCADE,
  dim INTEGER NOT NULL,
  embedding BLOB NOT NULL
);`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Append(ctx context.Context, sessionID, kind, content string) error {
	return s.AppendLevel(ctx, sessionID, "", int(LayerAtom), kind, content)
}

func (s *SQLiteStore) AppendLevel(ctx context.Context, sessionID, projectID string, level int, kind, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	layer := Layer(level)
	if layer < LayerConversation || layer > LayerPersona {
		return nil
	}
	switch layer {
	case LayerConversation, LayerAtom:
		if sessionID == "" {
			return nil
		}
	case LayerScenario:
		if projectID == "" {
			return nil
		}
	}
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_memory (id, level, session_id, project_id, kind, content, created_at) VALUES (?,?,?,?,?,?,?)`,
		id, int(layer), sessionID, projectID, kind, content, now,
	)
	if err != nil {
		return err
	}
	if s.Embed != nil {
		vec, err := s.Embed(ctx, content)
		if err == nil && len(vec) > 0 {
			_, _ = s.db.ExecContext(ctx,
				`INSERT OR REPLACE INTO agent_memory_embedding (memory_id, dim, embedding) VALUES (?,?,?)`,
				id, len(vec), float32Blob(vec),
			)
		}
	}
	return s.pruneLayer(ctx, layer, sessionID, projectID)
}

func (s *SQLiteStore) pruneLayer(ctx context.Context, layer Layer, sessionID, projectID string) error {
	limit := s.Cap.forLayer(layer)
	if limit <= 0 {
		return nil
	}
	var countQ string
	var delQ string
	var args []any
	switch layer {
	case LayerPersona:
		countQ = `SELECT COUNT(*) FROM agent_memory WHERE level=3`
		delQ = `DELETE FROM agent_memory WHERE id = (SELECT id FROM agent_memory WHERE level=3 ORDER BY created_at ASC LIMIT 1)`
	case LayerScenario:
		countQ = `SELECT COUNT(*) FROM agent_memory WHERE level=2 AND project_id=?`
		delQ = `DELETE FROM agent_memory WHERE id = (SELECT id FROM agent_memory WHERE level=2 AND project_id=? ORDER BY created_at ASC LIMIT 1)`
		args = []any{projectID}
	default:
		countQ = `SELECT COUNT(*) FROM agent_memory WHERE level=? AND session_id=?`
		delQ = `DELETE FROM agent_memory WHERE id = (SELECT id FROM agent_memory WHERE level=? AND session_id=? ORDER BY created_at ASC LIMIT 1)`
		args = []any{int(layer), sessionID}
	}
	for {
		var cnt int
		if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&cnt); err != nil {
			return err
		}
		if cnt <= limit {
			return nil
		}
		res, err := s.db.ExecContext(ctx, delQ, args...)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil
		}
	}
}

// Rows loads candidate rows for retrieval, embeddings included, newest first.
func (s *SQLiteStore) Rows(ctx context.Context, sessionID, projectID string, limit int) ([]Row, error) {
	if limit <= 0 {
		limit = 400
	}
	args := []any{projectID, sessionID, limit}
	rows, err := s.db.QueryContext(ctx, `
SELECT m.id, m.level, m.session_id, m.project_id, m.kind, m.content, m.created_at, e.embedding
FROM agent_memory m
LEFT JOIN agent_memory_embedding e ON e.memory_id = m.id
WHERE (m.level=3) OR (m.level=2 AND m.project_id=?) OR (m.level IN (0,1) AND m.session_id=?)
ORDER BY m.created_at ASC
LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var level int
		var emb []byte
		if err := rows.Scan(&r.ID, &level, &r.SessionID, &r.ProjectID, &r.Kind, &r.Content, &r.CreatedAt, &emb); err != nil {
			continue
		}
		r.Layer = Layer(level)
		r.Embedding = blobFloat32(emb)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Retrieve(ctx context.Context, sessionID, query string) ([]string, error) {
	return s.RetrieveLevels(ctx, sessionID, "", query)
}

// RetrieveLevels performs the layered hybrid retrieval: L2/L3 bootstrap, then BM25 + vector + RRF
// over L0/L1, capped by the retrieval budget.
func (s *SQLiteStore) RetrieveLevels(ctx context.Context, sessionID, projectID, query string) ([]string, error) {
	pool, err := s.Rows(ctx, sessionID, projectID, 400)
	if err != nil {
		return nil, err
	}
	if s.Ret == nil {
		s.Ret = NewRetriever(s.Embed)
	}
	s.Ret.Embed = s.Embed
	return s.Ret.Search(ctx, pool, query)
}
