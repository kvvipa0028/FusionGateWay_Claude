package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// Native/inspection/stop admission here is synthetic. Store, Scheduler and
// sealed archives are real; this helper does not prove a managed Resume.
func restoreFixture(t *testing.T) (*Archives, CheckpointRef, checkpointRecord, store.StageRun, *gateFixture, *policy.Scheduler) {
	t.Helper()
	record := archiveFixture(t)
	f, s, _, old, pending, scheduler := storedGrokFixture(t, 8, false)
	if e := s.ConfirmStarted(old.ID, old.Generation, old.Owner, record.run.NativeSessionID); e != nil {
		t.Fatal(e)
	}
	if e := pending.Activate(); e != nil {
		t.Fatal(e)
	}
	ctx, e := f.manager.WithStage(context.Background(), f.token, f.config.Claims)
	if e != nil {
		t.Fatal(e)
	}
	if e = scheduler.Permit(ctx, f.config.Claims, old.Target, 1); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(old.ID, old.Generation, old.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	pending.Cancel()
	record.run, e = s.Run(old.ID)
	if e != nil {
		t.Fatal(e)
	}
	record.owner = old.Owner
	record.proof.RunID, record.proof.Generation = old.ID, old.Generation
	scheduler.VerifyStop = func(p policy.StopProof) bool { return p == record.proof } // fixture only
	if e = scheduler.Release(record.proof); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(record.cwd, "approved.txt")
	if e = os.WriteFile(path, []byte("ORIGINAL_APPROVED_READ\n"), 0600); e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	id, ok := archiveIdentityOf(st)
	if !ok {
		t.Fatal("identity unavailable")
	}
	args, _ := json.Marshal(map[string]string{"target_file": path})
	record.reads = []archiveRead{{Call: readCall{ID: "call_old_read", Name: "read_file", Arguments: string(args), typed: true}, Argument: path, Path: path, Relative: "approved.txt", Raw: "ORIGINAL_APPROVED_READ\n", Identity: id, Mode: uint32(st.Mode()), Bytes: st.Size(), Modified: st.ModTime().UnixNano()}}
	a, e := NewArchives(privateAdapterDir(t))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Close() })
	ref, e := a.capture(context.Background(), record)
	if e != nil {
		t.Fatal(e)
	}
	next, e := scheduler.Prepare(context.Background(), store.StartRequest{TaskID: old.TaskID, Role: old.Role, PlanRevision: old.PlanRevision, Owner: "fixture-resume-owner", TTL: time.Minute, Target: old.Target})
	if e != nil {
		t.Fatal(e)
	}
	return a, ref, record, next, f, scheduler
}

func TestRestoreScopeBindsSealedCheckpointToFreshPreparedRun(t *testing.T) {
	a, ref, record, next, _, scheduler := restoreFixture(t)
	r, e := a.prepareRestore(context.Background(), ref, scheduler, next, record.cwd)
	if e != nil {
		t.Fatal("trusted restore consumer refused", e)
	}
	if r.manifest.Run.ID != record.run.ID || r.run.ID != next.ID || r.manifest.Run.NativeSessionID != record.run.NativeSessionID || len(r.files) != 14 || len(r.manifest.Reads) != 1 {
		t.Fatal("restore identity mismatch")
	}
	if strings.Contains(r.String(), "ORIGINAL_APPROVED_READ") {
		t.Fatal("restore bytes exposed")
	}
	budget, e := scheduler.Store.Budget(next.TaskID)
	if e != nil || budget.UsedCalls != 1 {
		t.Fatal("preparing history spent or refunded budget")
	}
}

func TestRestoreScopeRejectsDriftAndCallerAuthority(t *testing.T) {
	for _, mode := range []string{"id", "generation", "task", "role", "attempt", "revision", "owner", "account", "model", "route", "credential", "effort", "cwd", "source", "seal", "closed", "quota", "permission", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			a, ref, record, next, _, scheduler := restoreFixture(t)
			cwd := record.cwd
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "id":
				next.ID = record.run.ID
			case "generation":
				next.Generation--
			case "task":
				next.TaskID = "caller-task"
			case "role":
				next.Role = "review"
			case "attempt":
				next.Attempt--
			case "revision":
				next.PlanRevision++
			case "owner":
				next.Owner = "caller-owner"
			case "account":
				next.Target.Account = "caller-account"
			case "model":
				next.Target.RequestedModel = "caller-model"
			case "route":
				next.Target.Route.ID = "caller-route"
			case "credential":
				next.Target.CredentialIdentity = "caller-credential"
			case "effort":
				next.Target.Effort.RequestedMode = "auto"
			case "cwd":
				cwd = privateAdapterDir(t)
			case "source":
				os.WriteFile(filepath.Join(cwd, "approved.txt"), []byte("CHANGED\n"), 0600)
			case "seal":
				ref.Digest = strings.Repeat("a", 64)
			case "closed":
				a.Close()
			case "quota", "permission":
				inspect := scheduler.Inspect
				scheduler.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
					in, e := inspect(ctx, task, role, target)
					if mode == "quota" {
						in.Quota.Status = quota.Unknown
					} else {
						in.DataAllowed = false
					}
					return in, e
				}
			case "cancel":
				cancel()
			}
			if _, e := a.prepareRestore(ctx, ref, scheduler, next, cwd); e == nil {
				t.Fatal("restore drift accepted")
			}
			budget, _ := scheduler.Store.Budget(record.run.TaskID)
			if budget.UsedCalls != 1 {
				t.Fatal("rejected restore spent/refunded budget")
			}
		})
	}
}

func restoreReadFixture(t *testing.T) (*restoredCheckpoint, *ReadTools, *gateFixture, *policy.Scheduler) {
	t.Helper()
	a, ref, record, next, f, scheduler := restoreFixture(t)
	c, e := a.prepareRestore(context.Background(), ref, scheduler, next, record.cwd)
	if e != nil {
		t.Fatal(e)
	}
	b := clone(f.config.Binding.Observer)
	b.RunID, b.Generation, b.Role = next.ID, next.Generation, next.Role
	b.NativeSessionID, b.Cwd, b.MaxTurns = record.run.NativeSessionID, record.cwd, 1
	current := func(Binding) bool {
		run, e := scheduler.Store.CheckActive(next.ID, next.Generation)
		return f.current.Load() && e == nil && (run.State == "starting" || run.State == "running")
	}
	r, e := c.readTools(b, current, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	if e = scheduler.Store.ConfirmStarted(next.ID, next.Generation, next.Owner, record.run.NativeSessionID); e != nil {
		t.Fatal(e)
	}
	claims := policy.Claims{TaskID: next.TaskID, RunID: next.ID, Role: next.Role, Attempt: next.Attempt, PlanRevision: next.PlanRevision, Generation: next.Generation, ProjectID: record.projectID, Audience: policy.ModelAudience}
	f.token, e = f.manager.Issue(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	f.config.Claims, f.config.Binding.Observer, f.config.Binding.Target = claims, b, next.Target
	f.config.ReadTools = r
	f.config.Current = func(got GateBinding) bool { return current(got.Observer) && sameTarget(got.Target, next.Target) }
	readForwarder(f, gateSSE)
	return c, r, f, scheduler
}

func TestRestoreReadHistoryUsesFreshGateBudgetAndRejectsMissingPairs(t *testing.T) {
	c, r, f, scheduler := restoreReadFixture(t)
	old := c.manifest.Reads[0]
	g := newGate(t, f)
	body := continuation(t, old.Path, old.Call.ID, readText(old.Raw))
	for i := 0; i < 2; i++ {
		if w := send(t, g, f, body, "/v1/chat/completions"); w.Code != 200 {
			t.Fatal("approved historical pair refused", w.Code, g.Audit())
		}
	}
	budget, e := scheduler.Store.Budget(c.run.TaskID)
	if e != nil || budget.UsedCalls != 3 || f.calls.Load() != 2 || f.permits.Load() != 2 || !r.ready() {
		t.Fatal("history bypassed or refunded persistent call budget")
	}
	if w := send(t, g, f, gateBody, "/v1/chat/completions"); w.Code < 400 {
		t.Fatal("missing old Read pair accepted")
	}
	budget, _ = scheduler.Store.Budget(c.run.TaskID)
	if budget.UsedCalls != 3 || f.calls.Load() != 2 {
		t.Fatal("rejected missing history sent upstream")
	}
}

func restoreMessages(t *testing.T, body string) []json.RawMessage {
	t.Helper()
	var request struct{ Messages []json.RawMessage }
	if e := json.Unmarshal([]byte(body), &request); e != nil {
		t.Fatal(e)
	}
	return request.Messages
}

func TestRestoreReadHistoryRejectsForgedReplayAndChangedSource(t *testing.T) {
	for _, mode := range []string{"missing", "text", "id", "args", "orphan", "duplicate", "model", "source", "replace", "current", "closed", "event_replay"} {
		t.Run(mode, func(t *testing.T) {
			c, r, f, _ := restoreReadFixture(t)
			old := c.manifest.Reads[0]
			body := continuation(t, old.Path, old.Call.ID, readText(old.Raw))
			messages := restoreMessages(t, body)
			switch mode {
			case "missing":
				messages = messages[:1]
			case "text":
				messages = restoreMessages(t, continuation(t, old.Path, old.Call.ID, "FORGED_READ"))
			case "id":
				messages = restoreMessages(t, continuation(t, old.Path, "call_forged", readText(old.Raw)))
			case "args":
				messages = restoreMessages(t, continuation(t, "other.txt", old.Call.ID, readText(old.Raw)))
			case "orphan":
				messages = messages[2:]
			case "duplicate":
				messages = append(messages, messages[1:]...)
			case "model":
				messages[1] = bytes.Replace(messages[1], []byte(`"role":"assistant"`), []byte(`"model_id":"other-model","role":"assistant"`), 1)
			case "source":
				os.WriteFile(old.Path, []byte("CHANGED"), 0600)
			case "replace":
				os.Rename(old.Path, old.Path+".old")
				os.WriteFile(old.Path, []byte(old.Raw), 0600)
			case "current":
				f.current.Store(false)
			case "closed":
				r.Close()
			case "event_replay":
				frame, _ := json.Marshal(map[string]string{"type": "tool_call", "toolCallId": old.Call.ID})
				if e := r.event(frame); e == nil {
					t.Fatal("historical tool replay executed")
				}
				return
			}
			if r.request(messages, false) {
				t.Fatal("forged/stale historical record accepted")
			}
		})
	}
}

func TestRestoreReadHistoryDoesNotSpendNewToolTurn(t *testing.T) {
	c, r, _, _ := restoreReadFixture(t)
	old := c.manifest.Reads[0]
	messages := restoreMessages(t, continuation(t, old.Path, old.Call.ID, readText(old.Raw)))
	call := old.Call
	if e := r.response(messages, false, &call, nil); e == nil {
		t.Fatal("old tool ID reused")
	}
	call.ID = "call_new_read"
	if e := r.response(messages, false, &call, nil); e != nil {
		t.Fatal("history consumed new run's only tool turn", e)
	}
	call.ID = "call_second_new_read"
	if e := r.response(messages, false, &call, nil); e == nil {
		t.Fatal("new tool turn limit bypassed")
	}
	if r.history != 1 || len(r.records) != 2 {
		t.Fatal("historical/new records merged")
	}
}

func TestRestoreReadHistoryImportRechecksScopeAndPrivateMarkers(t *testing.T) {
	for _, mode := range []string{"scope", "cwd", "uuid", "model", "pin", "current", "marker", "source"} {
		t.Run(mode, func(t *testing.T) {
			a, ref, record, next, f, scheduler := restoreFixture(t)
			c, e := a.prepareRestore(context.Background(), ref, scheduler, next, record.cwd)
			if e != nil {
				t.Fatal(e)
			}
			b := clone(f.config.Binding.Observer)
			b.RunID, b.Generation, b.Role, b.Cwd, b.NativeSessionID = next.ID, next.Generation, next.Role, record.cwd, record.run.NativeSessionID
			var markers [][]byte
			switch mode {
			case "scope":
				b.Generation--
			case "cwd":
				b.Cwd = privateAdapterDir(t)
			case "uuid":
				b.NativeSessionID = "22222222-2222-4222-8222-222222222222"
			case "model":
				b.Model = "other-model"
			case "pin":
				b.ExecutableSHA256 = strings.Repeat("a", 64)
			case "current":
				f.current.Store(false)
			case "marker":
				markers = [][]byte{[]byte("ORIGINAL_APPROVED_READ")}
			case "source":
				os.WriteFile(record.reads[0].Path, []byte("CHANGED"), 0600)
			}
			if r, e := c.readTools(b, func(Binding) bool { return f.current.Load() }, markers); e == nil {
				r.Close()
				t.Fatal("stale/wrong/private import accepted")
			}
		})
	}
}

func TestRestoreReadHistoryRejectsDecodedCredentialFromValidSeal(t *testing.T) {
	_, _, record, next, f, scheduler := restoreFixture(t)
	old := &record.reads[0]
	old.Raw = `{"secret":"private\u002dgrant"}`
	if e := os.WriteFile(old.Path, []byte(old.Raw), 0600); e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(old.Path)
	if e != nil {
		t.Fatal(e)
	}
	old.Identity, _ = archiveIdentityOf(st)
	old.Mode, old.Bytes, old.Modified = uint32(st.Mode()), st.Size(), st.ModTime().UnixNano()
	a, e := NewArchives(privateAdapterDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ref, e := a.capture(context.Background(), record)
	if e != nil {
		t.Fatal("valid encoded Source fixture refused", e)
	}
	c, e := a.prepareRestore(context.Background(), ref, scheduler, next, record.cwd)
	if e != nil {
		t.Fatal(e)
	}
	b := clone(f.config.Binding.Observer)
	b.RunID, b.Generation, b.Role, b.Cwd, b.NativeSessionID = next.ID, next.Generation, next.Role, record.cwd, record.run.NativeSessionID
	r, e := c.readTools(b, func(Binding) bool { return true }, nil)
	if e != nil {
		t.Fatal("encoded history fixture invalid before marker check", e)
	}
	r.Close()
	if r, e := c.readTools(b, func(Binding) bool { return true }, [][]byte{[]byte("private-grant")}); e == nil {
		r.Close()
		t.Fatal("decoded new credential imported")
	}
}

func TestRestoreScopeFreezesInputAndUsesSealedPrivateCopy(t *testing.T) {
	a, ref, record, next, _, scheduler := restoreFixture(t)
	inspect := scheduler.Inspect
	scheduler.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
		next.Target.Capabilities[0] = "caller-mutation"
		return inspect(ctx, task, role, target)
	}
	if e := os.RemoveAll(filepath.Join(record.root, "config", "grok", "sessions")); e != nil {
		t.Fatal(e)
	}
	c, e := a.prepareRestore(context.Background(), ref, scheduler, next, record.cwd)
	if e != nil || c.run.Target.Capabilities[0] == "caller-mutation" || len(c.files) != 14 {
		t.Fatal("caller callback changed frozen restore or original Native required", e)
	}
	for _, file := range c.manifest.Files {
		if archiveHash(c.files[file.Name]) != file.Hash {
			t.Fatal("seal bytes mismatch")
		}
	}
}
