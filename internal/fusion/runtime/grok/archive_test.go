package grok

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/policy"
)

func archiveFixture(t *testing.T) checkpointRecord {
	t.Helper()
	_, r, in, _, _ := adapterFixture(t)
	r.State = "succeeded"
	r.NativeSessionID = "11111111-1111-4111-8111-111111111111"
	r.LaunchConfirmed = true
	st, e := os.Stat(in.Workspace)
	if e != nil {
		t.Fatal(e)
	}
	identity, ok := archiveIdentityOf(st)
	if !ok {
		t.Fatal("identity unsupported")
	}
	p := filepath.Join(in.Root, "config", "grok", "sessions", archiveCwd(in.Workspace), r.NativeSessionID)
	if e := os.MkdirAll(p, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range nativeArchiveFiles {
		data := []byte("{}\n")
		if strings.HasSuffix(name, ".lock") {
			data = nil
		}
		if e := os.WriteFile(filepath.Join(p, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	b, _ := json.Marshal(map[string]any{"info": map[string]string{"id": r.NativeSessionID, "cwd": in.Workspace}, "current_model_id": r.Target.RequestedModel})
	if e := os.WriteFile(filepath.Join(p, "summary.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	owner := r.Owner
	r.Owner = ""
	rootInfo, e := os.Stat(in.Root)
	if e != nil {
		t.Fatal(e)
	}
	rootID, ok := archiveIdentityOf(rootInfo)
	if !ok {
		t.Fatal("root identity unsupported")
	}
	return checkpointRecord{run: r, owner: owner, projectID: "fixture-project", root: in.Root, cwd: in.Workspace, cwdIdentity: identity, rootIdentity: rootID, proof: policy.StopProof{RunID: r.ID, Generation: r.Generation, NativeSessionID: r.NativeSessionID, DescendantsStopped: true, ReportHash: strings.Repeat("a", 64), ProcessIdentityHash: strings.Repeat("b", 64)}}
}
func TestArchivesRequirePrivateDirectoryAndKey(t *testing.T) {
	for _, mode := range []string{"relative", "public", "git", "symlink", "key_mode", "key_hardlink", "key_short", "key_symlink"} {
		t.Run(mode, func(t *testing.T) {
			p := privateAdapterDir(t)
			switch mode {
			case "relative":
				p = "relative"
			case "public":
				os.Chmod(p, 0755)
			case "git":
				os.Mkdir(filepath.Join(p, ".git"), 0700)
			case "symlink":
				link := filepath.Join(privateAdapterDir(t), "link")
				os.Symlink(p, link)
				p = link
			default:
				key := filepath.Join(p, "archive.key")
				os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600)
				switch mode {
				case "key_mode":
					os.Chmod(key, 0644)
				case "key_hardlink":
					os.Link(key, filepath.Join(p, "other-key"))
				case "key_short":
					os.WriteFile(key, []byte("short"), 0600)
				case "key_symlink":
					os.Rename(key, key+".old")
					os.Symlink(key+".old", key)
				}
			}
			if a, e := NewArchives(p); e == nil {
				a.Close()
				t.Fatal("unsafe archive key/root accepted")
			}
		})
	}
}
func TestArchivesSealReopenAndFreezeMetadata(t *testing.T) {
	record := archiveFixture(t)
	dir := privateAdapterDir(t)
	a, e := NewArchives(dir)
	if e != nil {
		t.Fatal(e)
	}
	ref, e := a.capture(context.Background(), record)
	if e != nil {
		t.Fatal(e)
	}
	info, e := a.Info(ref)
	if e != nil || info.RunID != record.run.ID || info.NativeSessionID != record.run.NativeSessionID || info.FileCount != len(nativeArchiveFiles) || info.ReadCount != 0 {
		t.Fatal("sealed metadata mismatch", e)
	}
	info.Target.Capabilities = []string{"caller-change"}
	again, e := a.Info(ref)
	if e != nil || len(again.Target.Capabilities) == 1 && again.Target.Capabilities[0] == "caller-change" {
		t.Fatal("metadata changed by caller")
	}
	repeat, e := a.capture(context.Background(), record)
	if e != nil || repeat != ref {
		t.Fatal("checkpoint retry changed archive", e)
	}
	if e := a.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := NewArchives(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if _, e = reopened.Info(ref); e != nil {
		t.Fatal("persisted seal unavailable", e)
	}
	if _, e = reopened.Info(CheckpointRef{ID: ref.ID, Digest: strings.Repeat("c", 64)}); e == nil {
		t.Fatal("wrong digest accepted")
	}
	key, _ := os.Stat(filepath.Join(dir, "archive.key"))
	if key.Mode().Perm() != 0600 {
		t.Fatal("archive key not private")
	}
	for _, name := range append(append([]string(nil), nativeArchiveFiles...), "manifest.json") {
		p := filepath.Join(dir, ref.ID, name)
		st, e := os.Stat(p)
		if e != nil || st.Mode().Perm() != 0600 {
			t.Fatal("archive not private", name)
		}
	}
}
func TestArchivesRejectNativeSnapshotAndSealedDrift(t *testing.T) {
	for _, mode := range []string{"missing", "extra", "symlink", "hardlink", "directory", "oversize", "summary_model", "summary_cwd", "summary_id", "secret", "encoded_secret", "workspace", "root_overlap", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			record := archiveFixture(t)
			dir := privateAdapterDir(t)
			a, e := NewArchives(dir)
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			p := filepath.Join(record.root, "config", "grok", "sessions", archiveCwd(record.cwd), record.run.NativeSessionID)
			file := filepath.Join(p, "chat_history.jsonl")
			switch mode {
			case "missing":
				os.Remove(file)
			case "extra":
				os.WriteFile(filepath.Join(p, "unknown.json"), []byte("{}"), 0600)
			case "symlink":
				os.Remove(file)
				os.Symlink(filepath.Join(p, "events.jsonl"), file)
			case "hardlink":
				os.Remove(file)
				os.Link(filepath.Join(p, "events.jsonl"), file)
			case "directory":
				os.Remove(file)
				os.Mkdir(file, 0700)
			case "oversize":
				os.WriteFile(file, make([]byte, maxArchiveFile+1), 0600)
			case "summary_model", "summary_cwd", "summary_id":
				model, cwd, id := record.run.Target.RequestedModel, record.cwd, record.run.NativeSessionID
				if mode == "summary_model" {
					model = "other"
				}
				if mode == "summary_cwd" {
					cwd = dir
				}
				if mode == "summary_id" {
					id = "22222222-2222-4222-8222-222222222222"
				}
				raw, _ := json.Marshal(map[string]any{"info": map[string]string{"id": id, "cwd": cwd}, "current_model_id": model})
				os.WriteFile(filepath.Join(p, "summary.json"), raw, 0600)
			case "secret", "encoded_secret":
				record.markers = [][]byte{[]byte("fixture-private-grant")}
				raw := []byte(`{"value":"fixture-private-grant"}`)
				if mode == "encoded_secret" {
					raw = []byte(`{"value":"fixture\u002dprivate\u002dgrant"}`)
				}
				os.WriteFile(file, raw, 0600)
			case "workspace":
				os.Rename(record.cwd, record.cwd+"-old")
				os.Mkdir(record.cwd, 0700)
			case "root_overlap":
				a.Close()
				a, _ = NewArchives(record.root)
				defer a.Close()
			case "cancelled":
				record.run.State = "cancelled"
			}
			if _, e := a.capture(context.Background(), record); e == nil {
				t.Fatal("unsafe snapshot accepted")
			}
		})
	}
	for _, mode := range []string{"content", "missing", "extra", "manifest", "key", "archive_dir", "workspace"} {
		t.Run("sealed_"+mode, func(t *testing.T) {
			record := archiveFixture(t)
			dir := privateAdapterDir(t)
			a, e := NewArchives(dir)
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			ref, e := a.capture(context.Background(), record)
			if e != nil {
				t.Fatal(e)
			}
			p := filepath.Join(dir, ref.ID)
			switch mode {
			case "content":
				os.WriteFile(filepath.Join(p, "updates.jsonl"), []byte("changed"), 0600)
			case "missing":
				os.Remove(filepath.Join(p, "chat_history.jsonl"))
			case "extra":
				os.WriteFile(filepath.Join(p, "config.toml"), []byte("forbidden"), 0600)
			case "manifest":
				raw, _ := os.ReadFile(filepath.Join(p, "manifest.json"))
				os.WriteFile(filepath.Join(p, "manifest.json"), append(raw, ' '), 0600)
			case "key":
				os.WriteFile(filepath.Join(dir, "archive.key"), []byte(strings.Repeat("x", 32)), 0600)
			case "archive_dir":
				os.Rename(p, p+"-old")
				os.Symlink(p+"-old", p)
			case "workspace":
				os.Rename(record.cwd, record.cwd+"-old")
				os.Mkdir(record.cwd, 0700)
			}
			if _, e := a.Info(ref); e == nil {
				t.Fatal("changed sealed archive accepted")
			}
		})
	}
}
func TestAdapterCheckpointNeedsOwnedReleasedSuccessfulHandle(t *testing.T) {
	c, r, _, _, _ := adapterFixture(t)
	a, e := NewAdapter(c)
	if e != nil {
		t.Fatal(e)
	}
	archives, e := NewArchives(privateAdapterDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer archives.Close()
	if _, e := a.Checkpoint(context.Background(), r.ID, r.Generation, archives); e == nil {
		t.Fatal("unowned/running execution archived")
	}
	if _, e := a.Checkpoint(context.Background(), r.ID, r.Generation, nil); e == nil {
		t.Fatal("nil archive accepted")
	}
}

func TestArchivesLargeManifestRemainsReadable(t *testing.T) {
	record := archiveFixture(t)
	for i, name := range []string{"a.txt", "b.txt", "c.txt"} {
		raw := strings.Repeat("<", 60000)
		path := filepath.Join(record.cwd, name)
		if os.WriteFile(path, []byte(raw), 0600) != nil {
			t.Fatal("read fixture failed")
		}
		info, e := os.Stat(path)
		if e != nil {
			t.Fatal(e)
		}
		id, ok := archiveIdentityOf(info)
		if !ok {
			t.Fatal("read identity unsupported")
		}
		args, _ := json.Marshal(map[string]string{"target_file": path})
		record.reads = append(record.reads, archiveRead{Call: readCall{ID: []string{"call_a", "call_b", "call_c"}[i], Name: "read_file", Arguments: string(args), typed: true}, Argument: path, Path: path, Relative: name, Raw: raw, Identity: id, Mode: uint32(info.Mode()), Bytes: info.Size(), Modified: info.ModTime().UnixNano()})
	}
	a, e := NewArchives(privateAdapterDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ref, e := a.capture(context.Background(), record)
	if e != nil {
		t.Fatal("bounded read manifest refused", e)
	}
	raw, e := os.ReadFile(filepath.Join(a.path, ref.ID, "manifest.json"))
	if e != nil || len(raw) <= 1<<20 {
		t.Fatal("manifest boundary not exercised")
	}
	if info, e := a.Info(ref); e != nil || info.ReadCount != 3 {
		t.Fatal("published manifest cannot be reopened", e)
	}
}

func TestArchivesSealCannotBeReplacedWithCallerDigest(t *testing.T) {
	record := archiveFixture(t)
	a, e := NewArchives(privateAdapterDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ref, e := a.capture(context.Background(), record)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(a.path, ref.ID, "manifest.json")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var sealed sealedCheckpoint
	if archiveJSON(raw, &sealed) != nil {
		t.Fatal("valid manifest decode failed")
	}
	var m checkpointManifest
	if archiveJSON(sealed.Payload, &m) != nil {
		t.Fatal("valid payload decode failed")
	}
	m.Run.Target.Account = "caller-account"
	sealed.Payload, _ = json.Marshal(m)
	changed, _ := json.Marshal(sealed)
	if os.WriteFile(path, changed, 0600) != nil {
		t.Fatal("tamper fixture failed")
	}
	ref.Digest = archiveHash(changed)
	if _, e := a.Info(ref); e == nil {
		t.Fatal("caller recomputed digest bypassed private seal")
	}
}
func TestArchivesCancellationAndLargeEncodedCredentialNeverPublish(t *testing.T) {
	record := archiveFixture(t)
	a, e := NewArchives(privateAdapterDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := a.capture(ctx, record); e == nil {
		t.Fatal("cancelled capture accepted")
	}
	record.markers = [][]byte{[]byte("fixture-private-grant")}
	path := filepath.Join(record.root, "config", "grok", "sessions", archiveCwd(record.cwd), record.run.NativeSessionID, "prompt_context.json")
	raw := []byte(`{"padding":"` + strings.Repeat("x", 1100000) + `","value":"fixture\u002dprivate\u002dgrant"}`)
	if os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("large JSON fixture failed")
	}
	if _, e := a.capture(context.Background(), record); e == nil {
		t.Fatal("large decoded credential archived")
	}
	entries, e := os.ReadDir(a.path)
	if e != nil || len(entries) != 1 || entries[0].Name() != "archive.key" {
		t.Fatal("failed capture published data")
	}
}

func TestArchivesRejectReplacedNativeRoot(t *testing.T) {
	record := archiveFixture(t)
	a, e := NewArchives(privateAdapterDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	relative := filepath.Join("config", "grok", "sessions", archiveCwd(record.cwd), record.run.NativeSessionID)
	before := map[string][]byte{}
	for _, name := range nativeArchiveFiles {
		raw, e := os.ReadFile(filepath.Join(record.root, relative, name))
		if e != nil {
			t.Fatal(e)
		}
		before[name] = raw
	}
	if os.Rename(record.root, record.root+"-old") != nil || os.Mkdir(record.root, 0700) != nil || os.MkdirAll(filepath.Join(record.root, relative), 0700) != nil {
		t.Fatal("root replacement failed")
	}
	for name, raw := range before {
		if os.WriteFile(filepath.Join(record.root, relative, name), raw, 0600) != nil {
			t.Fatal("replacement fixture failed")
		}
	}
	if _, e := a.capture(context.Background(), record); e == nil {
		t.Fatal("replacement launch root accepted as original Native state")
	}
}
