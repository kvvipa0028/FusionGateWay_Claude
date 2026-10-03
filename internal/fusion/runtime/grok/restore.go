package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/store"
)

// restoredCheckpoint is private controller state. It is produced only after
// verifying the archive and the fresh prepared run; it is not a request DTO or
// a public source of Read authority. Native seeding/launch is a separate step.
type restoredCheckpoint struct {
	manifest checkpointManifest
	files    map[string][]byte
	run      store.StageRun
	ref      CheckpointRef
}

func (*restoredCheckpoint) String() string { return "Grok verified restore state (private)" }

func sameRestoreRun(a, b store.StageRun) bool {
	return a.ID == b.ID && a.TaskID == b.TaskID && a.Role == b.Role && a.Attempt == b.Attempt && a.Generation == b.Generation && a.PlanRevision == b.PlanRevision && a.State == b.State && a.Owner == b.Owner && a.NativeSessionID == b.NativeSessionID && a.StartupIntent == b.StartupIntent && a.LaunchConfirmed == b.LaunchConfirmed && sameTarget(a.Target, b.Target)
}

func (a *Archives) prepareRestore(ctx context.Context, ref CheckpointRef, scheduler *policy.Scheduler, input store.StageRun, cwd string) (*restoredCheckpoint, error) {
	if a == nil || scheduler == nil || scheduler.Store == nil || ctx.Err() != nil {
		return nil, ErrUnverified
	}
	// Freeze caller-owned slices/pointers before trusted callbacks.
	raw, e := json.Marshal(input)
	var requested store.StageRun
	if e != nil || json.Unmarshal(raw, &requested) != nil {
		return nil, ErrIdentity
	}
	if e = scheduler.CheckPrepared(ctx, requested); e != nil {
		return nil, e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m, files, e := a.open(ref)
	if e != nil {
		return nil, e
	}
	current, e := scheduler.Store.CheckActive(requested.ID, requested.Generation)
	if e != nil || !sameRestoreRun(requested, current) || current.State != "starting" || current.Owner == "" || !current.StartupIntent || current.LaunchConfirmed || current.NativeSessionID != "" {
		return nil, ErrIdentity
	}
	reservation, e := scheduler.Store.Reservation(current.ID)
	if e != nil || reservation.WriteKey != "" {
		return nil, ErrUnverified
	}
	old, e := scheduler.Store.Run(m.Run.ID)
	if e != nil || !sameRestoreRun(old, m.Run) || old.State != "succeeded" || !old.LaunchConfirmed || old.Owner != "" {
		return nil, ErrIdentity
	}
	if _, e = scheduler.Store.Reservation(old.ID); !errors.Is(e, store.ErrNotFound) {
		return nil, ErrIdentity
	}
	task, e := scheduler.Store.Task(current.TaskID)
	if e != nil || task.ProjectID != m.ProjectID || task.Generation != current.Generation || task.State != "running" || current.ID == old.ID || current.Generation <= old.Generation || current.Attempt <= old.Attempt || current.TaskID != old.TaskID || current.Role != old.Role || current.PlanRevision != old.PlanRevision || !sameTarget(current.Target, old.Target) || cwd != m.Workspace {
		return nil, ErrIdentity
	}
	if ctx.Err() != nil || a.check() != nil {
		return nil, ErrIdentity
	}
	return &restoredCheckpoint{manifest: m, files: files, run: current, ref: ref}, nil
}

// readTools imports original completed grants into a distinct per-run scope.
// It never treats transcript tool records as grants. Each HTTP must still
// include and exactly match the historical pairs and recheck original Source.
func (c *restoredCheckpoint) readTools(b Binding, current func(Binding) bool, markers [][]byte) (*ReadTools, error) {
	if c == nil || b.RunID != c.run.ID || b.Generation != c.run.Generation || b.Role != c.run.Role || b.NativeSessionID != c.manifest.Run.NativeSessionID || b.Cwd != c.manifest.Workspace || b.Model != c.run.Target.RequestedModel || b.RuntimeVersion != c.manifest.RuntimeVersion || b.ExecutableSHA256 != c.manifest.ExecutableHash || !archiveWorkspace(c.manifest.Workspace, c.manifest.WorkspaceIdentity, c.manifest.Reads) {
		return nil, ErrIdentity
	}
	r, e := NewReadTools(b, current)
	if e != nil {
		return nil, e
	}
	failed := true
	defer func() {
		if failed {
			r.Close()
		}
	}()
	actual, ok := archiveIdentityOf(r.rootInfo)
	if !ok || actual != c.manifest.WorkspaceIdentity {
		return nil, ErrIdentity
	}
	for _, old := range c.manifest.Reads {
		for _, marker := range markers {
			if len(marker) == 0 || len(marker) > 4096 || bytes.Contains([]byte(old.Raw), marker) || privateJSON([]byte(old.Raw), marker) {
				return nil, ErrUnverified
			}
		}
		raw, info, e := r.file(old.Relative)
		if e != nil || string(raw) != old.Raw {
			return nil, ErrIdentity
		}
		identity, ok := archiveIdentityOf(info)
		if !ok || identity != old.Identity || uint32(info.Mode()) != old.Mode || info.Size() != old.Bytes || info.ModTime().UnixNano() != old.Modified || r.records[old.Call.ID] != nil {
			return nil, ErrIdentity
		}
		r.records[old.Call.ID] = &readRecord{call: old.Call, argument: old.Argument, path: old.Path, relative: old.Relative, raw: old.Raw, text: readText(old.Raw), info: info, started: true, located: true, done: true, continued: true, historical: true}
		r.bytes += len(raw)
		r.history++
	}
	if r.history > 64 || r.bytes > 512<<10 || r.check() != nil {
		return nil, ErrIdentity
	}
	failed = false
	return r, nil
}
