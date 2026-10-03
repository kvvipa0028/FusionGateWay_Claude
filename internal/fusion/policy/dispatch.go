package policy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var (
	ErrDispatchDenied = errors.New("strict dispatch denied")
	ErrRouteChanged   = errors.New("frozen route unavailable or changed")
	ErrCallFailed     = errors.New("controlled upstream call failed")
	ErrModelMismatch  = errors.New("upstream reported a different model")
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Model and Effort are optional assertions, never routing instructions. No
// client account, provider, headers, tools, group or arbitrary JSON is accepted.
type DispatchRequest struct {
	Model    string    `json:"model,omitempty"`
	Effort   *string   `json:"effort,omitempty"`
	Messages []Message `json:"messages"`
}
type Invocation struct {
	Target   stageplan.ExecutionTarget `json:"target"`
	Messages []Message                 `json:"messages"`
}
type Reply struct {
	Text                  string  `json:"text"`
	UpstreamReportedModel *string `json:"upstream_reported_model"`
}
type DispatchResult struct {
	RequestedModel        string  `json:"requested_model"`
	ResolvedModel         string  `json:"resolved_model"`
	UpstreamReportedModel *string `json:"upstream_reported_model"`
	Text                  string  `json:"text"`
	Calls                 int     `json:"calls"`
}

// Executor is a trusted server adapter, never supplied by a request/plugin.
// Its versioned admission must cover every call it can make. WP-08 owns that
// admission; this interface alone is not proof of a native Runtime's behavior.
type Executor interface {
	Call(context.Context, Invocation) (Reply, error)
}
type DispatchRoute struct {
	Route       stageplan.Route
	Executor    Executor
	TransportID string
}
type Dispatcher struct {
	Manager *Manager
	Store   *store.Store
	Lookup  func(stageplan.RouteRef) (DispatchRoute, error)
	// Permit is mandatory and rechecked per call: workspace/route permissions,
	// current quota and a shared bounded retry budget. It must not mutate target.
	Permit func(context.Context, Claims, stageplan.ExecutionTarget, int) error
}

// DispatchFailure permits a retry only when an admitted adapter proves no
// output/tool/write effect was delivered. Arbitrary transport errors do not.
type DispatchFailure struct{ Retryable bool }

func (*DispatchFailure) Error() string { return ErrCallFailed.Error() }

func sameJSON(a, b any) bool {
	x, e := json.Marshal(a)
	if e != nil {
		return false
	}
	y, e := json.Marshal(b)
	return e == nil && string(x) == string(y)
}
func copyTarget(in stageplan.ExecutionTarget) stageplan.ExecutionTarget {
	raw, _ := json.Marshal(in)
	var out stageplan.ExecutionTarget
	json.Unmarshal(raw, &out)
	return out
}

func currentTarget(r stageplan.Route, t stageplan.ExecutionTarget) (stageplan.ExecutionTarget, error) {
	if r.PluginVersion != nil || r.LockEnforcement != stageplan.ControlledCalls {
		return stageplan.ExecutionTarget{}, ErrRouteChanged
	}
	e := &stageplan.EffortSelection{Mode: t.Effort.RequestedMode}
	if e.Mode == stageplan.EffortExplicit && t.Effort.Value != nil {
		e.Value = *t.Effort.Value
	}
	b := stageplan.Binding{Mode: stageplan.Locked, Route: &t.Route, Model: t.RequestedModel, Effort: e, RequiredCapabilities: append([]string(nil), t.Capabilities...)}
	p, err := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: b}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{r})
	if err != nil {
		return stageplan.ExecutionTarget{}, ErrRouteChanged
	}
	out := *p.Bindings[stageplan.Design].Target
	if !sameJSON(out, t) {
		return stageplan.ExecutionTarget{}, ErrRouteChanged
	}
	return out, nil
}
func validateDispatch(in DispatchRequest, t stageplan.ExecutionTarget) bool {
	if in.Model != "" && in.Model != t.RequestedModel {
		return false
	}
	if in.Effort != nil && (t.Effort.Value == nil || *in.Effort != *t.Effort.Value) {
		return false
	}
	if len(in.Messages) == 0 || len(in.Messages) > 128 {
		return false
	}
	total := 0
	for _, m := range in.Messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			return false
		}
		total += len(m.Content)
		if total > 64*1024 {
			return false
		}
	}
	return true
}

func (d *Dispatcher) Dispatch(ctx context.Context, in DispatchRequest, maxCalls int) (DispatchResult, error) {
	var out DispatchResult
	if d == nil || d.Manager == nil || d.Store == nil || d.Lookup == nil || d.Permit == nil || maxCalls < 1 || maxCalls > 3 {
		return out, ErrDispatchDenied
	}
	var transportID string
	for ordinal := 1; ordinal <= maxCalls; ordinal++ {
		if ctx.Err() != nil {
			return out, ErrDispatchDenied
		}
		c, err := d.Manager.authorizeContext(ctx)
		if err != nil {
			return out, err
		}
		run, err := d.Store.CheckActive(c.RunID, c.Generation)
		if err != nil || run.State != "running" {
			return out, ErrDispatchDenied
		}
		t := run.Target
		if !validateDispatch(in, t) {
			return out, ErrDispatchDenied
		}
		entry, err := d.Lookup(t.Route)
		if err != nil || entry.Executor == nil || entry.TransportID == "" {
			return out, ErrRouteChanged
		}
		if ordinal == 1 {
			transportID = entry.TransportID
		} else if entry.TransportID != transportID {
			return out, ErrRouteChanged
		}
		if _, err = currentTarget(entry.Route, t); err != nil {
			return out, err
		}
		if err = d.Permit(ctx, c, copyTarget(t), ordinal); err != nil {
			return out, ErrDispatchDenied
		}
		// A budget/permission gate may wait. Resolve the exact revision again;
		// transport/admission changes during that wait cannot escape this exit.
		entry, err = d.Lookup(t.Route)
		if err != nil || entry.Executor == nil || entry.TransportID != transportID {
			return out, ErrRouteChanged
		}
		if _, err = currentTarget(entry.Route, t); err != nil {
			return out, err
		}
		// Permission checks may have waited. Revalidate immediately before send.
		if _, err = d.Manager.authorizeContext(ctx); err != nil {
			return out, err
		}
		if ctx.Err() != nil {
			return out, ErrDispatchDenied
		}
		out.RequestedModel = t.RequestedModel
		out.ResolvedModel = t.ResolvedModel
		reply, err := entry.Executor.Call(ctx, Invocation{Target: copyTarget(t), Messages: append([]Message(nil), in.Messages...)})
		out.Calls++
		if err != nil {
			var failure *DispatchFailure
			if errors.As(err, &failure) && failure.Retryable && ordinal < maxCalls {
				continue
			}
			return out, ErrCallFailed
		}
		if reply.UpstreamReportedModel != nil {
			model := *reply.UpstreamReportedModel
			if len(model) > 256 || strings.ContainsFunc(model, unicode.IsControl) {
				return out, ErrModelMismatch
			}
			out.UpstreamReportedModel = &model
			if model != t.ResolvedModel {
				return out, ErrModelMismatch
			}
		}
		out.Text = reply.Text
		return out, nil
	}
	return out, ErrCallFailed
}
