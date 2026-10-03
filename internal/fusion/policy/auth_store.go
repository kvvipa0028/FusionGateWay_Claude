package policy

import "github.com/yetone/magpie/internal/fusion/store"

// StoreValidator authorizes a capability scope against the persisted active
// attempt. Route/quota/workspace admission remains a separate execution gate.
func StoreValidator(s *store.Store) func(Claims) bool {
	return func(c Claims) bool {
		if s == nil || !validScope(c) {
			return false
		}
		r, err := s.CheckActive(c.RunID, c.Generation)
		if err != nil || r.TaskID != c.TaskID || r.Role != c.Role || r.Attempt != c.Attempt || r.PlanRevision != c.PlanRevision {
			return false
		}
		t, err := s.Task(c.TaskID)
		if err != nil || t.ProjectID != c.ProjectID || t.Generation != c.Generation || t.State != "running" {
			return false
		}
		if c.Audience == ModelAudience {
			return r.State == "running"
		}
		return r.State == "starting" || r.State == "running" || r.State == "cancelling"
	}
}
