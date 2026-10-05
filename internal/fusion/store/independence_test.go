package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func independenceFixture(t *testing.T, s *Store) (StartRequest, ReservationRequest) {
	t.Helper()
	def := "medium"
	a := stageplan.Route{ID: "model-a", Revision: 1, Model: "shared-model", Account: "account-a", Workspace: "fixture-workspace", CredentialIdentity: "credential-a", RuntimeVersion: "fixture-v1", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, Efforts: []string{def}, DefaultEffort: &def, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	b := a
	b.ID = "model-b"
	b.Model = "other-model"
	alias := a
	alias.ID = "other-account-a"
	alias.Account = "account-b"
	alias.CredentialIdentity = "credential-b"
	candidate := func(r stageplan.Route) stageplan.Candidate {
		return stageplan.Candidate{Route: stageplan.RouteRef{ID: r.ID, Revision: 1}, Model: r.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}
	}
	layer := stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: {Mode: stageplan.Auto, Candidates: []stageplan.Candidate{candidate(a), candidate(b)}}, stageplan.Acceptance: {Mode: stageplan.Auto, Candidates: []stageplan.Candidate{candidate(alias), candidate(b)}}}}
	p, e := stageplan.CompileWithIndependence(1, []stageplan.Role{stageplan.Design, stageplan.Acceptance}, layer, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{a, b, alias}, []stageplan.RolePair{{First: stageplan.Design, Second: stageplan.Acceptance}})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("independence-task", CreateRequest{ProjectID: "independence-project", Goal: "fixture", Plan: p, Budget: &Budget{MaxCalls: 12, MaxReworks: 1}})
	if e != nil {
		t.Fatal(e)
	}
	gen := int64(0)
	in := StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, ExpectedGeneration: &gen, Owner: "fixture-worker", TTL: time.Minute, Target: p.Bindings[stageplan.Design].Candidates[0], IdempotencyKey: "first-design"}
	req := ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("fixture-inspection"))}
	r, e := s.StartReservedOnce(in, req)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(r.Run.ID, r.Run.Generation, r.Run.Owner, "independent-first-session"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReserveCall(r.Run.ID, r.Run.Generation); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(r.Run.ID, r.Run.Generation, r.Run.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(r.Run.ID, r.Run.Generation, hash([]byte("stopped-proof")), true); e != nil {
		t.Fatal(e)
	}
	gen = 1
	in.Role = stageplan.Acceptance
	in.Target = p.Bindings[stageplan.Acceptance].Candidates[0]
	in.IdempotencyKey = "conflicting-acceptance"
	return in, req
}

func TestProjectIndependenceBlocksSameModelAcrossAccountsBeforeIntent(t *testing.T) {
	s, _ := openFixture(t)
	in, req := independenceFixture(t, s)
	task, _ := s.Task(in.TaskID)
	budget, _ := s.Budget(in.TaskID)
	events, _ := s.Events(in.TaskID, 0)
	if _, e := s.StartReservedOnce(in, req); e == nil {
		t.Fatal("same model on another account launched despite frozen project independence")
	}
	after, _ := s.Task(in.TaskID)
	afterBudget, _ := s.Budget(in.TaskID)
	afterEvents, _ := s.Events(in.TaskID, 0)
	if task != after || budget != afterBudget || !reflect.DeepEqual(events, afterEvents) {
		t.Fatal("denial changed scene or budget")
	}
	if _, e := s.LookupStart(in.IdempotencyKey, identityOf(in)); !errors.Is(e, ErrNotFound) {
		t.Fatal("denial created replay mapping", e)
	}
}

func TestProjectIndependenceRestartViableAutoAndRevisionCannotRemovePolicy(t *testing.T) {
	s, root := openFixture(t)
	in, req := independenceFixture(t, s)
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	identity := identityOf(in)
	if e = s.ValidateStageTargetAuthorized(identity, in.Target, func() bool { return true }); !errors.Is(e, ErrIndependence) {
		t.Fatal("restart lost policy/history", e)
	}
	p, _ := s.Plan(in.TaskID, 1)
	in.Target = p.Bindings[stageplan.Acceptance].Candidates[1]
	if e = s.ValidateStageTargetAuthorized(identity, in.Target, func() bool { return false }); !errors.Is(e, ErrWorkflowAuthority) {
		t.Fatal("revoked preflight", e)
	}
	stale := identity
	stale.Generation--
	if e = s.ValidateStageTargetAuthorized(stale, in.Target, func() bool { return true }); !errors.Is(e, ErrConflict) {
		t.Fatal("stale preflight", e)
	}
	if e = s.ValidateStageTargetAuthorized(identity, in.Target, func() bool { return true }); e != nil {
		t.Fatal("compatible approved auto choice blocked", e)
	}
	var next stageplan.Snapshot
	raw, _ := json.Marshal(p)
	json.Unmarshal(raw, &next)
	next.Revision = 2
	next.Independence = nil
	next.Hash = ""
	raw, _ = json.Marshal(next)
	next.Hash = hash(raw)
	if stageplan.VerifySnapshot(next) != nil {
		t.Fatal("fixture hash invalid")
	}
	if e = s.ValidateRevision(in.TaskID, 1, next); !errors.Is(e, ErrConflict) {
		t.Fatal("future plan removed frozen policy", e)
	}
	in.IdempotencyKey = "compatible-acceptance"
	r, e := s.StartReservedOnce(in, req)
	if e != nil || !r.Created {
		t.Fatal("approved distinct model did not start", e)
	}
	again, e := s.StartReservedOnce(in, req)
	if e != nil || again.Created || again.Run.ID != r.Run.ID {
		t.Fatal("idempotent replay relaunched", e)
	}
}
