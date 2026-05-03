package api

import (
	"net/http"
)

func (s *Server) handleAdminListMaintenanceTasks(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if !current.IsAdmin {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	items, err := s.loadMaintenanceTasks(r.Context())
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
