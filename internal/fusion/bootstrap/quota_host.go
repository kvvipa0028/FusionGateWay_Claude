package bootstrap

import (
	"context"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/runtime/glm"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// QuotaFactory grants query purpose only. It receives no Scheduler, Store,
// Runtime resolver or route-admission controls. HTTP cannot supply callbacks.
type QuotaFactory func(context.Context, QuotaEnvironment) (QuotaRegistration, error)
type QuotaEnvironment struct {
	Source  *Loaded
	Current func(string) bool
}
type QuotaRegistration struct {
	Sources map[string][]api.QuotaSource
	Close   func(context.Context) error
}

// OpenQuotaControl installs independently authorized readers while keeping all
// declared routes unadmitted and the execution Controller absent. Owned query
// cancellation and cleanup use the same ordered lifetime as execution hosts.
func OpenQuotaControl(parent context.Context, sourcePath, root, addr string, factory QuotaFactory) (*ControlHost, error) {
	if parent == nil || parent.Err() != nil || factory == nil {
		return nil, ErrControlHost
	}
	return openControl(parent, sourcePath, root, addr, func(ctx context.Context, e RuntimeEnvironment) (RuntimeRegistration, error) {
		q, err := factory(ctx, QuotaEnvironment{Source: e.Source, Current: e.Current})
		return RuntimeRegistration{QuotaSources: q.Sources, Close: q.Close}, err
	}, true)
}

// OpenGLMQuotaControl is explicit operator authorization to read the declared
// CN personal Coding Plan route using one private file. Opening performs no
// outbound query: only the authenticated quota refresh endpoint fetches data.
// These local labels do not establish upstream ownership or a physical pool.
func OpenGLMQuotaControl(parent context.Context, sourcePath, root, addr, projectID, routeID, keyPath string) (*ControlHost, error) {
	return OpenQuotaControl(parent, sourcePath, root, addr, glmQuotaFactory(projectID, routeID, keyPath, func(c glm.QuotaReaderConfig) (quota.Fetch, error) {
		reader, err := glm.NewQuotaReader(c)
		if err != nil {
			return nil, err
		}
		return reader.Read, nil
	}))
}

func glmQuotaFactory(projectID, routeID, keyPath string, newReader func(glm.QuotaReaderConfig) (quota.Fetch, error)) QuotaFactory {
	return func(ctx context.Context, e QuotaEnvironment) (QuotaRegistration, error) {
		fail := func() (QuotaRegistration, error) { return QuotaRegistration{}, ErrControlHost }
		if !declared(projectID) || !declared(routeID) || newReader == nil || e.Source == nil || e.Current == nil || ctx.Err() != nil || !e.Current(projectID) {
			return fail()
		}
		p, err := e.Source.Project(projectID)
		if err != nil {
			return fail()
		}
		var route stageplan.Route
		matches := 0
		for _, r := range p.Configuration.Routes {
			if r.ID == routeID {
				route = r
				matches++
			}
		}
		// Of the supported native declarations, only glm-cn-claude derives this
		// billing path. Refuse ambiguous revisions, rather than choosing the first.
		if matches != 1 || route.BillingPath != "coding_plan" || route.PluginVersion != nil {
			return fail()
		}
		for _, project := range e.Source.Projects() {
			if overlaps(project.Path, keyPath) {
				return fail()
			}
		}
		target := stageplan.ExecutionTarget{Route: stageplan.RouteRef{ID: route.ID, Revision: route.Revision}, Account: route.Account, Workspace: route.Workspace, CredentialIdentity: route.CredentialIdentity, BillingPath: route.BillingPath}
		credential, err := glm.NewFileCredential(keyPath, glm.FileCredentialScope{Account: route.Account, Workspace: route.Workspace, Identity: route.CredentialIdentity})
		if err != nil {
			return fail()
		}
		identity := quota.Identity{Provider: "bigmodel", Account: route.Account, Workspace: route.Workspace, Region: "CN", Generation: e.Source.Revision()}
		current := func(ctx context.Context, q quota.Identity) bool {
			if ctx.Err() != nil || q != identity || !e.Current(projectID) {
				return false
			}
			_, err := credential.Load(ctx, target)
			return err == nil && ctx.Err() == nil && e.Current(projectID)
		}
		fetch, err := newReader(glm.QuotaReaderConfig{Identity: identity, Target: target, QueryAllowed: current, LoadCredential: credential.Load})
		if err != nil || fetch == nil || !current(ctx, identity) {
			return fail()
		}
		return QuotaRegistration{Sources: map[string][]api.QuotaSource{projectID: {{Route: target.Route, Identity: identity, Current: current, Fetch: fetch}}}}, nil
	}
}
