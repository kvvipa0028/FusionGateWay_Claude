package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

var ErrWorkflowGate = errors.New("workflow requires review or design approval")
var ErrWorkflowAuthority = errors.New("workflow management authority unavailable")

type WorkflowDesign struct {
	RunID        string                  `json:"run_id"`
	PlanRevision int64                   `json:"plan_revision"`
	PlanHash     string                  `json:"plan_hash"`
	Generation   int64                   `json:"generation"`
	Snapshot     workflow.DesignSnapshot `json:"snapshot"`
}
type WorkflowApproval struct {
	PlanRevision   int64  `json:"plan_revision"`
	PlanHash       string `json:"plan_hash"`
	Generation     int64  `json:"generation"`
	DesignHash     string `json:"design_hash"`
	AcceptanceHash string `json:"acceptance_hash"`
}
type WorkflowView struct {
	TaskID     string              `json:"task_id"`
	Definition workflow.Definition `json:"definition"`
	Design     *WorkflowDesign     `json:"design"`
	Approval   *WorkflowApproval   `json:"approval"`
	Next       stageplan.Role      `json:"next"`
	Blocker    string              `json:"blocker"`
}
type workflowQuery interface {
	queryRow
	Query(string, ...any) (*sql.Rows, error)
}

func workflowJSON(raw, checksum string, out any) error {
	if hash([]byte(raw)) != checksum || json.Unmarshal([]byte(raw), out) != nil {
		return ErrWorkflowGate
	}
	b, e := json.Marshal(out)
	if e != nil || string(b) != raw {
		return ErrWorkflowGate
	}
	return nil
}
func workflowRoles(p stageplan.Snapshot, d workflow.Definition) bool {
	if stageplan.VerifySnapshot(p) != nil || len(p.RequiredRoles) != len(d.RequiredRoles) {
		return false
	}
	for _, role := range d.RequiredRoles {
		if _, ok := p.Bindings[role]; !ok {
			return false
		}
	}
	return true
}
func workflowRunStopped(q queryRow, r StageRun) bool {
	if r.State != "succeeded" || r.Owner != "" || !r.StartupIntent || !r.LaunchConfirmed {
		return false
	}
	var proof string
	if q.QueryRow("SELECT stop_proof_hash FROM reservations WHERE run_id=? AND state='released'", r.ID).Scan(&proof) != nil {
		return false
	}
	if len(proof) != 64 {
		return false
	}
	for _, c := range proof {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func workflowIn(q workflowQuery, t Task) (WorkflowView, error) {
	v := WorkflowView{TaskID: t.ID}
	var project, goal, raw, checksum string
	e := q.QueryRow("SELECT project_id,goal,definition_json,definition_hash FROM task_workflows WHERE task_id=?", t.ID).Scan(&project, &goal, &raw, &checksum)
	if errors.Is(e, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	if project != t.ProjectID || goal != t.Goal || workflowJSON(raw, checksum, &v.Definition) != nil || workflow.VerifyDefinition(v.Definition) != nil {
		return v, ErrWorkflowGate
	}
	p, e := planIn(q, t.ID, t.PlanRevision)
	if e != nil || !workflowRoles(p, v.Definition) {
		return v, ErrWorkflowGate
	}
	var designRunID string
	e = q.QueryRow("SELECT run_id,design_json,design_hash FROM workflow_designs WHERE task_id=?", t.ID).Scan(&designRunID, &raw, &checksum)
	if e == nil {
		d := WorkflowDesign{}
		if workflowJSON(raw, checksum, &d) != nil || workflow.VerifyDesign(d.Snapshot) != nil || d.Snapshot.Document.Goal != t.Goal || d.RunID != designRunID {
			return v, ErrWorkflowGate
		}
		r, err := runIn(q, d.RunID)
		if err != nil || r.TaskID != t.ID || r.Role != stageplan.Design || r.PlanRevision != d.PlanRevision || r.Generation != d.Generation || !workflowRunStopped(q, r) {
			return v, ErrWorkflowGate
		}
		original, err := planIn(q, t.ID, d.PlanRevision)
		if err != nil || original.Hash != d.PlanHash {
			return v, ErrWorkflowGate
		}
		v.Design = &d
	} else if !errors.Is(e, sql.ErrNoRows) {
		return v, e
	}
	e = q.QueryRow("SELECT approval_json,approval_hash FROM workflow_approvals WHERE task_id=? AND plan_revision=?", t.ID, t.PlanRevision).Scan(&raw, &checksum)
	if e == nil {
		a := WorkflowApproval{}
		if workflowJSON(raw, checksum, &a) != nil || v.Design == nil || a.PlanRevision != t.PlanRevision || a.PlanHash != p.Hash || a.Generation < v.Design.Generation || a.Generation > t.Generation || a.DesignHash != v.Design.Snapshot.Hash || a.AcceptanceHash != v.Design.Snapshot.AcceptanceHash {
			return v, ErrWorkflowGate
		}
		v.Approval = &a
	} else if !errors.Is(e, sql.ErrNoRows) {
		return v, e
	}
	rows, e := q.Query("SELECT id FROM stage_runs WHERE task_id=? ORDER BY generation", t.ID)
	if e != nil {
		return v, e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return v, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	sequence, e := workflowSequenceIn(q, t, v)
	if e != nil {
		return v, e
	}
	if len(ids) > len(sequence) {
		return v, ErrWorkflowGate
	}
	var generation int64
	for n, id := range ids {
		r, err := runIn(q, id)
		if err != nil || r.TaskID != t.ID || r.Role != sequence[n] || r.Generation <= generation || r.Generation > t.Generation {
			return v, ErrWorkflowGate
		}
		generation = r.Generation
		history, err := planIn(q, t.ID, r.PlanRevision)
		if err != nil {
			return v, ErrWorkflowGate
		}
		binding, ok := history.Bindings[r.Role]
		if !ok {
			return v, ErrWorkflowGate
		}
		match := false
		if binding.Target != nil && reflect.DeepEqual(*binding.Target, r.Target) {
			match = true
		}
		for _, target := range binding.Candidates {
			if reflect.DeepEqual(target, r.Target) {
				match = true
			}
		}
		if !match {
			return v, ErrWorkflowGate
		}
		if !workflowRunStopped(q, r) {
			switch r.State {
			case "starting", "running", "cancelling":
				v.Blocker = "stage_active"
			case "succeeded":
				v.Blocker = "stop_unverified"
			case "unknown":
				v.Blocker = "stage_unverified"
			default:
				v.Blocker = "stage_failed"
			}
			return v, nil
		}
	}
	if len(ids) > 0 && v.Definition.RequiredRoles[0] == stageplan.Design && v.Design == nil {
		v.Blocker = "design_required"
		return v, nil
	}
	if len(ids) == len(sequence) {
		v.Blocker = "workflow_complete"
		return v, nil
	}
	v.Next = sequence[len(ids)]
	if len(ids) > 0 && v.Definition.RequiredRoles[0] == stageplan.Design && v.Approval == nil {
		v.Blocker = "approval_required"
	}
	if t.State != "ready" {
		v.Blocker = "task_not_ready"
	}
	return v, nil
}
func (s *Store) Workflow(taskID string) (WorkflowView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return WorkflowView{}, ErrClosed
	}
	t, e := taskIn(s.db, taskID)
	if e != nil {
		return WorkflowView{}, e
	}
	return workflowIn(s.db, t)
}
func (s *Store) AttachWorkflow(taskID string, expected TaskVersion, kind workflow.Kind) (WorkflowView, error) {
	return s.AttachWorkflowAuthorized(taskID, expected, kind, func() bool { return true })
}

func (s *Store) AttachWorkflowAuthorized(taskID string, expected TaskVersion, kind workflow.Kind, current func() bool) (WorkflowView, error) {
	var out WorkflowView
	definition, e := workflow.DefinitionFor(kind)
	if e != nil {
		return out, ErrInvalid
	}
	e = s.workflowTransaction(current, func(tx *sql.Tx) error {
		t, err := taskVersionIn(tx, taskID, expected)
		if err != nil {
			return err
		}
		old, err := workflowIn(tx, t)
		if err == nil {
			if old.Definition.Kind != kind {
				return ErrConflict
			}
			out = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if t.State != "ready" || t.Generation != 0 {
			return ErrConflict
		}
		p, err := planIn(tx, t.ID, t.PlanRevision)
		if err != nil {
			return err
		}
		if !workflowRoles(p, definition) {
			return ErrConflict
		}
		raw, _ := json.Marshal(definition)
		if _, err = tx.Exec("INSERT INTO task_workflows VALUES(?,?,?,?,?)", t.ID, t.ProjectID, t.Goal, string(raw), hash(raw)); err != nil {
			return ErrConflict
		}
		if err = event(tx, t.ID, "workflow_attached", "", t.Generation); err != nil {
			return err
		}
		out, err = workflowIn(tx, t)
		return err
	})
	if e != nil {
		return WorkflowView{}, e
	}
	return out, nil
}
func (s *Store) SaveWorkflowDesign(taskID string, expected TaskVersion, runID string, doc workflow.DesignDocument) (WorkflowView, error) {
	return s.SaveWorkflowDesignAuthorized(taskID, expected, runID, doc, func() bool { return true })
}

func (s *Store) SaveWorkflowDesignAuthorized(taskID string, expected TaskVersion, runID string, doc workflow.DesignDocument, current func() bool) (WorkflowView, error) {
	var out WorkflowView
	snapshot, e := workflow.FreezeDesign(doc)
	if e != nil || !opaque(runID) {
		return out, ErrInvalid
	}
	e = s.workflowTransaction(current, func(tx *sql.Tx) error {
		t, err := taskVersionIn(tx, taskID, expected)
		if err != nil {
			return err
		}
		if t.State != "ready" {
			return ErrConflict
		}
		v, err := workflowIn(tx, t)
		if err != nil {
			return err
		}
		if doc.Goal != t.Goal {
			return ErrConflict
		}
		if v.Design != nil {
			if v.Design.RunID != runID || !reflect.DeepEqual(v.Design.Snapshot, snapshot) {
				return ErrConflict
			}
			out = v
			return nil
		}
		if v.Blocker != "design_required" {
			return ErrWorkflowGate
		}
		r, err := runIn(tx, runID)
		if err != nil || r.TaskID != t.ID || r.Role != stageplan.Design || r.Generation > t.Generation || !workflowRunStopped(tx, r) {
			return ErrWorkflowGate
		}
		p, err := planIn(tx, t.ID, r.PlanRevision)
		if err != nil {
			return err
		}
		d := WorkflowDesign{RunID: r.ID, PlanRevision: r.PlanRevision, PlanHash: p.Hash, Generation: r.Generation, Snapshot: snapshot}
		raw, _ := json.Marshal(d)
		if _, err = tx.Exec("INSERT INTO workflow_designs VALUES(?,?,?,?)", t.ID, r.ID, string(raw), hash(raw)); err != nil {
			return err
		}
		if err = event(tx, t.ID, "design_frozen", r.ID, t.Generation); err != nil {
			return err
		}
		out, err = workflowIn(tx, t)
		return err
	})
	if e != nil {
		return WorkflowView{}, e
	}
	return out, nil
}

// ApproveWorkflowDesign is a trusted human-authority operation. A model
// output, Worker grant or plain "continue" must never call it as authorization.
func (s *Store) ApproveWorkflowDesign(taskID string, expected TaskVersion, designHash, criteriaHash string) (WorkflowView, error) {
	return s.ApproveWorkflowDesignAuthorized(taskID, expected, designHash, criteriaHash, func() bool { return true })
}

func (s *Store) ApproveWorkflowDesignAuthorized(taskID string, expected TaskVersion, designHash, criteriaHash string, current func() bool) (WorkflowView, error) {
	var out WorkflowView
	e := s.workflowTransaction(current, func(tx *sql.Tx) error {
		t, err := taskVersionIn(tx, taskID, expected)
		if err != nil {
			return err
		}
		if t.State != "ready" {
			return ErrConflict
		}
		v, err := workflowIn(tx, t)
		if err != nil {
			return err
		}
		if v.Design == nil || v.Design.Snapshot.Hash != designHash || v.Design.Snapshot.AcceptanceHash != criteriaHash {
			return ErrConflict
		}
		if v.Approval != nil {
			out = v
			return nil
		}
		if v.Blocker != "approval_required" {
			return ErrWorkflowGate
		}
		p, err := planIn(tx, t.ID, t.PlanRevision)
		if err != nil {
			return err
		}
		a := WorkflowApproval{t.PlanRevision, p.Hash, t.Generation, designHash, criteriaHash}
		raw, _ := json.Marshal(a)
		if _, err = tx.Exec("INSERT INTO workflow_approvals VALUES(?,?,?,?)", t.ID, t.PlanRevision, string(raw), hash(raw)); err != nil {
			return err
		}
		if err = event(tx, t.ID, "design_approved", v.Design.RunID, t.Generation); err != nil {
			return err
		}
		out, err = workflowIn(tx, t)
		return err
	})
	if e != nil {
		return WorkflowView{}, e
	}
	return out, nil
}
func workflowStartIn(tx *sql.Tx, t Task, role stageplan.Role) error {
	v, e := workflowIn(tx, t)
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if v.Blocker != "" || v.Next != role {
		return ErrWorkflowGate
	}
	return nil
}

// ValidateWorkflowStart rejects a blocked workflow before target selection or
// Runtime preparation. startIntentIn repeats the gate in the write transaction.
func (s *Store) ValidateWorkflowStart(in StartIdentity) error {
	if !validStartIdentity(in) {
		return ErrInvalid
	}
	return s.transaction(func(tx *sql.Tx) error {
		t, e := taskVersionIn(tx, in.TaskID, TaskVersion{in.PlanRevision, in.Generation, "ready"})
		if e != nil {
			return e
		}
		return workflowStartIn(tx, t, in.Role)
	})
}

// current must be a local, nonblocking authority check that does not reenter
// Store. Recheck under its mutex and before commit so waiting/revoked requests
// cannot leave partial design or approval decisions.
func (s *Store) workflowTransaction(current func() bool, write func(*sql.Tx) error) error {
	return s.transaction(func(tx *sql.Tx) error {
		if current == nil || !current() {
			return ErrWorkflowAuthority
		}
		if e := write(tx); e != nil {
			return e
		}
		if !current() {
			return ErrWorkflowAuthority
		}
		return nil
	})
}
