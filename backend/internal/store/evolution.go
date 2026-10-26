package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
)

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
	kept := 0
	if r.KeptBase {
		kept = 1
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO harness_rounds(id, project_id, session_id, mode, k, adopted, kept_base, notes, inventory, always_on, errors_json, created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.SessionID, r.Mode, r.K, r.Adopted, kept, r.Notes, r.Inventory, r.AlwaysOn, string(errs), r.CreatedAt.Unix())
	return err
}

// ListHarnessRounds returns recent rounds for a project (empty project id means global scope).
func (s *Store) ListHarnessRounds(ctx context.Context, projectID string, limit int) ([]domain.HarnessRound, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, session_id, mode, k, adopted, kept_base, notes, inventory, always_on, errors_json, created_at
   FROM harness_rounds WHERE project_id = ? ORDER BY created_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HarnessRound
	for rows.Next() {
		var (
			id, pid, sid, mode, adopted, notes, inventory string
			k, alwaysOn, kept                             int
			errsJSON                                      string
			created                                       int64
		)
		if err := rows.Scan(&id, &pid, &sid, &mode, &k, &adopted, &kept, &notes, &inventory, &alwaysOn, &errsJSON, &created); err != nil {
			return nil, err
		}
		var errs []string
		_ = json.Unmarshal([]byte(errsJSON), &errs)
		out = append(out, domain.HarnessRound{
			ID: id, ProjectID: pid, SessionID: sid, Mode: mode, K: k, Adopted: adopted,
			KeptBase: kept == 1, Notes: notes, Inventory: inventory, AlwaysOn: alwaysOn,
			Errors: errs, CreatedAt: time.Unix(created, 0).UTC(),
		})
	}
	return out, rows.Err()
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
