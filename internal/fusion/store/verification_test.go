//go:build darwin

package store

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

var verificationCapture = flag.String("fusion-verification-capture", "", "write synthetic owned verification and handoff schema evidence")

func TestStoredVerifierWorker(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "version" {
		fmt.Print("stored-verifier 1\n")
		os.Exit(0)
	}
	fmt.Print(`<testsuite tests="1"><testcase name="actual"/></testsuite>`)
	if mode == "fail" {
		os.Exit(7)
	}
	os.Exit(0)
}

func verificationFixture(t *testing.T, s *Store, mode string) (ArtifactRecord, evidence.Result, workspace.FrozenArtifact, evidence.Spec, string, string) {
	t.Helper()
	def := "medium"
	route := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "fixture-model-a", Account: "fixture-account-a", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential-a", RuntimeVersion: "fixture-runtime-v1", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, Efforts: []string{"medium"}, DefaultEffort: &def, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	binding := stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}
	p, err := stageplan.Compile(1, []stageplan.Role{stageplan.Testing}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Testing: binding}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Create("verified-task", CreateRequest{ProjectID: "verified-project", Goal: "real tests", Plan: p, Budget: &Budget{MaxCalls: 5}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.StartReserved(StartRequest{TaskID: task.ID, Role: stageplan.Testing, PlanRevision: 1, Owner: "fixture", TTL: time.Minute, Target: *p.Bindings[stageplan.Testing].Target}, ReservationRequest{PoolKey: "fixture-pool", WriteKey: "fixture-write", GlobalLimit: 2, AdmissionHash: hash([]byte("fixture admission"))})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-native"); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(run.ID, run.Generation, run.Owner, "succeeded"); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	source, _ := filepath.EvalSymlinks(t.TempDir())
	_ = os.Chmod(root, 0700)
	if err = os.WriteFile(filepath.Join(source, "tests.txt"), []byte("actual input"), 0600); err != nil {
		t.Fatal(err)
	}
	copy, err := workspace.Copy(source, root, "copy")
	if err != nil {
		t.Fatal(err)
	}
	a, err := workspace.Freeze(copy, root, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	bytes, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	spec := evidence.Spec{Tool: evidence.Tool{Executable: exe, SHA256: hash(bytes), Version: "stored-verifier 1", VersionArgs: []string{"-test.run=^TestStoredVerifierWorker$", "--", "version"}}, Args: []string{"-test.run=^TestStoredVerifierWorker$", "--", mode}, SuitePaths: []string{"tests.txt"}, Rules: evidence.Rules{MinTests: 1}, Timeout: 2 * time.Second}
	result, err := evidence.Run(context.Background(), a, root, spec)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := json.Marshal(run.Target)
	b := handoff.Binding{TaskID: task.ID, PlanRevision: 1, PlanHash: p.Hash, RunID: run.ID, Generation: run.Generation, Role: stageplan.Testing, TargetHash: hash(target)}
	h, err := handoff.PublishVerified(a, b, task.Goal, handoff.Evidence{OutputHash: hash([]byte("model says passed")), StopProofHash: hash([]byte("synthetic actual Native stop"))}, result, spec)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := h.Reference(root)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := h.Artifact(b)
	if err != nil {
		t.Fatal(err)
	}
	return ArtifactRecord{Reference: ref, InputTreeHash: ref.BaseTreeHash}, result, bound, spec, source, root
}

func TestVerificationOwnedRegistrationSurvivesRestartAndCannotBeForged(t *testing.T) {
	s, dbroot := openFixture(t)
	a, result, frozen, spec, source, root := verificationFixture(t, s, "pass")
	if err := s.RecordVerifiedArtifactAuthorized(a, result, frozen, spec, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.VerifiedArtifact(a.Reference.Binding.RunID, source, root, spec); !errors.Is(err, ErrArtifactPending) {
		t.Fatal("held producer leaked proof", err)
	}
	if err := s.ReleaseReserved(a.Reference.Binding.RunID, a.Reference.Binding.Generation, a.Reference.StopProofHash, true); err != nil {
		t.Fatal(err)
	}
	got, verdict, err := s.VerifiedArtifact(a.Reference.Binding.RunID, source, root, spec)
	if err != nil || verdict.Status != evidence.Passed || got.Verification == nil {
		t.Fatal(err, verdict)
	}
	if *verificationCapture != "" {
		bundle, restoreErr := handoff.Restore(got.Reference, source, root)
		if restoreErr != nil {
			t.Fatal(restoreErr)
		}
		doc, readErr := bundle.Read(got.Reference.Binding)
		if readErr != nil {
			t.Fatal(readErr)
		}
		capture, marshalErr := json.MarshalIndent(map[string]any{"handoff": doc, "verification": got.Verification}, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if writeErr := os.WriteFile(*verificationCapture, append(capture, '\n'), 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	raw, _ := json.Marshal(a)
	var legacy map[string]any
	_ = json.Unmarshal(raw, &legacy)
	if _, ok := legacy["verification"]; ok {
		t.Fatal("legacy record bytes changed")
	}
	before, _ := s.Events(a.Reference.Binding.TaskID, 0)
	if err = s.RecordVerifiedArtifactAuthorized(a, result, frozen, spec, func() bool { return false }); err == nil {
		t.Fatal("revoked registration succeeded")
	}
	after, _ := s.Events(a.Reference.Binding.TaskID, 0)
	if len(before) != len(after) {
		t.Fatal("revocation wrote events")
	}
	if _, err = s.db.Exec("UPDATE stage_artifacts SET record_json='{}'"); err == nil {
		t.Fatal("verification receipt mutable")
	}
	if err = s.RecordArtifact(got); !errors.Is(err, ErrInvalid) {
		t.Fatal("caller declared proof was accepted", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dbroot)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, v, err := s.VerifiedArtifact(a.Reference.Binding.RunID, source, root, spec); err != nil || v.Status != evidence.Passed {
		t.Fatal("restart lost owned proof", err, v)
	}
	spec.Rules.MinTests = 2
	if _, v, err := s.VerifiedArtifact(a.Reference.Binding.RunID, source, root, spec); err != nil || v.Status != evidence.Superseded {
		t.Fatal("changed standard reused proof", err, v)
	}
}

func TestVerificationRejectsJSONEmptyResultAndForeignArtifactBeforeWrite(t *testing.T) {
	s, _ := openFixture(t)
	a, result, frozen, spec, _, _ := verificationFixture(t, s, "pass")
	data, err := evidence.Export(result, frozen, spec)
	if err != nil {
		t.Fatal(err)
	}
	bad := a
	bad.Verification = &ArtifactVerification{Data: data}
	if err = s.RecordArtifact(bad); !errors.Is(err, ErrInvalid) {
		t.Fatal("public write accepted self-declared data", err)
	}
	if err = s.RecordVerifiedArtifactAuthorized(a, evidence.Result{}, frozen, spec, func() bool { return true }); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty result became execution", err)
	}
	bad = a
	bad.Reference.Path += "-foreign"
	if err = s.RecordVerifiedArtifactAuthorized(bad, result, frozen, spec, func() bool { return true }); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign artifact reused result", err)
	}
	if _, err = s.Artifact(a.Reference.Binding.RunID); !errors.Is(err, ErrNotFound) {
		t.Fatal("invalid attempt persisted", err)
	}
}

func TestVerificationFailureNeedsReviewAndEventRollbackPreservesTask(t *testing.T) {
	s, _ := openFixture(t)
	a, result, frozen, spec, _, _ := verificationFixture(t, s, "fail")
	if _, err := s.db.Exec("CREATE TRIGGER fixture_verify_fail BEFORE INSERT ON events WHEN NEW.kind='verification_requires_review' BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordVerifiedArtifactAuthorized(a, result, frozen, spec, func() bool { return true }); err == nil {
		t.Fatal("failed event committed")
	}
	task, err := s.Task(a.Reference.Binding.TaskID)
	if err != nil || task.State != "ready" {
		t.Fatal("partial needs_review", err)
	}
	if _, err := s.Artifact(a.Reference.Binding.RunID); !errors.Is(err, ErrNotFound) {
		t.Fatal("partial proof survived", err)
	}
	if _, err = s.db.Exec("DROP TRIGGER fixture_verify_fail"); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordVerifiedArtifactAuthorized(a, result, frozen, spec, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	task, err = s.Task(a.Reference.Binding.TaskID)
	if err != nil || task.State != "needs_review" {
		t.Fatal("hard exit7 did not stop workflow", err)
	}
	if err = s.RecordVerifiedArtifactAuthorized(a, result, frozen, spec, func() bool { return true }); err != nil {
		t.Fatal("exact retry changed proof", err)
	}
	if _, err = s.Reservation(a.Reference.Binding.RunID); err != nil {
		t.Fatal("registration released Native reservation", err)
	}
}
