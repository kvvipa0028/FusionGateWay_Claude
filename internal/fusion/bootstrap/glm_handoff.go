package bootstrap

import (
	"reflect"
	"sort"

	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workflow"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

type glmStageParent struct {
	Binding    handoff.Binding `json:"binding"`
	TreeHash   string          `json:"tree_hash"`
	BaseHash   string          `json:"base_tree_hash"`
	ChangeHash string          `json:"change_hash"`
	HeaderHash string          `json:"header_hash"`
	Evidence   string          `json:"evidence_status"`
	Pending    []string        `json:"pending"`
}

type glmStagePrompt struct {
	Role                       stageplan.Role         `json:"role"`
	Goal                       string                 `json:"goal"`
	Workflow                   *store.WorkflowView    `json:"workflow,omitempty"`
	Parent                     *glmStageParent        `json:"parent,omitempty"`
	WritePaths                 []string               `json:"write_paths,omitempty"`
	Verification               *glmReviewVerification `json:"verification,omitempty"`
	ReviewResponseContract     string                 `json:"review_response_contract,omitempty"`
	Review                     *glmAcceptanceReview   `json:"review,omitempty"`
	AcceptanceResponseContract string                 `json:"acceptance_response_contract,omitempty"`
}

type glmAcceptanceReview struct {
	RunID    string                  `json:"run_id"`
	TextHash string                  `json:"text_hash"`
	Document workflow.ReviewDocument `json:"document"`
}

type glmReviewVerification struct {
	TestingRunID   string `json:"testing_run_id"`
	ArtifactHash   string `json:"artifact_hash"`
	SuiteHash      string `json:"suite_hash"`
	SpecHash       string `json:"spec_hash"`
	AcceptanceHash string `json:"acceptance_hash"`
	ReportHash     string `json:"report_hash"`
	ToolVersion    string `json:"tool_version"`
	ExitCode       int    `json:"exit_code"`
	Tests          int    `json:"tests"`
	Skipped        int    `json:"skipped"`
}

func glmTestingPathsValid(paths []string) bool {
	if !workspace.ValidWritePaths(paths) {
		return false
	}
	for _, p := range paths {
		if p == "." {
			return false
		}
	}
	return true
}

func glmStageWritePaths(role stageplan.Role, in store.StageInput, tests []string) ([]string, bool) {
	if role != stageplan.Implementation && role != stageplan.Testing {
		return nil, true
	}
	var approved []string
	if in.Workflow != nil && len(in.Workflow.Definition.RequiredRoles) > 1 {
		if in.Workflow.Design == nil || in.Workflow.Approval == nil {
			return nil, false
		}
		approved = in.Workflow.Design.Snapshot.Document.Scope
		if !workspace.ValidWritePaths(approved) || len(approved) == 0 {
			return nil, false
		}
	}
	if role == stageplan.Implementation {
		return append([]string(nil), approved...), true
	}
	if len(tests) == 0 || !glmTestingPathsValid(tests) {
		return nil, false
	}
	if len(approved) == 0 {
		return append([]string(nil), tests...), true
	}
	seen := map[string]bool{}
	for _, a := range approved {
		for _, b := range tests {
			if workspace.WritePathAllowed(b, []string{a}) {
				seen[b] = true
			} else if workspace.WritePathAllowed(a, []string{b}) {
				seen[a] = true
			}
		}
	}
	var out []string
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, len(out) > 0 && workspace.ValidWritePaths(out)
}

// Recheck immutable workflow context without interpreting its active-run
// blocker as new authority. The Controller/Store own actual stage transitions.
func glmStageContextCurrent(s *store.Store, task store.Task, input store.StageInput) bool {
	v, err := s.Workflow(task.ID)
	if input.Workflow == nil {
		return err == store.ErrNotFound
	}
	if err != nil || !reflect.DeepEqual(v.Definition, input.Workflow.Definition) || !reflect.DeepEqual(v.Design, input.Workflow.Design) || !reflect.DeepEqual(v.Approval, input.Workflow.Approval) {
		return false
	}
	if input.Parent != nil {
		a, err := s.Artifact(input.Parent.Reference.Binding.RunID)
		return err == nil && reflect.DeepEqual(a, *input.Parent)
	}
	return true
}

// A full finite workflow requires parent handoff and design approval;
// standalone single-role execution retains its existing contract.
func glmHasMultipleRoles(in store.StageInput) bool {
	return in.Workflow != nil && len(in.Workflow.Definition.RequiredRoles) > 1
}
