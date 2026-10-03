//go:build darwin || linux

package glm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"golang.org/x/sys/unix"
)

const privateFixtureKey = "fixture-only-private-glm-key"

func credentialFileFixture(t *testing.T) (string, FileCredentialScope, stageplan.ExecutionTarget) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "credentials")
	if e = os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "glm-coding-plan.key")
	if e = os.WriteFile(path, []byte(privateFixtureKey+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	scope := FileCredentialScope{Account: "fixture-account", Workspace: "fixture-workspace", Identity: "fixture-credential"}
	target := stageplan.ExecutionTarget{Account: scope.Account, Workspace: scope.Workspace, CredentialIdentity: scope.Identity, BillingPath: "coding_plan"}
	return path, scope, target
}

func TestGLMFileCredentialLoadsExactFrozenScopeWithoutDisclosure(t *testing.T) {
	path, scope, target := credentialFileFixture(t)
	c, e := NewFileCredential(path, scope)
	if e != nil {
		t.Fatal(e)
	}
	key, e := c.Load(context.Background(), target)
	if e != nil || key.Identity != scope.Identity || key.Key != privateFixtureKey {
		t.Fatal("credential not loaded")
	}
	for _, object := range []any{c, key} {
		raw, e := json.Marshal(object)
		if e != nil || strings.Contains(string(raw), privateFixtureKey) || strings.Contains(fmt.Sprintf("%v %#v", object, object), privateFixtureKey) || strings.Contains(string(raw), path) {
			t.Fatal("credential disclosure")
		}
	}
	for _, field := range []string{"account", "workspace", "identity", "billing", "cancel"} {
		got := target
		ctx := context.Background()
		switch field {
		case "account":
			got.Account = "other"
		case "workspace":
			got.Workspace = "other"
		case "identity":
			got.CredentialIdentity = "other"
		case "billing":
			got.BillingPath = "payg"
		case "cancel":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		if _, e := c.Load(ctx, got); e == nil {
			t.Fatal("changed credential scope accepted", field)
		}
	}
}

func TestGLMFileCredentialRefusesUnsafeInitialFiles(t *testing.T) {
	for _, mode := range []string{"file_mode", "dir_mode", "root_mode", "symlink", "parent_symlink", "hardlink", "fifo", "git", "empty", "large", "spaces", "control", "placeholder", "scope", "relative"} {
		t.Run(mode, func(t *testing.T) {
			path, scope, _ := credentialFileFixture(t)
			check := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			switch mode {
			case "file_mode":
				check(os.Chmod(path, 0644))
			case "dir_mode":
				check(os.Chmod(filepath.Dir(path), 0755))
			case "root_mode":
				check(os.Chmod(filepath.Dir(filepath.Dir(path)), 0755))
			case "symlink":
				check(os.Rename(path, path+".old"))
				check(os.Symlink(path+".old", path))
			case "parent_symlink":
				dir := filepath.Dir(path)
				check(os.Rename(dir, dir+".old"))
				check(os.Symlink(dir+".old", dir))
			case "hardlink":
				check(os.Link(path, path+".linked"))
			case "fifo":
				check(os.Remove(path))
				check(unix.Mkfifo(path, 0600))
			case "git":
				check(os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(path)), ".git"), []byte("gitdir: fixture"), 0600))
			case "empty":
				check(os.WriteFile(path, nil, 0600))
			case "large":
				check(os.WriteFile(path, []byte(strings.Repeat("x", 4098)), 0600))
			case "spaces":
				check(os.WriteFile(path, []byte(" padded-fixture-key \n"), 0600))
			case "control":
				check(os.WriteFile(path, []byte("fixture\x01key\n"), 0600))
			case "placeholder":
				check(os.WriteFile(path, []byte("REPLACE_WITH_KEY\n"), 0600))
			case "scope":
				scope.Identity = ""
			case "relative":
				path = "credentials/glm-coding-plan.key"
			}
			if _, e := NewFileCredential(path, scope); e == nil || strings.Contains(e.Error(), privateFixtureKey) || strings.Contains(e.Error(), path) {
				t.Fatal("unsafe file accepted or disclosed")
			}
		})
	}
}

func TestGLMFileCredentialRechecksPinnedFileOnEveryLoad(t *testing.T) {
	for _, mode := range []string{"changed_key", "replaced_file", "permission", "directory", "hardlink", "git", "symlink", "missing"} {
		t.Run(mode, func(t *testing.T) {
			path, scope, target := credentialFileFixture(t)
			c, e := NewFileCredential(path, scope)
			if e != nil {
				t.Fatal(e)
			}
			check := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			switch mode {
			case "changed_key":
				check(os.WriteFile(path, []byte("changed-fixture-secret\n"), 0600))
			case "replaced_file":
				check(os.Rename(path, path+".old"))
				check(os.WriteFile(path, []byte(privateFixtureKey+"\n"), 0600))
			case "permission":
				check(os.Chmod(path, 0644))
			case "directory":
				check(os.Chmod(filepath.Dir(path), 0755))
			case "hardlink":
				check(os.Link(path, path+".link"))
			case "git":
				check(os.Mkdir(filepath.Join(filepath.Dir(path), ".git"), 0700))
			case "symlink":
				check(os.Rename(path, path+".old"))
				check(os.Symlink(path+".old", path))
			case "missing":
				check(os.Remove(path))
			}
			if _, e := c.Load(context.Background(), target); e == nil {
				t.Fatal("changed pinned credential accepted")
			}
		})
	}
}
