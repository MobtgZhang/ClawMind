package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	_ "github.com/mattn/go-sqlite3"
)

// Store persists sessions, messages, and projects (SQLite3 via mattn/go-sqlite3).
// LLM 与界面设置见 .clawmind/config.json（clawmindcfg 包）。
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := path + "?_foreign_keys=1&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}'
);`,
		`CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  parts_json TEXT NOT NULL DEFAULT '[]',
  parent_message_id TEXT,
  branch_id TEXT
);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);`,
		`CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE sessions ADD COLUMN project_id TEXT REFERENCES projects(id) ON DELETE SET NULL`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	return nil
}

// --- projects ---

func (s *Store) CreateProject(ctx context.Context, title string) (*domain.Project, error) {
	now := time.Now().UTC()
	id := uuid.NewString()
	if title == "" {
		title = "新项目"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects(id, title, created_at, updated_at) VALUES(?,?,?,?)`,
		id, title, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, err
	}
	return &domain.Project{ID: id, Title: title, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, created_at, updated_at FROM projects ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var c, u int64
		if err := rows.Scan(&p.ID, &p.Title, &c, &u); err != nil {
			return nil, err
		}
		p.CreatedAt = time.UnixMilli(c).UTC()
		p.UpdatedAt = time.UnixMilli(u).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateProjectTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE projects SET title = ?, updated_at = ? WHERE id = ?`,
		title, time.Now().UTC().UnixMilli(), id)
	return err
}

// --- sessions ---

func (s *Store) CreateSession(ctx context.Context, model string, projectID *string) (*domain.Session, error) {
	now := time.Now().UTC()
	id := uuid.NewString()
	meta := map[string]string{}
	b, _ := json.Marshal(meta)
	var pid any
	if projectID != nil && *projectID != "" {
		pid = *projectID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions(id, title, model, project_id, created_at, updated_at, metadata_json) VALUES(?,?,?,?,?,?,?)`,
		id, "新对话", model, pid, now.UnixMilli(), now.UnixMilli(), string(b))
	if err != nil {
		return nil, err
	}
	sess := &domain.Session{
		ID: id, Title: "新对话", Model: model, CreatedAt: now, UpdatedAt: now, Metadata: meta,
	}
	if projectID != nil && *projectID != "" {
		v := *projectID
		sess.ProjectID = &v
	}
	return sess, nil
}

// ListSessions lists sessions; projectFilter nil = all; pointer to "unassigned" = project_id IS NULL; else match id.
func (s *Store) ListSessions(ctx context.Context, projectFilter *string) ([]domain.Session, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if projectFilter == nil {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, title, model, project_id, created_at, updated_at, metadata_json FROM sessions ORDER BY updated_at DESC`)
	} else if *projectFilter == "unassigned" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, title, model, project_id, created_at, updated_at, metadata_json FROM sessions WHERE project_id IS NULL ORDER BY updated_at DESC`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, title, model, project_id, created_at, updated_at, metadata_json FROM sessions WHERE project_id = ? ORDER BY updated_at DESC`,
			*projectFilter)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanSessions(rows)
}

func (s *Store) scanSessions(rows *sql.Rows) ([]domain.Session, error) {
	var out []domain.Session
	for rows.Next() {
		var sess domain.Session
		var created, updated int64
		var metaJSON string
		var pid sql.NullString
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Model, &pid, &created, &updated, &metaJSON); err != nil {
			return nil, err
		}
		sess.CreatedAt = time.UnixMilli(created).UTC()
		sess.UpdatedAt = time.UnixMilli(updated).UTC()
		_ = json.Unmarshal([]byte(metaJSON), &sess.Metadata)
		if pid.Valid {
			v := pid.String
			sess.ProjectID = &v
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, model, project_id, created_at, updated_at, metadata_json FROM sessions WHERE id = ?`, id)
	var sess domain.Session
	var created, updated int64
	var metaJSON string
	var pid sql.NullString
	if err := row.Scan(&sess.ID, &sess.Title, &sess.Model, &pid, &created, &updated, &metaJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	sess.CreatedAt = time.UnixMilli(created).UTC()
	sess.UpdatedAt = time.UnixMilli(updated).UTC()
	_ = json.Unmarshal([]byte(metaJSON), &sess.Metadata)
	if pid.Valid {
		v := pid.String
		sess.ProjectID = &v
	}
	return &sess, nil
}

func (s *Store) TouchSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now().UTC().UnixMilli(), id)
	return err
}

func (s *Store) UpdateSessionTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`,
		title, time.Now().UTC().UnixMilli(), id)
	return err
}

// PatchSessionMetadata merges keys into sessions.metadata_json（如 siblingChoice）。
func (s *Store) PatchSessionMetadata(ctx context.Context, sessionID string, patch map[string]string) error {
	if len(patch) == 0 {
		return nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT metadata_json FROM sessions WHERE id = ?`, sessionID)
	var raw string
	if err := row.Scan(&raw); err != nil {
		return err
	}
	meta := map[string]string{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &meta)
	}
	if meta == nil {
		meta = map[string]string{}
	}
	for k, v := range patch {
		meta[k] = v
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE sessions SET updated_at = ?, metadata_json = ? WHERE id = ?`,
		time.Now().UTC().UnixMilli(), string(b), sessionID)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) ListMessages(ctx context.Context, sessionID string) ([]domain.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, role, created_at, parts_json, parent_message_id, branch_id FROM messages WHERE session_id = ? ORDER BY created_at ASC`,
		sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var created int64
		var partsJSON string
		var parent, branch sql.NullString
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &created, &partsJSON, &parent, &branch); err != nil {
			return nil, err
		}
		m.CreatedAt = time.UnixMilli(created).UTC()
		if err := json.Unmarshal([]byte(partsJSON), &m.Parts); err != nil {
			m.Parts = nil
		}
		if parent.Valid {
			v := parent.String
			m.ParentMessageID = &v
		}
		if branch.Valid {
			v := branch.String
			m.BranchID = &v
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetMessage(ctx context.Context, id string) (*domain.Message, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, role, created_at, parts_json, parent_message_id, branch_id FROM messages WHERE id = ?`, id)
	var m domain.Message
	var created int64
	var partsJSON string
	var parent, branch sql.NullString
	if err := row.Scan(&m.ID, &m.SessionID, &m.Role, &created, &partsJSON, &parent, &branch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m.CreatedAt = time.UnixMilli(created).UTC()
	_ = json.Unmarshal([]byte(partsJSON), &m.Parts)
	if parent.Valid {
		v := parent.String
		m.ParentMessageID = &v
	}
	if branch.Valid {
		v := branch.String
		m.BranchID = &v
	}
	return &m, nil
}

func (s *Store) InsertMessage(ctx context.Context, m *domain.Message) error {
	b, err := json.Marshal(m.Parts)
	if err != nil {
		return err
	}
	var parent, branch any
	if m.ParentMessageID != nil {
		parent = *m.ParentMessageID
	}
	if m.BranchID != nil {
		branch = *m.BranchID
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO messages(id, session_id, role, created_at, parts_json, parent_message_id, branch_id) VALUES(?,?,?,?,?,?,?)`,
		m.ID, m.SessionID, string(m.Role), m.CreatedAt.UnixMilli(), string(b), parent, branch)
	return err
}

func (s *Store) UpdateMessageParts(ctx context.Context, id string, parts []domain.Part) error {
	b, err := json.Marshal(parts)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE messages SET parts_json = ? WHERE id = ?`, string(b), id)
	return err
}

// SessionProjectUpdater for FK safety — verify project exists before assign.
func (s *Store) ProjectExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// SetSessionProject sets session's project (nil or empty string clears).
func (s *Store) SetSessionProject(ctx context.Context, sessionID string, projectID *string) error {
	var pid any
	if projectID != nil && *projectID != "" {
		ok, err := s.ProjectExists(ctx, *projectID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("project not found")
		}
		pid = *projectID
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET project_id = ?, updated_at = ? WHERE id = ?`,
		pid, time.Now().UTC().UnixMilli(), sessionID)
	return err
}
