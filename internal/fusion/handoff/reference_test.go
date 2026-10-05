package handoff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

func referenceFixture(t *testing.T) (Bundle, Binding, string, string) {
	t.Helper()
	p := t.TempDir()
	p, _ = filepath.EvalSymlinks(p)
	source, root := filepath.Join(p, "source"), filepath.Join(p, "state")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "code"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := workspace.Copy(source, root, "producer")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Path, "code"), []byte("actual edited code"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := workspace.Freeze(s, root, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	b := Binding{TaskID: "task-reference", PlanRevision: 1, PlanHash: strings.Repeat("a", 64), RunID: "run-reference", Generation: 1, Role: stageplan.Implementation, TargetHash: strings.Repeat("b", 64)}
	h, err := Publish(a, b, "reference goal", Evidence{OutputHash: strings.Repeat("c", 64), StopProofHash: strings.Repeat("d", 64)})
	if err != nil {
		t.Fatal(err)
	}
	return h, b, source, root
}

func TestHandoffReferenceRehydratesExactCopyWithAllAncestors(t *testing.T) {
	h, b, source, root := referenceFixture(t)
	ref, err := h.Reference(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Reference
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	h = Bundle{} // Restart discards the producer's memory authority.
	restored, err := Restore(persisted, source, root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := restored.Copy(b, root, "testing")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Path, "code"))
	if err != nil || string(data) != "actual edited code" {
		t.Fatal("restart lost code", err)
	}
	if err := os.WriteFile(filepath.Join(s.Path, "test"), []byte("new test"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := workspace.Freeze(s, root, "testing-artifact")
	if err != nil {
		t.Fatal(err)
	}
	b2 := b
	b2.RunID = "run-testing"
	b2.Generation++
	b2.Role = stageplan.Testing
	h2, err := Publish(a, b2, "reference goal", Evidence{OutputHash: strings.Repeat("e", 64), StopProofHash: strings.Repeat("f", 64)})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := h2.Reference(root)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Restore(r2, source, root)
	if err != nil {
		t.Fatal(err)
	}
	third, err := reloaded.Copy(b2, root, "review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(third.Path, "test")); err != nil {
		t.Fatal(err)
	}
	// Header provenance must survive every copy and restart, not only code.
	p := filepath.Join(ref.Path, "handoff.json")
	if err := os.Chmod(p, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("tampered parent header"), 0600); err != nil {
		t.Fatal(err)
	}
	if third.SourceCurrent() {
		t.Fatal("ancestor header drift accepted")
	}
	if _, err := Restore(r2, source, root); err == nil {
		t.Fatal("corrupt ancestor rehydrated")
	}
}

func TestHandoffReferenceRejectsChangedIdentityAndUntrustedScope(t *testing.T) {
	for _, mode := range []string{"witness", "header", "code", "original_replacement", "root_replacement", "source", "root", "binding", "tree", "version"} {
		t.Run(mode, func(t *testing.T) {
			h, _, source, root := referenceFixture(t)
			ref, err := h.Reference(root)
			if err != nil {
				t.Fatal(err)
			}
			good := ref
			switch mode {
			case "witness":
				ref.Witness = append(json.RawMessage(nil), ref.Witness...)
				ref.Witness[0] = '['
			case "header", "code":
				p := filepath.Join(ref.Path, "handoff.json")
				if mode == "code" {
					p = filepath.Join(ref.Path, "code", "code")
				}
				if err := os.Chmod(p, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "original_replacement":
				p := filepath.Join(source, "code")
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			case "root_replacement":
				old := root + "-old"
				if err := os.Rename(root, old); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(old, "artifact"), filepath.Join(root, "artifact")); err != nil {
					t.Fatal(err)
				}
			case "source":
				source = t.TempDir()
				source, _ = filepath.EvalSymlinks(source)
			case "root":
				root = filepath.Dir(root)
			case "binding":
				ref.Binding.RunID = "other-run"
			case "tree":
				ref.TreeHash = strings.Repeat("f", 64)
			case "version":
				ref.Version = 2
			}
			if _, err := Restore(ref, source, root); err == nil {
				t.Fatal("changed authority accepted")
			}
			if mode == "witness" && !reflect.DeepEqual(good.Binding, ref.Binding) {
				t.Fatal("fixture changed binding")
			}
		})
	}
}
