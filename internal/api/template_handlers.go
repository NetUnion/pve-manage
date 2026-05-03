package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/NetUnion/pve-manage/internal/model"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	_, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	clusterKey := r.URL.Query().Get("cluster_key")
	query := `
		SELECT id, cluster_key, template_vmid, name, description, os_type, real_status_json, last_seen_at, created_at, updated_at
		FROM templates
	`
	args := make([]any, 0)
	if clusterKey != "" {
		query += " WHERE cluster_key = ?"
		args = append(args, clusterKey)
	}
	query += " ORDER BY cluster_key, template_vmid"
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := make([]templateSummary, 0)
	for rows.Next() {
		var item templateSummary
		var realRaw string
		if err := rows.Scan(&item.ID, &item.ClusterKey, &item.TemplateVMID, &item.Name, &item.Description, &item.OSType, &realRaw, &item.LastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		item.RealStatus = rawJSONFromString(realRaw)
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAdminListVMs(w http.ResponseWriter, r *http.Request) {
	_, err := s.requireAdmin(r)
	if err != nil {
		s.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	items, err := s.listAllVMs(r.Context())
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r); err != nil {
		s.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, username, email, name, groups_json, is_active, is_admin, created_at, updated_at, last_login_at
		FROM users
		ORDER BY username
	`)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := make([]userEnvelope, 0)
	for rows.Next() {
		var user model.User
		if err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.Name, &user.GroupsJSON, &user.IsActive, &user.IsAdmin, new(string), new(string), new(sql.NullString)); err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, userEnvelope{
			Username: user.Username,
			Email:    user.Email,
			Name:     user.Name,
			Groups:   parseJSONStrings(user.GroupsJSON),
			IsAdmin:  user.IsAdmin,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAdminListSecurityGroups(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r); err != nil {
		s.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, owner_username, name, rules_json, policy_in, policy_out, created_at, updated_at
		FROM security_groups
		ORDER BY owner_username, name
	`)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := make([]securityGroupSummary, 0)
	for rows.Next() {
		var item securityGroupSummary
		var rulesRaw string
		if err := rows.Scan(&item.ID, &item.OwnerUsername, &item.Name, &rulesRaw, &item.PolicyIn, &item.PolicyOut, &item.CreatedAt, &item.UpdatedAt); err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		item.PolicyIn = normalizeFirewallPolicyDisplay(item.PolicyIn)
		item.PolicyOut = normalizeFirewallPolicyDisplay(item.PolicyOut)
		item.Rules = rawJSONFromString(rulesRaw)
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAdminListSSHKeys(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r); err != nil {
		s.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, owner_username, name, public_key, created_at, updated_at
		FROM ssh_keys
		ORDER BY owner_username, name, id
	`)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := make([]sshKeySummary, 0)
	for rows.Next() {
		var item sshKeySummary
		if err := rows.Scan(&item.ID, &item.OwnerUsername, &item.Name, &item.PublicKey, &item.CreatedAt, &item.UpdatedAt); err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type adminPatchVMRequest struct {
	IP string `json:"ip"`
}

func (s *Server) handleAdminPatchVM(w http.ResponseWriter, r *http.Request) {
	current, err := s.requireAdmin(r)
	if err != nil {
		s.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	_ = current
	id, err := parseIDParam(r, "id")
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid vm id")
		return
	}
	vm, err := s.loadVMRow(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.jsonError(w, http.StatusNotFound, "vm not found")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "unmanaged vm ip cannot be modified")
		return
	}
	if vm.SyncState == "deleting" || vm.DeleteRequestedAt != nil {
		s.jsonError(w, http.StatusBadRequest, "vm is pending deletion")
		return
	}
	var req adminPatchVMRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.IP == "" {
		s.jsonError(w, http.StatusBadRequest, "ip is required")
		return
	}
	cluster, err := s.getClusterConfig(vm.ClusterKey)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	bridge, ok := cluster.BridgeByKey("custom")
	if !ok {
		for _, candidate := range cluster.Network.Bridge {
			bridge = candidate
			ok = true
			break
		}
	}
	if !ok {
		s.jsonError(w, http.StatusBadRequest, "cluster has no bridge")
		return
	}
	if _, _, err := net.ParseCIDR(req.IP); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid cidr")
		return
	}
	var conflict int
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(1)
		FROM vms
		WHERE cluster_key = ? AND ip = ? AND id != ? AND deleted_at IS NULL
	`, vm.ClusterKey, req.IP, vm.ID).Scan(&conflict); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if conflict > 0 {
		s.jsonError(w, http.StatusBadRequest, "ip already used")
		return
	}
	now := timestamp()
	cfg := map[string]any{}
	_ = json.Unmarshal(vm.Config, &cfg)
	cfg["ip"] = req.IP
	cfg["gateway"] = bridge.IPv4.Gateway
	cfgBytes, _ := json.Marshal(cfg)
	prefer := map[string]any{}
	_ = json.Unmarshal(vm.PreferStatus, &prefer)
	prefer["ip"] = req.IP
	prefer["gateway"] = bridge.IPv4.Gateway
	prefer["generation"] = intFromOrZero(prefer, "generation") + 1
	preferBytes, _ := json.Marshal(prefer)
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE vms
		SET ip = ?, config_json = ?, prefer_status_json = ?, sync_state = 'pending', sync_error = NULL, updated_at = ?, version = version + 1
		WHERE id = ?
	`, req.IP, string(cfgBytes), string(preferBytes), now, vm.ID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func parseIDParam(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, name), 10, 64)
}
