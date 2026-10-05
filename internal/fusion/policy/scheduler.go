package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

type Blocker struct {
	Code string `json:"code"`
}

func (b *Blocker) Error() string { return "execution blocked: " + b.Code }
func blocked(code string) error  { return &Blocker{Code: code} }

// Inspection is collected by trusted route, workspace and verification
// services. Neither these verdicts nor quota Meta may come from a task body.
type Inspection struct {
	Route                                                                               stageplan.Route
	Provider                                                                            string
	Quota                                                                               quota.Snapshot
	QueryAdmitted, DataAllowed, SandboxVerified, VerificationAvailable, AllCallsCounted bool
	ManagedExecutions                                                                   int
	WriteKey                                                                            string
	ProofHash                                                                           string
}
type StopProof struct {
	RunID                                            string
	Generation                                       int64
	NativeSessionID, ProcessIdentityHash, ReportHash string
	DescendantsStopped                               bool
}
type Scheduler struct {
	Store      *store.Store
	Inspect    func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Inspection, error)
	VerifyStop func(StopProof) bool
}

func digest(v any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func digestValue(v string) bool { raw, e := hex.DecodeString(v); return e == nil && len(raw) == 32 }
func physicalPool(p quota.Pool) string {
	return digest(struct{ ID, Provider, Region, Scope, Owner string }{p.ID, p.Provider, p.Region, p.Scope, p.Owner})
}

func (s *Scheduler) inspect(ctx context.Context, t store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (Inspection, error) {
	if s == nil || s.Store == nil || s.Inspect == nil {
		return Inspection{}, blocked("admission_unavailable")
	}
	in, e := s.Inspect(ctx, t, role, copyTarget(target))
	if e != nil {
		return Inspection{}, blocked("admission_unavailable")
	}
	if _, e = currentTarget(in.Route, target); e != nil {
		return in, blocked("route_changed")
	}
	if !in.DataAllowed {
		return in, blocked("data_permission_denied")
	}
	if !in.SandboxVerified {
		return in, blocked("sandbox_unverified")
	}
	if !in.VerificationAvailable {
		return in, blocked("verification_unavailable")
	}
	if !in.AllCallsCounted || in.ManagedExecutions != 1 {
		return in, blocked("internal_execution_uncontrolled")
	}
	if !digestValue(in.ProofHash) {
		return in, blocked("admission_proof_missing")
	}
	if in.WriteKey != "" && (role != stageplan.Implementation && role != stageplan.Testing) {
		return in, blocked("write_permission_denied")
	}
	if !in.QueryAdmitted {
		return in, blocked("quota_query_not_admitted")
	}
	q := in.Quota
	if q.Identity.Provider != in.Provider || q.Identity.Account != target.Account || q.Identity.Workspace != target.Workspace || q.Identity.Generation < 1 {
		return in, blocked("quota_identity_mismatch")
	}
	if status := quota.State(q, time.Now(), time.Minute); status != quota.Available {
		return in, blocked("quota_" + string(status))
	}
	if ctx.Err() != nil {
		return in, blocked("cancelled")
	}
	return in, nil
}

// Prepare commits both the startup intent and capacity/write reservation before
// external spawning. The exact frozen target is resolved by the server caller.
func (s *Scheduler) Prepare(ctx context.Context, in store.StartRequest) (store.StageRun, error) {
	if in.IdempotencyKey != "" {
		return store.StageRun{}, blocked("start_request_invalid")
	}
	req, e := s.prepareReservation(ctx, in)
	if e != nil {
		return store.StageRun{}, e
	}
	r, e := s.Store.StartReserved(in, req)
	if e != nil {
		if errors.Is(e, store.ErrConflict) {
			return store.StageRun{}, blocked("capacity_busy")
		}
		if errors.Is(e, store.ErrBudget) {
			return store.StageRun{}, blocked("budget_exhausted")
		}
		return store.StageRun{}, blocked("reservation_failed")
	}
	return r, nil
}

// prepareReservation reads current admission without recording intent or
// spending a call. Both entry points retain exactly the same admission gates.
func (s *Scheduler) prepareReservation(ctx context.Context, in store.StartRequest) (store.ReservationRequest, error) {
	if s == nil || s.Store == nil {
		return store.ReservationRequest{}, blocked("admission_unavailable")
	}
	t, e := s.Store.Task(in.TaskID)
	if e != nil {
		return store.ReservationRequest{}, blocked("task_unavailable")
	}
	if t.State != "ready" || t.PlanRevision != in.PlanRevision || in.ExpectedGeneration != nil && *in.ExpectedGeneration != t.Generation {
		return store.ReservationRequest{}, blocked("task_not_ready")
	}
	p, e := s.Store.Plan(t.ID, t.PlanRevision)
	if e != nil {
		return store.ReservationRequest{}, blocked("plan_unavailable")
	}
	b, ok := p.Bindings[in.Role]
	allowed := ok && b.Mode == stageplan.Locked && b.Target != nil && sameJSON(*b.Target, in.Target)
	if b.Mode == stageplan.Auto {
		for _, candidate := range b.Candidates {
			if sameJSON(candidate, in.Target) {
				allowed = true
			}
		}
	}
	if !allowed {
		return store.ReservationRequest{}, blocked("target_not_approved")
	}
	if e = s.Store.ValidateStageTargetAuthorized(store.StartIdentity{TaskID: t.ID, Role: in.Role, PlanRevision: in.PlanRevision, Generation: t.Generation, Restore: in.Restore}, in.Target, func() bool { return ctx != nil && ctx.Err() == nil }); e != nil {
		return store.ReservationRequest{}, e
	}
	inspection, e := s.inspect(ctx, t, in.Role, in.Target)
	if e != nil {
		return store.ReservationRequest{}, e
	}
	budget, e := s.Store.Budget(t.ID)
	if e != nil {
		return store.ReservationRequest{}, blocked("budget_missing")
	}
	if budget.UsedCalls >= budget.MaxCalls {
		return store.ReservationRequest{}, blocked("budget_exhausted")
	}
	limit, e := s.Store.Capacity()
	if e != nil {
		return store.ReservationRequest{}, blocked("capacity_unavailable")
	}
	return store.ReservationRequest{PoolKey: physicalPool(inspection.Quota.Pool), WriteKey: inspection.WriteKey, GlobalLimit: limit, AdmissionHash: inspection.ProofHash}, nil
}

// CheckPrepared repeats current admission immediately before an external
// launch, without issuing a model credential or spending a call. Preparation
// alone cannot preserve permissions, quota or a changed physical reservation.
func (s *Scheduler) CheckPrepared(ctx context.Context, expected store.StageRun) error {
	if s == nil || s.Store == nil {
		return blocked("admission_unavailable")
	}
	r, e := s.Store.CheckActive(expected.ID, expected.Generation)
	if e != nil || r.State != "starting" || r.Owner != expected.Owner || r.TaskID != expected.TaskID || r.Role != expected.Role || r.Attempt != expected.Attempt || r.PlanRevision != expected.PlanRevision || !sameJSON(r.Target, expected.Target) {
		return blocked("execution_fenced")
	}
	t, e := s.Store.Task(r.TaskID)
	if e != nil {
		return blocked("task_unavailable")
	}
	in, e := s.inspect(ctx, t, r.Role, r.Target)
	if e != nil {
		return e
	}
	reservation, e := s.Store.Reservation(r.ID)
	if e != nil || reservation.PoolKey != physicalPool(in.Quota.Pool) || reservation.WriteKey != in.WriteKey || reservation.AdmissionHash != in.ProofHash {
		return blocked("reservation_changed")
	}
	b, e := s.Store.Budget(r.TaskID)
	if e != nil {
		return blocked("budget_missing")
	}
	if b.UsedCalls >= b.MaxCalls {
		return blocked("budget_exhausted")
	}
	return nil
}

// Permit is passed to Dispatcher, after issuer authentication. It rechecks
// scope/current services and spends the persisted shared budget before sending.
func (s *Scheduler) Permit(ctx context.Context, c Claims, target stageplan.ExecutionTarget, ordinal int) error {
	if s == nil || s.Store == nil || !validScope(c) || c.Audience != ModelAudience || ordinal < 1 || ordinal > 3 {
		return blocked("stage_identity_invalid")
	}
	r, e := s.Store.CheckActive(c.RunID, c.Generation)
	if e != nil || r.State != "running" || r.TaskID != c.TaskID || r.Role != c.Role || r.Attempt != c.Attempt || r.PlanRevision != c.PlanRevision || !sameJSON(r.Target, target) {
		return blocked("execution_fenced")
	}
	t, e := s.Store.Task(c.TaskID)
	if e != nil || t.ProjectID != c.ProjectID {
		return blocked("stage_identity_invalid")
	}
	in, e := s.inspect(ctx, t, c.Role, target)
	if e != nil {
		return e
	}
	reservation, e := s.Store.Reservation(c.RunID)
	if e != nil || reservation.PoolKey != physicalPool(in.Quota.Pool) || reservation.WriteKey != in.WriteKey || reservation.AdmissionHash != in.ProofHash {
		return blocked("reservation_changed")
	}
	if e = s.Store.ReserveCall(c.RunID, c.Generation); e != nil {
		if errors.Is(e, store.ErrBudget) {
			return blocked("budget_exhausted")
		}
		return blocked("execution_fenced")
	}
	return nil
}
func (s *Scheduler) Release(proof StopProof) error {
	if s == nil || s.Store == nil || s.VerifyStop == nil || !proof.DescendantsStopped || !digestValue(proof.ReportHash) || !digestValue(proof.ProcessIdentityHash) {
		return blocked("stop_unverified")
	}
	r, e := s.Store.Run(proof.RunID)
	if e != nil || r.Generation != proof.Generation || r.NativeSessionID != proof.NativeSessionID || !s.VerifyStop(proof) {
		return blocked("stop_unverified")
	}
	if e = s.Store.ReleaseReserved(r.ID, r.Generation, proof.ReportHash, true); e != nil {
		return blocked("execution_not_reconciled")
	}
	return nil
}
