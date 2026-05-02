package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/NetUnion/pve-manage/internal/model"
)

func (s *Server) requireUser(r *http.Request) (*model.User, error) {
	return s.auth.CurrentUser(r)
}

func (s *Server) requireAdmin(r *http.Request) (*model.User, error) {
	user, err := s.requireUser(r)
	if err != nil {
		return nil, err
	}
	if !user.IsAdmin {
		return nil, errors.New("admin privileges required")
	}
	return user, nil
}

func parseJSONStringSlice(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func toJSONString(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func isVisibleTo(user *model.User, vmOwner string, sharedUsernames []string) bool {
	if user.IsAdmin || user.Username == vmOwner {
		return true
	}
	for _, shared := range sharedUsernames {
		if shared == user.Username {
			return true
		}
	}
	return false
}

func boolFromJSON(raw json.RawMessage, key string) (bool, bool) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, false
	}
	v, ok := obj[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func intFromAny(v any) (int, bool) {
	switch vv := v.(type) {
	case float64:
		return int(vv), true
	case int:
		return vv, true
	case int64:
		return int(vv), true
	default:
		return 0, false
	}
}

func netParseIPv4(s string) (net.IP, bool) {
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, false
	}
	ip = ip.To4()
	return ip, ip != nil
}

func addIPv4(base net.IP, offset int) (net.IP, error) {
	ip4 := base.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("base IP must be IPv4")
	}
	out := make(net.IP, len(ip4))
	copy(out, ip4)
	carry := offset
	for i := 3; i >= 0; i-- {
		sum := int(out[i]) + (carry & 0xff)
		out[i] = byte(sum & 0xff)
		carry = (carry >> 8) + (sum >> 8)
	}
	if carry != 0 {
		return nil, fmt.Errorf("ipv4 offset overflow")
	}
	return out, nil
}

func timestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func queryOneRowString(ctx context.Context, db *sql.DB, query string, args ...any) (string, error) {
	var value string
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

func userGroupsJSON(user *model.User) []string {
	return parseJSONStringSlice(user.GroupsJSON)
}

func hasGroup(groups []string, group string) bool {
	for _, g := range groups {
		if strings.TrimSpace(g) == group {
			return true
		}
	}
	return false
}
