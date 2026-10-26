package store

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
)

const harnessRoundColumns = `id, project_id, session_id, mode, k, adopted, kept_base, notes, inventory, always_on, errors_json, created_at,
 task_id, runmark, ledger_r, ledger_p, measured_r, pool_json, dropped_json, cached`

// CreateHarnessRound persists one Propose-Probe-Compose-Reflect step.
func (s *Store) CreateHarnessRound(ctx context.Context, r *domain.HarnessRound) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	errs, err := json.Marshal(r.Errors)
	if err != nil {
		errs = []byte("[]")
	}
	pool, err := json.Marshal(r.Pool)
	if err != nil {
		pool = []byte("[]")
	}
	dropped, err := json.Marshal(r.Dropped)
	if err != nil {
		dropped = []byte("[]")
	}
	kept, cached := 0, 0
	if r.KeptBase {
		kept = 1
	}
	if r.Cached {
		cached = 1
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO harness_rounds(`+harnessRoundColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.SessionID, r.Mode, r.K, r.Adopted, kept, r.Notes, r.Inventory, r.AlwaysOn, string(errs), r.CreatedAt.Unix(),
		r.TaskID, r.Runmark, r.LedgerR, r.LedgerP, r.MeasuredR, string(pool), string(dropped), cached)
	return err
}

// GetHarnessRound reads one round by id.
func (s *Store) GetHarnessRound(ctx context.Context, id string) (*domain.HarnessRound, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+harnessRoundColumns+` FROM harness_rounds WHERE id = ?`, id)
	return scanHarnessRound(row)
}

func scanHarnessRound(row *sql.Row) (*domain.HarnessRound, error) {
	var (
		id, pid, sid, mode, adopted, notes, inventory string
		k, alwaysOn, kept                             int
		errsJSON, poolJSON, droppedJSON               string
		taskID, runmark                               string
		ledgerR, ledgerP, measuredR, cached           int
		created                                       int64
	)
	if err := row.Scan(&id, &pid, &sid, &mode, &k, &adopted, &kept, &notes, &inventory, &alwaysOn, &errsJSON, &created,
		&taskID, &runmark, &ledgerR, &ledgerP, &measuredR, &poolJSON, &droppedJSON, &cached); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var errs, pool, dropped []string
	_ = json.Unmarshal([]byte(errsJSON), &errs)
	_ = json.Unmarshal([]byte(poolJSON), &pool)
	_ = json.Unmarshal([]byte(droppedJSON), &dropped)
	return &domain.HarnessRound{
		ID: id, ProjectID: pid, SessionID: sid, Mode: mode, K: k, Adopted: adopted,
		KeptBase: kept == 1, Notes: notes, Inventory: inventory, AlwaysOn: alwaysOn,
		Errors: errs, CreatedAt: time.Unix(created, 0).UTC(),
		TaskID: taskID, Runmark: runmark, LedgerR: ledgerR, LedgerP: ledgerP, MeasuredR: measuredR,
		Pool: pool, Dropped: dropped, Cached: cached == 1,
	}, nil
}

// ListHarnessRounds returns recent rounds for a project (empty project id means global scope).
func (s *Store) ListHarnessRounds(ctx context.Context, projectID string, limit int) ([]domain.HarnessRound, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+harnessRoundColumns+` FROM harness_rounds WHERE project_id = ? ORDER BY created_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HarnessRound
	for rows.Next() {
		var (
			id, pid, sid, mode, adopted, notes, inventory string
			k, alwaysOn, kept                             int
			errsJSON, poolJSON, droppedJSON               string
			taskID, runmark                               string
			ledgerR, ledgerP, measuredR, cached           int
			created                                       int64
		)
		if err := rows.Scan(&id, &pid, &sid, &mode, &k, &adopted, &kept, &notes, &inventory, &alwaysOn, &errsJSON, &created,
			&taskID, &runmark, &ledgerR, &ledgerP, &measuredR, &poolJSON, &droppedJSON, &cached); err != nil {
			return nil, err
		}
		var errs, pool, dropped []string
		_ = json.Unmarshal([]byte(errsJSON), &errs)
		_ = json.Unmarshal([]byte(poolJSON), &pool)
		_ = json.Unmarshal([]byte(droppedJSON), &dropped)
		out = append(out, domain.HarnessRound{
			ID: id, ProjectID: pid, SessionID: sid, Mode: mode, K: k, Adopted: adopted,
			KeptBase: kept == 1, Notes: notes, Inventory: inventory, AlwaysOn: alwaysOn,
			Errors: errs, CreatedAt: time.Unix(created, 0).UTC(),
			TaskID: taskID, Runmark: runmark, LedgerR: ledgerR, LedgerP: ledgerP, MeasuredR: measuredR,
			Pool: pool, Dropped: dropped, Cached: cached == 1,
		})
	}
	return out, rows.Err()
}

// AddHarnessLedger records the budget accounting for one round. The ledger is stored as it was
// recorded, not reconstructed afterwards, so a stage that never ran stays visible.
func (s *Store) AddHarnessLedger(ctx context.Context, roundID string, mode, taskID string, k int, entries []LedgerEntry) error {
	if roundID == "" {
		return nil
	}
	r, p, measured := 0, 0, 0
	for _, e := range entries {
		if e.Kind == "rollout" {
			r++
			if e.Measured {
				measured++
			}
		} else if e.Kind == "session" {
			p++
		}
	}
	body, err := json.Marshal(entries)
	if err != nil {
		body = []byte("[]")
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO harness_ledger(id, round_id, mode, task_id, k, r, p, measured_r, entries_json, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), roundID, mode, taskID, k, r, p, measured, string(body), time.Now().UTC().Unix())
	return err
}

// LedgerEntry is one persisted budget entry.
type LedgerEntry struct {
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Measured bool   `json:"measured"`
	Cached   bool   `json:"cached"`
	Attempts int    `json:"attempts"`
	Tokens   int    `json:"tokens"`
}

// ListHarnessLedger returns the recorded entries for one round.
func (s *Store) ListHarnessLedger(ctx context.Context, roundID string) ([]LedgerEntry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT entries_json FROM harness_ledger WHERE round_id = ? LIMIT 1`, roundID)
	var body string
	if err := row.Scan(&body); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var out []LedgerEntry
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Runmark is the persisted trial identity: task + harness + mode + k.
type Runmark struct {
	ID          string `json:"id"`
	TaskID      string `json:"taskId"`
	HarnessHash string `json:"harnessHash"`
	Mode        string `json:"mode"`
	K           int    `json:"k"`
	RoundID     string `json:"roundId"`
	Chosen      string `json:"chosen"`
	Completed   bool   `json:"completed"`
	Version     int    `json:"version"`
	CreatedAt   int64  `json:"createdAt"`
}

// SaveRunmark records that a trial finished. It is written only after the run returned normally.
func (s *Store) SaveRunmark(ctx context.Context, m *Runmark) error {
	if m == nil || m.TaskID == "" || m.HarnessHash == "" {
		return nil
	}
	if m.ID == "" {
		m.ID = RunmarkID(m.TaskID, m.HarnessHash, m.Mode, m.K)
	}
	if m.Version == 0 {
		m.Version = 1
	}
	now := time.Now().UTC().Unix()
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	completed := 0
	if m.Completed {
		completed = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO harness_runmarks(id, task_id, harness_hash, mode, k, round_id, chosen, completed, version, created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET round_id=excluded.round_id, chosen=excluded.chosen, completed=excluded.completed, created_at=excluded.created_at`,
		m.ID, m.TaskID, m.HarnessHash, m.Mode, m.K, m.RoundID, m.Chosen, completed, m.Version, m.CreatedAt)
	return err
}

// LookupRunmark returns the recorded trial for this exact configuration, or nil.
func (s *Store) LookupRunmark(ctx context.Context, taskID, harnessHash, mode string, k int) (*Runmark, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, task_id, harness_hash, mode, k, round_id, chosen, completed, version, created_at
 FROM harness_runmarks WHERE id = ? LIMIT 1`, RunmarkID(taskID, harnessHash, mode, k))
	var m Runmark
	var completed int
	if err := row.Scan(&m.ID, &m.TaskID, &m.HarnessHash, &m.Mode, &m.K, &m.RoundID, &m.Chosen, &completed, &m.Version, &m.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	m.Completed = completed == 1
	return &m, nil
}

// RunmarkID is the identity of a trial: a run produced by a different configuration is never reused.
func RunmarkID(taskID, harnessHash, mode string, k int) string {
	sum := sha1.Sum([]byte("1|" + taskID + "|" + harnessHash + "|" + mode + "|" + strconv.Itoa(k)))
	return hex.EncodeToString(sum[:])
}

// AddHarnessEdits records the per-edit verdict rows for one round.
func (s *Store) AddHarnessEdits(ctx context.Context, roundID string, edits []domain.HarnessEdit) error {
	if len(edits) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Unix()
	for _, e := range edits {
		if e.ID == "" {
			e.ID = uuid.NewString()
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO harness_edits(id, round_id, component, candidates, change, observed, verdict, created_at) VALUES(?,?,?,?,?,?,?,?)`,
			e.ID, roundID, e.Component, e.Candidates, e.Change, e.Observed, string(e.Verdict), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListHarnessEdits returns the edit rows recorded for one round.
func (s *Store) ListHarnessEdits(ctx context.Context, roundID string) ([]domain.HarnessEdit, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, round_id, component, candidates, change, observed, verdict, created_at
   FROM harness_edits WHERE round_id = ? ORDER BY created_at ASC`, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HarnessEdit
	for rows.Next() {
		var id, rid, comp, cands, change, observed, verdict string
		var created int64
		if err := rows.Scan(&id, &rid, &comp, &cands, &change, &observed, &verdict, &created); err != nil {
			return nil, err
		}
		out = append(out, domain.HarnessEdit{
			ID: id, RoundID: rid, Component: comp, Candidates: cands, Change: change,
			Observed: observed, Verdict: domain.Verdict(verdict), CreatedAt: time.Unix(created, 0).UTC(),
		})
	}
	return out, rows.Err()
}

// AddHarnessLesson stores one transferable lesson; duplicate cues are not inserted twice.
func (s *Store) AddHarnessLesson(ctx context.Context, l *domain.HarnessLesson) error {
	if l == nil || l.Cue == "" || l.Lesson == "" {
		return nil
	}
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}
	var exists string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM harness_lessons WHERE project_id = ? AND cue = ? AND lesson = ? LIMIT 1`,
		l.ProjectID, l.Cue, l.Lesson).Scan(&exists)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO harness_lessons(id, project_id, round_id, cue, lesson, evidence, confidence, created_at) VALUES(?,?,?,?,?,?,?,?)`,
		l.ID, l.ProjectID, l.RoundID, l.Cue, l.Lesson, l.Evidence, l.Confidence, l.CreatedAt.Unix())
	return err
}

// ListHarnessLessons returns recent lessons for a scope, newest first.
func (s *Store) ListHarnessLessons(ctx context.Context, projectID string, limit int) ([]domain.HarnessLesson, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, COALESCE(round_id, ''), cue, lesson, evidence, confidence, created_at
   FROM harness_lessons WHERE project_id = ? ORDER BY created_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HarnessLesson
	for rows.Next() {
		var id, pid, rid, cue, lesson, evidence, confidence string
		var created int64
		if err := rows.Scan(&id, &pid, &rid, &cue, &lesson, &evidence, &confidence, &created); err != nil {
			return nil, err
		}
		out = append(out, domain.HarnessLesson{
			ID: id, ProjectID: pid, RoundID: rid, Cue: cue, Lesson: lesson,
			Evidence: evidence, Confidence: confidence, CreatedAt: time.Unix(created, 0).UTC(),
		})
	}
	return out, rows.Err()
}
