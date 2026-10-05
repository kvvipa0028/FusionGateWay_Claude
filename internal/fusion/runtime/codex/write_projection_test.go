//go:build darwin || linux

package codex

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// Official 0.160.0 wire shapes: apply_patch is a custom (freeform) tool with a
// lark grammar; model tool calls arrive as function_call/custom_tool_call
// items and Native replies with the matching *_output items.
const applyPatchTool = `{"type":"custom","name":"apply_patch","description":"Apply a patch","format":{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}}`

const writeGateBody = `{"model":"fixture-model","instructions":"fixture","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"fixture"}]},{"type":"custom_tool_call","call_id":"call-fixture-1","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"},{"type":"custom_tool_call_output","call_id":"call-fixture-1","name":"apply_patch","output":"done"}],"tools":[` + applyPatchTool + `],"tool_choice":"auto","parallel_tool_calls":false,"reasoning":{"effort":"high"},"store":false,"stream":true,"include":["reasoning.encrypted_content"]}`

func TestCodexWriteToolRequestProjection(t *testing.T) {
	for _, mode := range []string{"apply-patch", "function-tool", "history-function-call", "unknown-tool-type", "malformed-name", "too-many-tools", "oversized-arguments", "unknown-item-field", "shell-tool-namespace"} {
		t.Run(mode, func(t *testing.T) {
			body := writeGateBody
			switch mode {
			case "function-tool":
				body = strings.Replace(writeGateBody, applyPatchTool, `{"type":"function","name":"read_file","description":"d","parameters":{"type":"object"},"strict":true}`, 1)
			case "history-function-call":
				body = strings.Replace(writeGateBody, `"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"fixture"}]},`, `"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"fixture"}]},{"type":"function_call","name":"fixture_tool","call_id":"call-fixture-2","arguments":"{}"},{"type":"function_call_output","call_id":"call-fixture-2","name":"fixture_tool","output":"{}"},`, 1)
			case "unknown-tool-type":
				body = strings.Replace(writeGateBody, applyPatchTool, `{"type":"web_search"}`, 1)
			case "malformed-name":
				body = strings.Replace(writeGateBody, `"name":"apply_patch","description"`, `"name":"bad name","description"`, 1)
			case "too-many-tools":
				crowd := make([]string, 33)
				for i := range crowd {
					crowd[i] = applyPatchTool
				}
				body = strings.Replace(writeGateBody, applyPatchTool, strings.Join(crowd, ","), 1)
			case "oversized-arguments":
				body = strings.Replace(writeGateBody, `"input":"*** Begin Patch\n*** End Patch"`, `"input":"`+strings.Repeat("x", 65<<10)+`"`, 1)
			case "unknown-item-field":
				body = strings.Replace(writeGateBody, `"call_id":"call-fixture-1","name":"apply_patch","input"`, `"call_id":"call-fixture-1","name":"apply_patch","secret":true,"input"`, 1)
			case "shell-tool-namespace":
				body = strings.Replace(writeGateBody, applyPatchTool, `{"type":"custom","name":"shell","format":{"type":"grammar","syntax":"lark","definition":"start: anything"}}`, 1)
			}
			g := &CallGate{binding: Binding{Target: fixtureWriteTarget()}}
			ok := g.request([]byte(body))
			if mode == "shell-tool-namespace" || mode == "unknown-tool-type" || mode == "malformed-name" || mode == "too-many-tools" || mode == "oversized-arguments" || mode == "unknown-item-field" {
				if ok {
					t.Fatal("unsafe tool projection accepted")
				}
				return
			}
			if !ok {
				t.Fatal("official write projection rejected", mode)
			}
		})
	}
}

func TestCodexWriteToolResponseItems(t *testing.T) {
	for _, mode := range []string{"custom-tool-call", "function-call", "unknown-item", "bad-call-id"} {
		t.Run(mode, func(t *testing.T) {
			var item string
			switch mode {
			case "custom-tool-call":
				item = `{"type":"custom_tool_call","id":"tc_fixture","call_id":"call_fixture","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"}`
			case "function-call":
				item = `{"type":"function_call","id":"fc_fixture","call_id":"call_fixture2","name":"fixture_tool","arguments":"{}"}`
			case "unknown-item":
				item = `{"type":"local_shell_call","call_id":"call_x","action":{}}`
			case "bad-call-id":
				item = `{"type":"function_call","id":"fc_fixture","call_id":"bad call","name":"fixture_tool","arguments":"{}"}`
			}
			_, _, _, itemOK := outputItem([]byte(item), false)
			if mode == "custom-tool-call" || mode == "function-call" {
				if !itemOK {
					t.Fatal("official tool call item rejected", mode)
				}
			} else if itemOK {
				t.Fatal("unsafe item accepted", mode)
			}
			events := []map[string]any{
				{"type": "response.created", "response": map[string]any{"id": "resp_fixture", "status": "in_progress", "model": "fixture-model", "output": []any{}}},
				{"type": "response.output_item.added", "output_index": 0, "item": json.RawMessage(item)},
				{"type": "response.output_item.done", "output_index": 0, "item": json.RawMessage(item)},
				{"type": "response.completed", "response": map[string]any{"id": "resp_fixture", "status": "completed", "model": "fixture-model", "output": []any{json.RawMessage(item)}, "usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}}},
			}
			var b strings.Builder
			for i, e := range events {
				e["sequence_number"] = i
				raw, _ := json.Marshal(e)
				fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", e["type"], raw)
			}
			streamOK := responsesStream([]byte(b.String()), "fixture-model")
			if mode == "custom-tool-call" || mode == "function-call" {
				if !streamOK {
					t.Fatal("official tool call stream rejected", mode)
				}
			} else if streamOK {
				t.Fatal("unsafe stream accepted", mode)
			}
		})
	}
}

func fixtureWriteTarget() stageplan.ExecutionTarget {
	return stageplan.ExecutionTarget{Route: stageplan.RouteRef{ID: "fixture-native", Revision: 1}, RequestedModel: "fixture-model", ResolvedModel: "fixture-model", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-identity", RuntimeVersion: CLIVersion, BillingPath: "subscription", LockEnforcement: stageplan.ControlledCalls, Effort: stageplan.FrozenEffort{RequestedMode: stageplan.EffortExplicit, Value: ptr("high")}}
}
