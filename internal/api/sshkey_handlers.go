package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type sshKeyRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

func validSSHKeyName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == ' ':
		default:
			return false
		}
		if i == 0 && (r == '-' || r == '_' || r == '.' || r == ' ') {
			return false
		}
	}
	return true
}

func (s *Server) handleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	items, err := s.loadSSHKeyRows(r.Context(), current.Username)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateSSHKey(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req sshKeyRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !validSSHKeyName(req.Name) {
		s.jsonError(w, http.StatusBadRequest, "invalid ssh key name")
		return
	}
	publicKey, err := normalizeSSHKeyLine(req.PublicKey)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid ssh public key: %v", err))
		return
	}
	if publicKey == "" {
		s.jsonError(w, http.StatusBadRequest, "invalid ssh public key")
		return
	}
	now := timestamp()
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO ssh_keys(owner_username, name, public_key, created_at, updated_at)
		VALUES($1,$2,$3,$4,$5)
	`, current.Username, req.Name, publicKey, now, now); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid ssh key id")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		DELETE FROM ssh_keys
		WHERE owner_username = $1 AND id = $2
	`, current.Username, id)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		s.jsonError(w, http.StatusNotFound, "ssh key not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
