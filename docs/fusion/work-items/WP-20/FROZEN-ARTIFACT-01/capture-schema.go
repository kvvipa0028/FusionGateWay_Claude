// This fixture serializes the real producer API with synthetic identities and
// evidence. It does not execute Native or establish account/StopProof admission.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

func main() {
	if len(os.Args) != 2 || !filepath.IsAbs(os.Args[1]) {
		panic("explicit fixture output required")
	}
	root, err := os.MkdirTemp("", "fusion-schema-")
	must(err)
	defer os.RemoveAll(root)
	root, err = filepath.EvalSymlinks(root)
	must(err)
	source, state := filepath.Join(root, "source"), filepath.Join(root, "state")
	must(os.Mkdir(source, 0700))
	must(os.Mkdir(state, 0700))
	must(os.WriteFile(filepath.Join(source, "code"), []byte("synthetic base\n"), 0600))
	s, err := workspace.Copy(source, state, "producer")
	must(err)
	must(os.WriteFile(filepath.Join(s.Path, "code"), []byte("synthetic implementation\n"), 0600))
	a, err := workspace.Freeze(s, state, "artifact")
	must(err)
	b := handoff.Binding{TaskID: "task-schema-fixture", PlanRevision: 1, PlanHash: strings.Repeat("a", 64), RunID: "run-schema-fixture", Generation: 1, Role: stageplan.Implementation, TargetHash: strings.Repeat("b", 64)}
	h, err := handoff.Publish(a, b, "synthetic schema goal", handoff.Evidence{OutputHash: strings.Repeat("c", 64), StopProofHash: strings.Repeat("d", 64)})
	must(err)
	doc, err := h.Read(b)
	must(err)
	raw, err := json.MarshalIndent(doc, "", "  ")
	must(err)
	must(os.WriteFile(os.Args[1], append(raw, '\n'), 0600))
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
