package policy

import (
	"context"
	"errors"

	"github.com/yetone/magpie/internal/fusion/store"
)

// PrepareOnce prepares a new intent through current admission, or reads the
// existing durable receipt before any admission query. Only Created=true may
// be passed to an external launcher; a read never grants execution authority.
// The caller must authenticate, resolve the frozen Target, and revalidate
// CheckPrepared at launch. HTTP disconnect is not a lifecycle cancellation.
func (s *Scheduler) PrepareOnce(ctx context.Context, in store.StartRequest) (store.StartReceipt, error) {
	if in.Restore != nil {
		ref := *in.Restore
		in.Restore = &ref
	}
	old, e := s.lookupPreparedStart(in)
	if !errors.Is(e, store.ErrNotFound) {
		return old, e
	}
	req, e := s.prepareReservation(ctx, in)
	if e != nil {
		// A concurrent identical request can commit while this request is
		// inspecting or discovering that the task is no longer ready. Read
		// its receipt only; this does not waive admission for any new launch.
		old, lookupErr := s.lookupPreparedStart(in)
		if !errors.Is(lookupErr, store.ErrNotFound) {
			return old, lookupErr
		}
		return store.StartReceipt{}, e
	}
	r, e := s.Store.StartReservedOnce(in, req)
	if e == nil {
		return r, nil
	}
	if errors.Is(e, store.ErrConflict) {
		// Distinguish a competing key/payload from a later CAS/capacity
		// conflict. Store still owns the atomic winner, not this read.
		old, lookupErr := s.lookupPreparedStart(in)
		if !errors.Is(lookupErr, store.ErrNotFound) {
			return old, lookupErr
		}
		return store.StartReceipt{}, blocked("start_conflict")
	}
	if errors.Is(e, store.ErrBudget) {
		return store.StartReceipt{}, blocked("budget_exhausted")
	}
	if errors.Is(e, store.ErrStageLimit) {
		return store.StartReceipt{}, e
	}
	if errors.Is(e, store.ErrInvalid) {
		return store.StartReceipt{}, blocked("start_request_invalid")
	}
	return store.StartReceipt{}, blocked("reservation_failed")
}

func (s *Scheduler) lookupPreparedStart(in store.StartRequest) (store.StartReceipt, error) {
	if s == nil || s.Store == nil {
		return store.StartReceipt{}, blocked("admission_unavailable")
	}
	if in.ExpectedGeneration == nil {
		return store.StartReceipt{}, blocked("start_request_invalid")
	}
	r, e := s.Store.LookupStart(in.IdempotencyKey, store.StartIdentity{TaskID: in.TaskID, Role: in.Role, PlanRevision: in.PlanRevision, Generation: *in.ExpectedGeneration, Restore: in.Restore})
	if errors.Is(e, store.ErrNotFound) {
		return store.StartReceipt{}, e
	}
	if errors.Is(e, store.ErrInvalid) {
		return store.StartReceipt{}, blocked("start_request_invalid")
	}
	if errors.Is(e, store.ErrConflict) || e == nil && !sameJSON(r.Target, in.Target) {
		return store.StartReceipt{}, blocked("start_request_conflict")
	}
	if e != nil {
		return store.StartReceipt{}, blocked("admission_unavailable")
	}
	return store.StartReceipt{Run: r, Created: false}, nil
}
