package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/store"
)

// Trusted in-process reader only. Returned current must not reenter Store or
// Server; it checks the independently frozen Runtime/verification authority.
type FinalEvidenceReader func(context.Context, store.Task, store.StageRun) (store.FinalEvidence, func() bool, error)

func (s *Server) SetFinalEvidenceReader(reader FinalEvidenceReader) error {
	if reader == nil {
		return errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finalEvidence != nil {
		return store.ErrConflict
	}
	s.finalEvidence = reader
	return nil
}

type HumanDecisionRequest struct {
	RunID          string `json:"run_id"`
	TextHash       string `json:"text_hash"`
	TreeHash       string `json:"tree_hash"`
	SpecHash       string `json:"spec_hash"`
	DesignHash     string `json:"design_hash"`
	AcceptanceHash string `json:"acceptance_hash"`
	Action         string `json:"action"`
	Reason         string `json:"reason"`
}
type HumanDecisionReply struct {
	Task        store.Task                     `json:"task"`
	Report      *store.FinalReport             `json:"report"`
	Decision    *store.HumanAcceptanceDecision `json:"decision"`
	Unavailable string                         `json:"unavailable"`
}

func decisionPath(path string) bool {
	return strings.HasPrefix(path, "/control/v1/tasks/") && strings.Contains(path, "/workflow/decision")
}
func readHumanDecision(r *http.Request) (HumanDecisionRequest, error) {
	var fields map[string]json.RawMessage
	if read(r, &fields) != nil || len(fields) != 8 {
		return HumanDecisionRequest{}, errInvalid
	}
	for _, k := range []string{"run_id", "text_hash", "tree_hash", "spec_hash", "design_hash", "acceptance_hash", "action", "reason"} {
		if _, ok := fields[k]; !ok {
			return HumanDecisionRequest{}, errInvalid
		}
	}
	raw, _ := json.Marshal(fields)
	var out HumanDecisionRequest
	if json.Unmarshal(raw, &out) != nil || !opaque(out.RunID) || !workflowHash(out.TextHash) || !workflowHash(out.TreeHash) || !workflowHash(out.SpecHash) || !workflowHash(out.DesignHash) || !workflowHash(out.AcceptanceHash) || out.Action != "accept" && out.Action != "return" || !utf8.ValidString(out.Reason) || strings.TrimSpace(out.Reason) == "" || len(out.Reason) > 8192 || strings.ContainsRune(out.Reason, 0) {
		return HumanDecisionRequest{}, errInvalid
	}
	return out, nil
}
func matchesFinalRequest(in HumanDecisionRequest, report store.FinalReport) bool {
	return in.RunID == report.RunID && in.TextHash == report.TextHash && in.TreeHash == report.TreeHash && in.SpecHash == report.SpecHash && in.DesignHash == report.DesignHash && in.AcceptanceHash == report.AcceptanceHash
}
func (s *Server) decisionControl(w http.ResponseWriter, r *http.Request) {
	if err := s.workflowAuthority(r); err != nil {
		controlFailure(w, err)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/control/v1/tasks/"), "/")
	if len(parts) != 3 || !opaque(parts[0]) || parts[1] != "workflow" || parts[2] != "decision" || r.URL.RawPath != "" || len(r.Header.Values("Idempotency-Key")) != 0 {
		controlFailure(w, errInvalid)
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	var condition taskCondition
	var in HumanDecisionRequest
	var err error
	if r.Method == "POST" {
		condition, err = taskIfMatch(r)
		if err == nil {
			in, err = readHumanDecision(r)
		}
	} else if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		err = e
		if len(b) > 0 {
			err = errInvalid
		}
	}
	if err != nil {
		controlFailure(w, err)
		return
	}
	s.mu.Lock()
	out, current, err := s.decisionLocked(r, parts[0], condition, in)
	s.mu.Unlock()
	if authErr := s.workflowAuthority(r); authErr != nil {
		controlFailure(w, authErr)
		return
	}
	if current != nil && !current() {
		controlFailure(w, control.ErrUnsupported)
		return
	}
	if err != nil {
		controlFailure(w, err)
		return
	}
	w.Header().Set("ETag", taskETag(out.Task))
	respond(w, 200, out)
}
func (s *Server) decisionLocked(r *http.Request, id string, condition taskCondition, in HumanDecisionRequest) (HumanDecisionReply, func() bool, error) {
	var out HumanDecisionReply
	if err := s.workflowAuthority(r); err != nil {
		return out, nil, err
	}
	task, err := s.store.Task(id)
	if err != nil {
		return out, nil, err
	}
	if _, ok := s.projects[task.ProjectID]; !ok {
		return out, nil, store.ErrNotFound
	}
	if r.Method == "POST" && taskETag(task) != condition.raw {
		return out, nil, errTaskPrecondition
	}
	out.Task = task
	historical, err := s.store.HumanDecision(id)
	if err == nil {
		out.Decision = &historical
	} else if !errors.Is(err, store.ErrNotFound) {
		return out, nil, err
	}
	version := store.TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}
	run, err := s.store.FinalAcceptanceRun(id, version)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return out, nil, err
	}
	if err != nil || s.finalEvidence == nil {
		out.Unavailable = "acceptance_pending"
		if s.finalEvidence == nil {
			out.Unavailable = "verification_reader_unavailable"
		}
		if r.Method == "POST" {
			return out, nil, store.ErrWorkflowGate
		}
		live, checkErr := s.store.Task(id)
		if checkErr != nil {
			return out, nil, checkErr
		}
		if live != task {
			return out, nil, errTaskPrecondition
		}
		return out, nil, nil
	}
	proof, authority, err := s.finalEvidence(r.Context(), task, run)
	if err != nil {
		return out, nil, err
	}
	current := func() bool { return authority != nil && authority() && r.Context().Err() == nil && proof.Current() }
	if !current() {
		return out, current, control.ErrUnsupported
	}
	report := proof.Report()
	if report.RunID != run.ID {
		return out, current, control.ErrIdentity
	}
	out.Report = &report
	if r.Method == "POST" {
		if !matchesFinalRequest(in, report) {
			return out, current, errTaskPrecondition
		}
		d, err := s.store.DecideHumanAcceptance(id, version, proof, in.Action, in.Reason, func() bool {
			return s.auth.ManagementCurrent(r.Context()) && authority != nil && authority() && r.Context().Err() == nil
		})
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				live, _ := s.store.Task(id)
				if live != task {
					err = errTaskPrecondition
				}
			}
			return out, current, err
		}
		out.Decision = &d
	}
	live, err := s.store.Task(id)
	if err != nil {
		return out, current, err
	}
	if r.Method == "GET" && live != task || r.Method == "POST" && (live.ID != task.ID || live.ProjectID != task.ProjectID || live.Goal != task.Goal || live.PlanRevision != task.PlanRevision || live.Generation != task.Generation) {
		return out, current, errTaskPrecondition
	}
	out.Task = live
	return out, current, nil
}
