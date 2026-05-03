package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type securityGroupRule struct {
	Direction string `json:"direction"`
	Action    string `json:"action"`
	Ethertype string `json:"ethertype"`
	Protocol  string `json:"protocol"`
	CIDR      string `json:"cidr"`
	PortStart *int   `json:"port_start,omitempty"`
	PortEnd   *int   `json:"port_end,omitempty"`
}

type securityGroupRequest struct {
	Name      string              `json:"name"`
	PolicyIn  string              `json:"policy_in"`
	PolicyOut string              `json:"policy_out"`
	Rules     []securityGroupRule `json:"rules"`
}

func (s *Server) handleListSecurityGroups(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, owner_username, name, rules_json, policy_in, policy_out, created_at, updated_at
		FROM security_groups
		WHERE owner_username = ?
		ORDER BY owner_username, name
	`, current.Username)
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

func (s *Server) handleGetSecurityGroup(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	name := chi.URLParam(r, "name")
	row := s.db.QueryRowContext(r.Context(), `
		SELECT id, owner_username, name, rules_json, policy_in, policy_out, created_at, updated_at
		FROM security_groups
		WHERE owner_username = ? AND name = ?
	`, current.Username, name)
	var item securityGroupSummary
	var rulesRaw string
	if err := row.Scan(&item.ID, &item.OwnerUsername, &item.Name, &rulesRaw, &item.PolicyIn, &item.PolicyOut, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.jsonError(w, http.StatusNotFound, "security group not found")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	item.PolicyIn = normalizeFirewallPolicyDisplay(item.PolicyIn)
	item.PolicyOut = normalizeFirewallPolicyDisplay(item.PolicyOut)
	item.Rules = rawJSONFromString(rulesRaw)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleCreateSecurityGroup(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req securityGroupRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !validSecurityGroupName(req.Name) {
		s.jsonError(w, http.StatusBadRequest, "invalid security group name")
		return
	}
	quota := s.effectiveQuota(current)
	if quota.SecurityGroup <= 0 {
		s.jsonError(w, http.StatusForbidden, "security group quota not configured")
		return
	}
	count, err := s.countUserSecurityGroups(r.Context(), current.Username)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count+1 > quota.SecurityGroup {
		s.jsonError(w, http.StatusBadRequest, "security group quota exceeded")
		return
	}
	rulesJSON, err := normalizeSecurityGroupRules(req.Rules)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	policyIn, policyOut, err := normalizeFirewallPolicies(req.PolicyIn, req.PolicyOut)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := timestamp()
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO security_groups(owner_username, name, rules_json, policy_in, policy_out, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?)
	`, current.Username, req.Name, rulesJSON, policyIn, policyOut, now, now); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handlePatchSecurityGroup(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	name := chi.URLParam(r, "name")
	var req securityGroupRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name != "" && req.Name != name {
		s.jsonError(w, http.StatusBadRequest, "renaming security group is not supported")
		return
	}
	rulesJSON, err := normalizeSecurityGroupRules(req.Rules)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	policyIn, policyOut, err := normalizeFirewallPolicies(req.PolicyIn, req.PolicyOut)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := timestamp()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(r.Context(), `
		UPDATE security_groups
		SET rules_json = ?, policy_in = ?, policy_out = ?, updated_at = ?
		WHERE owner_username = ? AND name = ?
	`, rulesJSON, policyIn, policyOut, now, current.Username, name)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		s.jsonError(w, http.StatusNotFound, "security group not found")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE vms
		SET sync_state = 'pending',
		    sync_error = NULL,
		    updated_at = ?,
		    version = version + 1
		WHERE owner_username = ?
		  AND security_group_name = ?
		  AND deleted_at IS NULL
		  AND managed = 1
		  AND sync_state != 'deleting'
	`, now, current.Username, name); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "requeued": true})
}

func (s *Server) handleDeleteSecurityGroup(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	name := chi.URLParam(r, "name")
	var refCount int
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(1)
		FROM vms
		WHERE owner_username = ? AND security_group_name = ? AND deleted_at IS NULL
	`, current.Username, name).Scan(&refCount); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if refCount > 0 {
		s.jsonError(w, http.StatusBadRequest, "security group is still in use")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		DELETE FROM security_groups
		WHERE owner_username = ? AND name = ?
	`, current.Username, name)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		s.jsonError(w, http.StatusNotFound, "security group not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func normalizeSecurityGroupRules(rules []securityGroupRule) (string, error) {
	normalized := make([]securityGroupRule, 0, len(rules))
	for _, rule := range rules {
		switch rule.Direction {
		case "in", "out":
		default:
			return "", fmt.Errorf("invalid direction %q", rule.Direction)
		}
		switch rule.Action {
		case "accept", "drop":
		default:
			return "", fmt.Errorf("invalid action %q", rule.Action)
		}
		switch rule.Ethertype {
		case "ipv4", "ipv6":
		default:
			return "", fmt.Errorf("invalid ethertype %q", rule.Ethertype)
		}
		switch rule.Protocol {
		case "tcp", "udp", "icmp":
		default:
			return "", fmt.Errorf("invalid protocol %q", rule.Protocol)
		}
		cidr, err := normalizeCIDR(rule.CIDR, rule.Ethertype)
		if err != nil {
			return "", err
		}
		rule.CIDR = cidr
		if rule.Protocol == "icmp" {
			if rule.PortStart != nil || rule.PortEnd != nil {
				return "", errors.New("icmp rules must not include ports")
			}
		} else {
			if rule.PortStart != nil && *rule.PortStart < 0 {
				return "", errors.New("port_start must be positive")
			}
			if rule.PortEnd != nil && *rule.PortEnd < 0 {
				return "", errors.New("port_end must be positive")
			}
			if rule.PortStart != nil && rule.PortEnd != nil && *rule.PortStart > *rule.PortEnd {
				return "", errors.New("port_start must not exceed port_end")
			}
		}
		rule.Action = normalizeFirewallAction(rule.Action)
		normalized = append(normalized, rule)
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func normalizeFirewallPolicies(policyIn, policyOut string) (string, string, error) {
	in := normalizeFirewallPolicyInput(policyIn)
	out := normalizeFirewallPolicyInput(policyOut)
	switch in {
	case "ACCEPT", "DROP":
	default:
		return "", "", fmt.Errorf("invalid policy_in %q", policyIn)
	}
	switch out {
	case "ACCEPT", "DROP":
	default:
		return "", "", fmt.Errorf("invalid policy_out %q", policyOut)
	}
	return in, out, nil
}

func normalizeFirewallAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "accept":
		return "accept"
	case "drop":
		return "drop"
	default:
		return strings.ToLower(strings.TrimSpace(action))
	}
}

func normalizeFirewallPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", "accept":
		return "ACCEPT"
	case "drop":
		return "DROP"
	default:
		return strings.ToUpper(strings.TrimSpace(policy))
	}
}

func normalizeFirewallPolicyInput(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "ACCEPT":
		return "ACCEPT"
	case "DROP":
		return "DROP"
	default:
		return strings.ToUpper(strings.TrimSpace(value))
	}
}

func intPtr(v int) *int { return &v }
