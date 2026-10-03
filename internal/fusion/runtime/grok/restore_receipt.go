package grok

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"

	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

type restoreReceipt struct {
	Version                                                 int
	Run                                                     store.StageRun
	OriginRunID                                             string
	OriginGeneration                                        int64
	NativeSessionID, ProjectID, Workspace, Root, PromptHash string
	RootIdentity                                            archiveIdentity
	Checkpoint                                              CheckpointRef
}

// RestoreInfo records a prepared mapping, not proof that the new process ran
// or stopped. Caller must authorize task/project access; it never relaunches.
type RestoreInfo struct {
	RunID, OriginRunID, NativeSessionID string
	Generation, OriginGeneration        int64
	Checkpoint                          CheckpointRef
}

func (RestoreInfo) String() string { return "Grok prepared restore mapping (private)" }

func (a *Archives) live() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.check() == nil
}
func restoreReceiptName(id string) string { return "restore-" + archiveHash([]byte(id)) + ".json" }

func (a *Archives) recordRestore(ctx context.Context, c *restoredCheckpoint, rootPath string, prompt []byte) error {
	if a == nil || c == nil || ctx.Err() != nil || workspace.PrivateState(rootPath) != nil || !archiveSeparate(a.path, rootPath) || !archiveSeparate(c.manifest.Workspace, rootPath) {
		return ErrIdentity
	}
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return ErrIdentity
	}
	defer root.Close()
	if !archiveNames(root, nil) {
		return ErrIdentity
	}
	info, e := root.Stat(".")
	if e != nil {
		return ErrIdentity
	}
	id, ok := archiveIdentityOf(info)
	if !ok {
		return ErrIdentity
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m, _, e := a.open(c.ref)
	if e != nil || !sameRestoreRun(m.Run, c.manifest.Run) || m.ProjectID != c.manifest.ProjectID {
		return ErrIdentity
	}
	r := restoreReceipt{Version: 1, Run: c.run, OriginRunID: m.Run.ID, OriginGeneration: m.Run.Generation, NativeSessionID: m.Run.NativeSessionID, ProjectID: m.ProjectID, Workspace: m.Workspace, Root: rootPath, RootIdentity: id, PromptHash: archiveHash(prompt), Checkpoint: c.ref}
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > maxArchiveFile {
		return ErrUnverified
	}
	mac := hmac.New(sha256.New, a.key)
	mac.Write(raw)
	sealed, e := json.Marshal(sealedCheckpoint{Payload: raw, MAC: hex.EncodeToString(mac.Sum(nil))})
	if e != nil || len(sealed) > maxArchiveFile {
		return ErrUnverified
	}
	name := restoreReceiptName(c.run.ID)
	if _, e = a.root.Lstat(name); e == nil {
		old, e := a.receipt(c.run.ID, c.run.Generation)
		oldRaw, _ := json.Marshal(old)
		if e != nil || string(oldRaw) != string(raw) {
			return ErrIdentity
		}
		return nil
	} else if !os.IsNotExist(e) {
		return ErrIdentity
	}
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return ErrUnverified
	}
	temp := ".restore-" + hex.EncodeToString(nonce[:])
	defer a.root.Remove(temp)
	if e = archiveWrite(a.root, temp, sealed); e != nil || ctx.Err() != nil || a.check() != nil {
		return ErrUnverified
	}
	// Link publishes without replacing an existing mapping from another opener.
	if a.root.Link(temp, name) != nil {
		return ErrIdentity
	}
	if a.root.Remove(temp) != nil || archiveSync(a.root) != nil {
		return ErrUnverified
	}
	return nil
}

func (a *Archives) receipt(runID string, gen int64) (restoreReceipt, error) {
	var r restoreReceipt
	if a.check() != nil || runID == "" || len(runID) > 128 || strings.ContainsRune(runID, 0) || gen < 1 {
		return r, ErrIdentity
	}
	raw, _, e := archiveReadFile(a.root, restoreReceiptName(runID), maxArchiveFile, true)
	if e != nil {
		return r, ErrIdentity
	}
	var sealed sealedCheckpoint
	if archiveJSON(raw, &sealed) != nil {
		return r, ErrProtocol
	}
	mac := hmac.New(sha256.New, a.key)
	mac.Write(sealed.Payload)
	got, e := hex.DecodeString(sealed.MAC)
	if e != nil || !hmac.Equal(got, mac.Sum(nil)) || archiveJSON(sealed.Payload, &r) != nil || r.Version != 1 || r.Run.ID != runID || r.Run.Generation != gen || !uuid.MatchString(r.NativeSessionID) || !archiveDigest(r.PromptHash) || !archiveDigest(r.Checkpoint.ID) || !archiveDigest(r.Checkpoint.Digest) {
		return r, ErrIdentity
	}
	return r, nil
}
func (a *Archives) RestoreInfo(runID string, generation int64) (RestoreInfo, error) {
	if a == nil {
		return RestoreInfo{}, ErrUnverified
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r, e := a.receipt(runID, generation)
	if e != nil {
		return RestoreInfo{}, e
	}
	return RestoreInfo{RunID: r.Run.ID, Generation: r.Run.Generation, OriginRunID: r.OriginRunID, OriginGeneration: r.OriginGeneration, NativeSessionID: r.NativeSessionID, Checkpoint: r.Checkpoint}, nil
}
