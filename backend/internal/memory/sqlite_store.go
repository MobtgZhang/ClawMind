package memory

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxL1Rows = 150
	maxL2Rows = 100
	maxL3Rows = 200
)

// EmbedFunc optionally embeds text for semantic memory retrieval.
type EmbedFunc func(ctx context.Context, text string) ([]float32, error)

// SQLiteStore persists L1–L3 memory in SQLite (tables agent_memory, agent_memory_embedding).
type SQLiteStore struct {
	db           *sql.DB
	Embed        EmbedFunc
	SemanticTopK int
}

// NewSQLiteStore creates a store backed by the given DB (same file as main app DB).
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db, SemanticTopK: 8}
}

func (s *SQLiteStore) semanticK() int {
	if s.SemanticTopK < 1 {
		return 8
	}
	if s.SemanticTopK > 40 {
		return 40
	}
	return s.SemanticTopK
}

func (s *SQLiteStore) Append(ctx context.Context, sessionID, kind, content string) error {
	return s.AppendLevel(ctx, sessionID, "", 1, kind, content)
}

func (s *SQLiteStore) AppendLevel(ctx context.Context, sessionID, projectID string, level int, kind, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	switch level {
	case 3:
	case 2:
		if projectID == "" {
			return nil
		}
	case 1, 0:
		if sessionID == "" {
			return nil
		}
		if level == 0 {
			level = 1
		}
	default:
		return nil
	}
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_memory (id, level, session_id, project_id, kind, content, created_at) VALUES (?,?,?,?,?,?,?)`,
		id, level, sessionID, projectID, kind, content, now,
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
	return s.pruneAfterAppend(ctx, level, sessionID, projectID)
}

func (s *SQLiteStore) pruneAfterAppend(ctx context.Context, level int, sessionID, projectID string) error {
	switch level {
	case 3:
		return s.pruneByQuery(ctx, `SELECT COUNT(*) FROM agent_memory WHERE level=3`, nil,
			`DELETE FROM agent_memory WHERE id = (SELECT id FROM agent_memory WHERE level=3 ORDER BY created_at ASC LIMIT 1)`, maxL3Rows)
	case 2:
		return s.pruneByQuery(ctx, `SELECT COUNT(*) FROM agent_memory WHERE level=2 AND project_id=?`, []any{projectID},
			`DELETE FROM agent_memory WHERE id = (SELECT id FROM agent_memory WHERE level=2 AND project_id=? ORDER BY created_at ASC LIMIT 1)`, maxL2Rows, projectID)
	default:
		return s.pruneByQuery(ctx, `SELECT COUNT(*) FROM agent_memory WHERE level=1 AND session_id=?`, []any{sessionID},
			`DELETE FROM agent_memory WHERE id = (SELECT id FROM agent_memory WHERE level=1 AND session_id=? ORDER BY created_at ASC LIMIT 1)`, maxL1Rows, sessionID)
	}
}

func (s *SQLiteStore) pruneByQuery(ctx context.Context, countQ string, countArgs []any, delQ string, limit int, delArgs ...any) error {
	for {
		row := s.db.QueryRowContext(ctx, countQ, countArgs...)
		var cnt int
		if err := row.Scan(&cnt); err != nil {
			return err
		}
		if cnt <= limit {
			return nil
		}
		res, err := s.db.ExecContext(ctx, delQ, delArgs...)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil
		}
	}
}

func (s *SQLiteStore) Retrieve(ctx context.Context, sessionID, query string) ([]string, error) {
	return s.RetrieveLevels(ctx, sessionID, "", query)
}

func (s *SQLiteStore) RetrieveLevels(ctx context.Context, sessionID, projectID, query string) ([]string, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if s.Embed != nil && q != "" {
		if sem, err := s.retrieveSemantic(ctx, sessionID, projectID, query); err == nil && len(sem) > 0 {
			return trimLines(sem, 40), nil
		}
	}
	return s.retrieveSubstring(ctx, sessionID, projectID, q)
}

func trimLines(out []string, max int) []string {
	if len(out) <= max {
		return out
	}
	return out[len(out)-max:]
}

type scoredLine struct {
	score float64
	line  string
}

func (s *SQLiteStore) retrieveSemantic(ctx context.Context, sessionID, projectID, query string) ([]string, error) {
	qvec, err := s.Embed(ctx, query)
	if err != nil || len(qvec) == 0 {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT m.id, m.level, m.kind, m.content, e.embedding
FROM agent_memory m
LEFT JOIN agent_memory_embedding e ON e.memory_id = m.id
WHERE m.level=3 OR (m.level=2 AND m.project_id=?) OR (m.level=1 AND m.session_id=?)
ORDER BY m.created_at DESC
LIMIT 400`, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scored []scoredLine
	qLower := strings.ToLower(strings.TrimSpace(query))
	for rows.Next() {
		var id string
		var level int
		var kind, content string
		var embBlob []byte
		if err := rows.Scan(&id, &level, &kind, &content, &embBlob); err != nil {
			continue
		}
		label := "[L1]"
		switch level {
		case 3:
			label = "[L3]"
		case 2:
			label = "[L2]"
		}
		line := label + " [" + kind + "] " + content
		vec := blobFloat32(embBlob)
		if len(vec) > 0 && len(vec) == len(qvec) {
			sc := cosineSim(qvec, vec)
			scored = append(scored, scoredLine{score: sc, line: line})
		} else if qLower != "" && strings.Contains(strings.ToLower(content), qLower) {
			scored = append(scored, scoredLine{score: 0.05, line: line})
		}
	}
	if len(scored) == 0 {
		return nil, nil
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	k := s.semanticK()
	if k > len(scored) {
		k = len(scored)
	}
	out := make([]string, 0, k)
	seen := make(map[string]struct{})
	for i := 0; i < len(scored) && len(out) < k; i++ {
		ln := scored[i].line
		if _, ok := seen[ln]; ok {
			continue
		}
		seen[ln] = struct{}{}
		out = append(out, ln)
	}
	return out, nil
}

func (s *SQLiteStore) retrieveSubstring(ctx context.Context, sessionID, projectID, query string) ([]string, error) {
	var out []string
	appendRows := func(label, q string, args ...any) error {
		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var kind, content string
			if err := rows.Scan(&kind, &content); err != nil {
				continue
			}
			if query == "" || strings.Contains(strings.ToLower(content), query) {
				out = append(out, label+" ["+kind+"] "+content)
			}
		}
		return rows.Err()
	}
	if err := appendRows("[L3]", `SELECT kind, content FROM agent_memory WHERE level=3 ORDER BY created_at ASC`); err != nil {
		return nil, err
	}
	if projectID != "" {
		if err := appendRows("[L2]", `SELECT kind, content FROM agent_memory WHERE level=2 AND project_id=? ORDER BY created_at ASC`, projectID); err != nil {
			return nil, err
		}
	}
	if sessionID != "" {
		if err := appendRows("[L1]", `SELECT kind, content FROM agent_memory WHERE level=1 AND session_id=? ORDER BY created_at ASC`, sessionID); err != nil {
			return nil, err
		}
	}
	return trimLines(out, 40), nil
}
