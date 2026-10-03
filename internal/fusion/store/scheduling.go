package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrBudget = errors.New("shared task budget exhausted")

type Budget struct {
	MaxCalls    int `json:"max_calls"`
	MaxReworks  int `json:"max_reworks"`
	UsedCalls   int `json:"used_calls"`
	UsedReworks int `json:"used_reworks"`
}
type ReservationRequest struct {
	PoolKey, WriteKey, AdmissionHash string
	GlobalLimit                      int
}

func budgetIn(q queryRow, taskID string) (Budget, error) {
	var b Budget
	e := q.QueryRow("SELECT max_calls,max_reworks,used_calls,used_reworks FROM task_budgets WHERE task_id=?", taskID).Scan(&b.MaxCalls, &b.MaxReworks, &b.UsedCalls, &b.UsedReworks)
	if errors.Is(e, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, e
}
func (s *Store) Budget(taskID string) (Budget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return Budget{}, ErrClosed
	}
	return budgetIn(s.db, taskID)
}
func (s *Store) ConfigureBudget(taskID string, b Budget) error {
	if !initialBudget(b) {
		return ErrInvalid
	}
	return s.transaction(func(tx *sql.Tx) error {
		old, e := budgetIn(tx, taskID)
		if e == nil {
			if old.MaxCalls == b.MaxCalls && old.MaxReworks == b.MaxReworks {
				return nil
			}
			return ErrConflict
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		t, e := taskIn(tx, taskID)
		if e != nil {
			return e
		}
		if t.State != "ready" || t.Generation != 0 {
			return ErrConflict
		}
		_, e = tx.Exec("INSERT INTO task_budgets(task_id,max_calls,max_reworks) VALUES(?,?,?)", taskID, b.MaxCalls, b.MaxReworks)
		return e
	})
}
func initialBudget(b Budget) bool {
	return b.MaxCalls >= 1 && b.MaxCalls <= 1000 && b.MaxReworks >= 0 && b.MaxReworks <= 1 && b.UsedCalls == 0 && b.UsedReworks == 0
}
func (s *Store) ConfigureCapacity(limit int) error {
	if limit < 1 || limit > 16 {
		return ErrInvalid
	}
	return s.transaction(func(tx *sql.Tx) error {
		var active int
		if e := tx.QueryRow("SELECT COUNT(*) FROM reservations WHERE state='held'").Scan(&active); e != nil {
			return e
		}
		if active != 0 {
			return ErrConflict
		}
		_, e := tx.Exec("UPDATE controller_policy SET max_active=? WHERE id=1", limit)
		return e
	})
}

func (s *Store) Capacity() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return 0, ErrClosed
	}
	var limit int
	e := s.db.QueryRow("SELECT max_active FROM controller_policy WHERE id=1").Scan(&limit)
	return limit, e
}
func (s *Store) StartReserved(in StartRequest, req ReservationRequest) (StageRun, error) {
	// Legacy callers have no Created discriminator and cannot safely consume
	// an idempotent replay as fresh authorization to launch.
	if in.IdempotencyKey != "" {
		return StageRun{}, ErrInvalid
	}
	r, e := s.startReserved(in, req)
	return r.Run, e
}
func (s *Store) startReserved(in StartRequest, req ReservationRequest) (StartReceipt, error) {
	var result StageRun
	created := false
	if !opaque(in.Owner) || in.TTL <= 0 || in.TTL > time.Minute || !opaque(req.PoolKey) || req.WriteKey != "" && !opaque(req.WriteKey) || !validHash(req.AdmissionHash) || in.ExpectedGeneration != nil && *in.ExpectedGeneration < 0 {
		return StartReceipt{}, ErrInvalid
	}
	e := s.transaction(func(tx *sql.Tx) error {
		if in.IdempotencyKey != "" {
			old, e := lookupStartIn(tx, in.IdempotencyKey, startIdentity(in))
			if e == nil {
				requested, _ := json.Marshal(in.Target)
				frozen, _ := json.Marshal(old.Target)
				if string(requested) != string(frozen) {
					return ErrConflict
				}
				result = old
				return nil
			}
			if !errors.Is(e, ErrNotFound) {
				return e
			}
		}
		var limit, active int
		if e := tx.QueryRow("SELECT max_active FROM controller_policy WHERE id=1").Scan(&limit); e != nil {
			return e
		}
		if req.GlobalLimit != limit {
			return ErrInvalid
		}
		if e := tx.QueryRow("SELECT COUNT(*) FROM reservations WHERE state='held'").Scan(&active); e != nil {
			return e
		}
		if active >= limit {
			return ErrConflict
		}
		var conflict int
		if e := tx.QueryRow("SELECT COUNT(*) FROM reservations WHERE state='held' AND (pool_key=? OR (?<>'' AND write_key=?))", req.PoolKey, req.WriteKey, req.WriteKey).Scan(&conflict); e != nil {
			return e
		}
		if conflict > 0 {
			return ErrConflict
		}
		if e := tx.QueryRow("SELECT COUNT(*) FROM reservations JOIN stage_runs ON stage_runs.id=reservations.run_id WHERE reservations.state='held' AND stage_runs.task_id=?", in.TaskID).Scan(&conflict); e != nil {
			return e
		}
		if conflict > 0 {
			return ErrConflict
		}
		b, e := budgetIn(tx, in.TaskID)
		if e != nil {
			return e
		}
		if b.UsedCalls >= b.MaxCalls {
			return ErrBudget
		}
		if e = s.startIntentIn(tx, in, &result); e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO reservations(run_id,pool_key,write_key,state,admission_hash) VALUES(?,?,?,'held',?)", result.ID, req.PoolKey, req.WriteKey, req.AdmissionHash); e != nil {
			return e
		}
		if in.IdempotencyKey != "" {
			if _, e = tx.Exec("INSERT INTO start_requests(key,task_id,payload_hash,run_id) VALUES(?,?,?,?)", in.IdempotencyKey, result.TaskID, startHash(startIdentity(in)), result.ID); e != nil {
				return e
			}
		}
		created = true
		return event(tx, result.TaskID, "reservation_held", result.ID, result.Generation)
	})
	if e != nil {
		return StartReceipt{}, e
	}
	return StartReceipt{Run: result, Created: created}, nil
}
func (s *Store) Reservation(runID string) (ReservationRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ReservationRequest{}, ErrClosed
	}
	var r ReservationRequest
	e := s.db.QueryRow("SELECT pool_key,write_key,admission_hash FROM reservations WHERE run_id=? AND state='held'", runID).Scan(&r.PoolKey, &r.WriteKey, &r.AdmissionHash)
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, e
}
func (s *Store) ReserveCall(runID string, gen int64) error {
	return s.transaction(func(tx *sql.Tx) error {
		r, e := runIn(tx, runID)
		if e != nil {
			return e
		}
		r, e = s.fenced(tx, runID, gen, r.Owner)
		if e != nil {
			return e
		}
		if r.State != "running" {
			return ErrFenced
		}
		var held int
		if e = tx.QueryRow("SELECT COUNT(*) FROM reservations WHERE run_id=? AND state='held'", runID).Scan(&held); e != nil {
			return e
		}
		if held != 1 {
			return ErrFenced
		}
		b, e := budgetIn(tx, r.TaskID)
		if e != nil {
			return e
		}
		if b.UsedCalls >= b.MaxCalls {
			return ErrBudget
		}
		if _, e = tx.Exec("UPDATE task_budgets SET used_calls=used_calls+1 WHERE task_id=?", r.TaskID); e != nil {
			return e
		}
		return event(tx, r.TaskID, "model_call_reserved", runID, gen)
	})
}
func (s *Store) ReserveRework(taskID string, gen int64) error {
	return s.transaction(func(tx *sql.Tx) error {
		t, e := taskIn(tx, taskID)
		if e != nil {
			return e
		}
		if t.Generation != gen || gen < 1 || t.State == "completed" {
			return ErrFenced
		}
		b, e := budgetIn(tx, taskID)
		if e != nil {
			return e
		}
		if b.UsedReworks >= b.MaxReworks {
			return ErrBudget
		}
		if _, e = tx.Exec("UPDATE task_budgets SET used_reworks=used_reworks+1 WHERE task_id=?", taskID); e != nil {
			return e
		}
		return event(tx, taskID, "rework_reserved", "", gen)
	})
}

// stoppedVerified is a trusted supervisor verdict, never a request flag. The
// Scheduler requires its native identity/descendant verifier before calling.
func (s *Store) ReleaseReserved(runID string, gen int64, proofHash string, stoppedVerified bool) error {
	if !stoppedVerified || !validHash(proofHash) {
		return ErrInvalid
	}
	return s.transaction(func(tx *sql.Tx) error {
		r, e := runIn(tx, runID)
		if e != nil {
			return e
		}
		if r.Generation != gen || (r.State != "succeeded" && r.State != "failed" && r.State != "cancelled" && r.State != "advisory_only" && r.State != "interrupted") {
			return ErrFenced
		}
		var old string
		e = tx.QueryRow("SELECT stop_proof_hash FROM reservations WHERE run_id=?", runID).Scan(&old)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if old != "" {
			if old == proofHash {
				return nil
			}
			return ErrConflict
		}
		if _, e = tx.Exec("UPDATE reservations SET state='released',stop_proof_hash=? WHERE run_id=?", proofHash, runID); e != nil {
			return e
		}
		if e = event(tx, r.TaskID, "reservation_released", runID, gen); e != nil {
			return e
		}
		task, e := taskIn(tx, r.TaskID)
		if e != nil {
			return e
		}
		if task.State == "pausing" && task.Generation == gen {
			state, kind := "needs_review", "task_pause_requires_review"
			if r.State == "succeeded" {
				quiet, e := taskQuiescentIn(tx, r.TaskID)
				if e != nil {
					return e
				}
				if quiet {
					state, kind = "paused", "task_paused"
				}
			}
			if _, e = tx.Exec("UPDATE tasks SET state=? WHERE id=?", state, r.TaskID); e != nil {
				return e
			}
			return event(tx, r.TaskID, kind, runID, gen)
		}
		return nil
	})
}
