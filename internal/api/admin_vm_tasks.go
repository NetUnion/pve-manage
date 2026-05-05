package api

import (
	"net/http"
	"strings"
)

func (s *Server) handleAdminListVMTasks(w http.ResponseWriter, r *http.Request) {
	current, err := s.requireAdmin(r)
	if err != nil {
		s.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	_ = current

	includeCompleted := strings.TrimSpace(r.URL.Query().Get("include_completed")) == "1"
	items, err := s.loadAllVMTasks(r.Context(), includeCompleted)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
