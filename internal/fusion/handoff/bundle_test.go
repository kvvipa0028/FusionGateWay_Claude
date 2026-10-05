package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

func handoffFixture(t *testing.T) (workspace.FrozenArtifact, Binding, Evidence, string) {
	t.Helper()
	root, source := t.TempDir(), t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	root, _ = filepath.EvalSymlinks(root)
	source, _ = filepath.EvalSymlinks(source)
	if err := os.WriteFile(filepath.Join(source, "code"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := workspace.Copy(source, root, "implementation")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Path, "code"), []byte("actual implementation"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := workspace.Freeze(s, root, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	b := Binding{TaskID: "task-fixture", PlanRevision: 1, PlanHash: strings.Repeat("a", 64), RunID: "run-fixture", Generation: 1, Role: stageplan.Implementation, TargetHash: strings.Repeat("b", 64)}
	e := Evidence{OutputHash: strings.Repeat("c", 64), StopProofHash: strings.Repeat("d", 64)}
	return a, b, e, root
}

func TestHandoffCopiesCodeOnlyForExactBinding(t *testing.T) {
	a, b, e, root := handoffFixture(t)
	h, err := Publish(a, b, "frozen task goal", e)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := h.Read(b)
	if err != nil || doc.Version != 1 || doc.Task.Goal != "frozen task goal" || doc.Evidence.TestsExecuted || doc.Evidence.Status != "unverified" || len(doc.Decisions) != 0 || len(doc.Pending) == 0 || len(doc.Changes) != 1 {
		t.Fatal("invented evidence or missing package", err, doc)
	}
	doc.Task.Goal = "forged"
	doc.Artifact.Entries = nil
	if current, err := h.Read(b); err != nil || current.Task.Goal != "frozen task goal" || len(current.Artifact.Entries) == 0 {
		t.Fatal("public mutation changed bundle", err)
	}
	s, err := h.Copy(b, root, "testing")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Path, "code"))
	if err != nil || string(raw) != "actual implementation" {
		t.Fatal("handoff lost actual code", err)
	}
	for _, field := range []string{"task", "run", "plan", "generation", "role", "target"} {
		t.Run(field, func(t *testing.T) {
			bad := b
			switch field {
			case "task":
				bad.TaskID = "other"
			case "run":
				bad.RunID = "other"
			case "plan":
				bad.PlanHash = strings.Repeat("e", 64)
			case "generation":
				bad.Generation++
			case "role":
				bad.Role = stageplan.Review
			case "target":
				bad.TargetHash = strings.Repeat("e", 64)
			}
			if _, err := h.Read(bad); err == nil {
				t.Fatal("cross-binding bundle accepted")
			}
			if _, err := h.Copy(bad, root, "denied-"+field); err == nil {
				t.Fatal("cross-binding code transferred")
			}
			if _, err := os.Stat(filepath.Join(root, "denied-"+field)); !os.IsNotExist(err) {
				t.Fatal("unauthorized copy published")
			}
		})
	}
	if _, err := Publish(a, b, "same task", e); err == nil {
		t.Fatal("existing bundle overwritten")
	}
}

func TestHandoffRejectsHeaderAndCodeCorruption(t *testing.T) {
	for _, kind := range []string{"header", "code", "header_link", "source"} {
		t.Run(kind, func(t *testing.T) {
			a, b, e, root := handoffFixture(t)
			h, err := Publish(a, b, "goal", e)
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(a.Path(), "handoff.json")
			switch kind {
			case "header":
				if err := os.Chmod(p, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(`{"version":1,"tests_executed":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "code":
				p = filepath.Join(a.Path(), "code", "code")
				if err := os.Chmod(p, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "header_link":
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(a.Path(), "manifest.json"), p); err != nil {
					t.Fatal(err)
				}
			case "source":
				if err := os.Rename(a.Path(), a.Path()+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(a.Path(), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := h.Read(b); err == nil {
				t.Fatal("tampered bundle accepted")
			}
			if _, err := h.Copy(b, root, "denied"); err == nil {
				t.Fatal("tampered code transferred")
			}
		})
	}
}

func TestHandoffCannotInventSuccessOrAcceptUnownedArtifact(t *testing.T) {
	a, b, e, _ := handoffFixture(t)
	bad := b
	bad.PlanHash = "not a digest"
	if _, err := Publish(a, bad, "goal", e); err == nil {
		t.Fatal("bad binding")
	}
	if _, err := Publish(workspace.FrozenArtifact{}, b, "goal", e); err == nil {
		t.Fatal("unowned code")
	}
	if _, err := (Bundle{}).Read(b); err == nil {
		t.Fatal("unowned bundle")
	}
	if _, err := Publish(a, b, strings.Repeat("x", 65<<10), e); err == nil {
		t.Fatal("unbounded goal")
	}
	badEvidence := e
	badEvidence.StopProofHash = ""
	if _, err := Publish(a, b, "goal", badEvidence); err == nil {
		t.Fatal("missing process-stop evidence")
	}
}
