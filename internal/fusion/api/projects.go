package api

import (
	"net/http"
	"sort"

	"github.com/yetone/magpie/internal/fusion/control"
)

// ProjectReference identifies trusted server registrations. Detailed route
// metadata remains available only through the selected configuration endpoint.
type ProjectReference struct {
	ID                    string `json:"id"`
	ConfigurationRevision int64  `json:"configuration_revision"`
}
type ProjectsReply struct {
	Projects []ProjectReference `json:"projects"`
}

func (s *Server) registeredProjects() (ProjectsReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ProjectsReply{Projects: make([]ProjectReference, 0, len(s.projects))}
	for id := range s.projects {
		p, err := s.currentProjectLocked(id)
		if err != nil {
			return ProjectsReply{}, err
		}
		out.Projects = append(out.Projects, ProjectReference{ID: id, ConfigurationRevision: p.revision})
	}
	sort.Slice(out.Projects, func(i, j int) bool { return out.Projects[i].ID < out.Projects[j].ID })
	return out, nil
}
func (s *Server) projectsControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	out, err := s.registeredProjects()
	// A blocked Store read or competing registration never extends authority.
	if !s.auth.ManagementCurrent(r.Context()) {
		controlFailure(w, control.ErrForbidden)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, http.StatusOK, out)
}
