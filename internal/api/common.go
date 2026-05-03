package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NetUnion/pve-manage/internal/config"
	"github.com/NetUnion/pve-manage/internal/model"
	"golang.org/x/crypto/ssh"
)

var (
	sgNamePattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}[a-z0-9]$|^[a-z0-9]$`)
	usernameTokenRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)
)

const (
	nodePlacementCPUWeight = 0.35
	nodePlacementMemWeight = 0.65
)

type apiError struct {
	Error string `json:"error"`
}

type userEnvelope struct {
	Username string        `json:"username"`
	Email    string        `json:"email"`
	Name     string        `json:"name"`
	Groups   []string      `json:"groups"`
	IsAdmin  bool          `json:"is_admin"`
	Quota    quotaEnvelope `json:"quota"`
	Usage    usageEnvelope `json:"usage"`
}

type quotaEnvelope struct {
	Number        int    `json:"number"`
	CPU           int    `json:"cpu"`
	Memory        int    `json:"memory"`
	Storage       int    `json:"storage"`
	SecurityGroup int    `json:"security_group"`
	UESTC         string `json:"uestc"`
}

type usageEnvelope struct {
	Count         int `json:"count"`
	CPU           int `json:"cpu"`
	Memory        int `json:"memory"`
	Storage       int `json:"storage"`
	SecurityGroup int `json:"security_group"`
}

type vmSummary struct {
	ID                 int64           `json:"id"`
	OwnerUsername      string          `json:"owner_username"`
	ClusterKey         string          `json:"cluster_key"`
	VMID               int             `json:"vmid"`
	VMName             string          `json:"vmname"`
	IP                 string          `json:"ip"`
	Node               string          `json:"node"`
	TargetNode         string          `json:"target_node"`
	Password           string          `json:"-"`
	SSHKeys            []string        `json:"sshkeys"`
	SharedUsernames    []string        `json:"shared_usernames"`
	SecurityGroupName  string          `json:"security_group_name"`
	UESTCRestricted    bool            `json:"uestc_restricted"`
	Managed            bool            `json:"managed"`
	TaskQueuePaused    bool            `json:"task_queue_paused"`
	Config             json.RawMessage `json:"config"`
	PreferStatus       json.RawMessage `json:"prefer_status"`
	RealStatus         json.RawMessage `json:"real_status"`
	SyncState          string          `json:"sync_state"`
	SyncError          *string         `json:"sync_error,omitempty"`
	Version            int             `json:"version"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
	DeletedAt          *string         `json:"deleted_at,omitempty"`
	DeleteRequestedAt  *string         `json:"delete_requested_at,omitempty"`
	DeleteExecuteAfter *string         `json:"delete_execute_after,omitempty"`
	Metrics            *vmMetrics      `json:"metrics,omitempty"`
	MetricsHistory     []vmMetricPoint `json:"metrics_history,omitempty"`
}

type vmMetrics struct {
	WindowSeconds int     `json:"window_seconds"`
	Samples       int     `json:"samples"`
	CPU           float64 `json:"cpu"`
	Memory        float64 `json:"memory"`
	DiskIO        float64 `json:"disk_io"`
	Network       float64 `json:"network"`
}

type vmMetricPoint struct {
	Time    string  `json:"time"`
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
	DiskIO  float64 `json:"disk_io"`
	Network float64 `json:"network"`
}

type vmTaskSummary struct {
	ID         int64           `json:"id"`
	VMID       int64           `json:"vm_id"`
	Seq        int             `json:"seq"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
	Status     string          `json:"status"`
	Error      *string         `json:"error,omitempty"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
	StartedAt  *string         `json:"started_at,omitempty"`
	FinishedAt *string         `json:"finished_at,omitempty"`
}

type maintenanceTaskSummary struct {
	ID         int64           `json:"id"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
	Status     string          `json:"status"`
	Error      *string         `json:"error,omitempty"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
	StartedAt  *string         `json:"started_at,omitempty"`
	FinishedAt *string         `json:"finished_at,omitempty"`
}

type securityGroupSummary struct {
	ID            int64           `json:"id"`
	OwnerUsername string          `json:"owner_username"`
	Name          string          `json:"name"`
	Rules         json.RawMessage `json:"rules"`
	PolicyIn      string          `json:"policy_in"`
	PolicyOut     string          `json:"policy_out"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

type sshKeySummary struct {
	ID            int64  `json:"id"`
	OwnerUsername string `json:"owner_username"`
	Name          string `json:"name"`
	PublicKey     string `json:"public_key"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type templateSummary struct {
	ID           int64           `json:"id"`
	ClusterKey   string          `json:"cluster_key"`
	TemplateVMID int             `json:"template_vmid"`
	Name         string          `json:"name"`
	Description  *string         `json:"description,omitempty"`
	OSType       *string         `json:"os_type,omitempty"`
	RealStatus   json.RawMessage `json:"real_status"`
	LastSeenAt   string          `json:"last_seen_at"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

type nodePlacementCandidate struct {
	Node         string
	AllocatedCPU int
	AllocatedMem int
	RequestedCPU int
	RequestedMem int
	CPULimit     int
	MemoryLimit  int
}

type quota struct {
	Number        int
	CPU           int
	Memory        int
	Storage       int
	SecurityGroup int
	UESTC         string
}

func (s *Server) jsonError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func decodeJSONBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func parseJSONStrings(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func rawJSONFromString(raw string) json.RawMessage {
	if raw == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(raw)
}

func normalizeFirewallPolicyDisplay(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "drop":
		return "drop"
	case "accept":
		return "accept"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func (s *Server) loadVMTasks(ctx context.Context, vmID int64) ([]vmTaskSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, vm_id, seq, kind, payload_json, status, error, created_at, updated_at, started_at, finished_at
		FROM vm_tasks
		WHERE vm_id = ?
		ORDER BY seq, id
	`, vmID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]vmTaskSummary, 0)
	for rows.Next() {
		var item vmTaskSummary
		var payloadRaw string
		var startedAt sql.NullString
		var finishedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.VMID, &item.Seq, &item.Kind, &payloadRaw, &item.Status, &item.Error, &item.CreatedAt, &item.UpdatedAt, &startedAt, &finishedAt); err != nil {
			return nil, err
		}
		item.Payload = rawJSONFromString(payloadRaw)
		if startedAt.Valid {
			value := startedAt.String
			item.StartedAt = &value
		}
		if finishedAt.Valid {
			value := finishedAt.String
			item.FinishedAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadMaintenanceTasks(ctx context.Context) ([]maintenanceTaskSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, payload_json, status, error, created_at, updated_at, started_at, finished_at
		FROM maintenance_tasks
		ORDER BY id DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]maintenanceTaskSummary, 0)
	for rows.Next() {
		var item maintenanceTaskSummary
		var payloadRaw string
		var startedAt sql.NullString
		var finishedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.Kind, &payloadRaw, &item.Status, &item.Error, &item.CreatedAt, &item.UpdatedAt, &startedAt, &finishedAt); err != nil {
			return nil, err
		}
		item.Payload = rawJSONFromString(payloadRaw)
		if startedAt.Valid {
			value := startedAt.String
			item.StartedAt = &value
		}
		if finishedAt.Valid {
			value := finishedAt.String
			item.FinishedAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadVMTask(ctx context.Context, vmID, taskID int64) (*vmTaskSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, vm_id, seq, kind, payload_json, status, error, created_at, updated_at, started_at, finished_at
		FROM vm_tasks
		WHERE vm_id = ? AND id = ?
	`, vmID, taskID)

	var item vmTaskSummary
	var payloadRaw string
	var startedAt sql.NullString
	var finishedAt sql.NullString
	if err := row.Scan(&item.ID, &item.VMID, &item.Seq, &item.Kind, &payloadRaw, &item.Status, &item.Error, &item.CreatedAt, &item.UpdatedAt, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	item.Payload = rawJSONFromString(payloadRaw)
	if startedAt.Valid {
		value := startedAt.String
		item.StartedAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.String
		item.FinishedAt = &value
	}
	return &item, nil
}

func queueVMTaskTx(ctx context.Context, tx *sql.Tx, vmID int64, kind string, payload any) error {
	var nextSeq int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) + 1
		FROM vm_tasks
		WHERE vm_id = ?
	`, vmID).Scan(&nextSeq); err != nil {
		return err
	}
	payloadJSON := mustJSON(payload)
	now := timestamp()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO vm_tasks(vm_id, seq, kind, payload_json, status, created_at, updated_at)
		VALUES(?,?,?,?, 'pending', ?, ?)
	`, vmID, nextSeq, kind, payloadJSON, now, now)
	return err
}

func queueVMTaskConn(ctx context.Context, conn *sql.Conn, vmID int64, kind string, payload any) error {
	var nextSeq int
	if err := conn.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) + 1
		FROM vm_tasks
		WHERE vm_id = ?
	`, vmID).Scan(&nextSeq); err != nil {
		return err
	}
	payloadJSON := mustJSON(payload)
	now := timestamp()
	_, err := conn.ExecContext(ctx, `
		INSERT INTO vm_tasks(vm_id, seq, kind, payload_json, status, created_at, updated_at)
		VALUES(?,?,?,?, 'pending', ?, ?)
	`, vmID, nextSeq, kind, payloadJSON, now, now)
	return err
}

func normalizeStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeSSHKeyList(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		line, err := normalizeSSHKeyLine(value)
		if err != nil {
			return nil, err
		}
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeSSHKeyLine(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r", ""))
	if value == "" || strings.HasPrefix(value, "#") {
		return "", nil
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(value))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))), nil
}

func validSecurityGroupName(name string) bool {
	return sgNamePattern.MatchString(name)
}

func validUsernameLike(name string) bool {
	return usernameTokenRegex.MatchString(name)
}

func prefixedVMName(owner, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || owner == "" {
		return name
	}
	prefix := owner + "-"
	if strings.HasPrefix(name, prefix) {
		return name
	}
	return prefix + name
}

func normalizeCIDR(input string, ethertype string) (string, error) {
	ip := net.ParseIP(input)
	if ip == nil {
		_, n, err := net.ParseCIDR(input)
		if err != nil {
			return "", err
		}
		if ethertype == "ipv4" && n.IP.To4() == nil {
			return "", fmt.Errorf("cidr must be ipv4")
		}
		if ethertype == "ipv6" && n.IP.To4() != nil {
			return "", fmt.Errorf("cidr must be ipv6")
		}
		return n.String(), nil
	}

	if ethertype == "ipv4" {
		return ip.To4().String() + "/32", nil
	}
	if ethertype == "ipv6" {
		return ip.String() + "/128", nil
	}
	return "", fmt.Errorf("unsupported ethertype %q", ethertype)
}

func parsePort(v *int, raw any) error {
	if raw == nil {
		*v = 0
		return nil
	}
	switch n := raw.(type) {
	case float64:
		*v = int(n)
	case int:
		*v = n
	case int64:
		*v = int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return err
		}
		*v = int(i)
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return err
		}
		*v = i
	default:
		return fmt.Errorf("invalid port type %T", raw)
	}
	return nil
}

func randomPassword(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if n <= 0 {
		n = 16
	}
	out := make([]byte, n)
	for i := range out {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			out[i] = alphabet[i%len(alphabet)]
			continue
		}
		out[i] = alphabet[v.Int64()]
	}
	return string(out)
}

func isSQLiteUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed: vms.cluster_key, vms.vmid") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Server) currentUser(r *http.Request) (*model.User, error) {
	return s.auth.CurrentUser(r)
}

func (s *Server) currentGroups(r *http.Request) ([]string, *model.User, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return nil, nil, err
	}
	return parseJSONStrings(user.GroupsJSON), user, nil
}

func (s *Server) adminAndUser(r *http.Request) (*model.User, bool, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return nil, false, err
	}
	return user, user.IsAdmin, nil
}

func parseNullableTime(raw sql.NullString) *string {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	value := raw.String
	return &value
}

func parseTimeString(raw string) string {
	return raw
}

func (s *Server) loadUserRow(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	var createdAt, updatedAt string
	var lastLogin sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, name, groups_json, is_active, is_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE username = ?
	`, username).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Name,
		&user.GroupsJSON,
		&user.IsActive,
		&user.IsAdmin,
		&createdAt,
		&updatedAt,
		&lastLogin,
	)
	if err != nil {
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		user.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		user.UpdatedAt = t
	}
	if lastLogin.Valid {
		if t, err := time.Parse(time.RFC3339Nano, lastLogin.String); err == nil {
			user.LastLoginAt = &t
		}
	}
	return &user, nil
}

func (s *Server) getClusterConfig(clusterKey string) (config.Cluster, error) {
	cluster, ok := s.config.ClusterByKey(clusterKey)
	if !ok {
		return config.Cluster{}, fmt.Errorf("unknown cluster %q", clusterKey)
	}
	return cluster, nil
}

func (s *Server) effectiveQuota(user *model.User) quota {
	limits := s.config.EffectiveUserLimit(parseJSONStrings(user.GroupsJSON))
	return quota{
		Number:        limits.Number,
		CPU:           limits.CPU,
		Memory:        limits.Memory,
		Storage:       limits.Storage,
		SecurityGroup: limits.SecurityGroup,
		UESTC:         limits.UESTC,
	}
}

func (s *Server) listUserVMUsage(ctx context.Context, username string) (count, cpu, memory, storage int, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT config_json
		FROM vms
		WHERE owner_username = ? AND deleted_at IS NULL AND managed = 1
	`, username)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return 0, 0, 0, 0, err
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			continue
		}
		count++
		if v, ok := cfg["cpu_cores"]; ok {
			if n, ok := intFromAny(v); ok {
				cpu += n
			}
		}
		if v, ok := cfg["memory_gb"]; ok {
			if n, ok := intFromAny(v); ok {
				memory += n
			}
		}
		if v, ok := cfg["disk_gb"]; ok {
			if n, ok := intFromAny(v); ok {
				storage += n
			}
		}
	}
	return count, cpu, memory, storage, rows.Err()
}

func (s *Server) countUserSecurityGroups(ctx context.Context, username string) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM security_groups
		WHERE owner_username = ?
	`, username).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Server) clusterVMCounts(ctx context.Context, clusterKey string) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM vms
		WHERE cluster_key = ? AND deleted_at IS NULL
	`, clusterKey).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type vmIDQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func nextClusterVMID(ctx context.Context, q vmIDQuerier, clusterKey string, startVMID, limit int) (int, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT vmid
		FROM vms
		WHERE cluster_key = ? AND deleted_at IS NULL
		ORDER BY vmid
	`, clusterKey)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	used := make(map[int]struct{}, limit)
	for rows.Next() {
		var vmid int
		if err := rows.Scan(&vmid); err != nil {
			return 0, err
		}
		used[vmid] = struct{}{}
	}

	for vmid := startVMID; vmid < startVMID+limit; vmid++ {
		if _, ok := used[vmid]; !ok {
			return vmid, nil
		}
	}
	return 0, fmt.Errorf("no vmid available in cluster %s", clusterKey)
}

func (s *Server) nextClusterVMID(ctx context.Context, clusterKey string, startVMID, limit int) (int, error) {
	return nextClusterVMID(ctx, s.db, clusterKey, startVMID, limit)
}

func (s *Server) deriveIPv4(cluster config.Cluster, bridgeKey string, vmid int) (string, error) {
	bridge, ok := cluster.BridgeByKey(bridgeKey)
	if !ok {
		return "", fmt.Errorf("unknown bridge %q", bridgeKey)
	}
	base, ok := netParseIPv4(bridge.IPv4.StartIP)
	if !ok {
		return "", fmt.Errorf("bridge %s has invalid ipv4 start_ip", bridgeKey)
	}
	offset := vmid - cluster.StartVMID
	ip, err := addIPv4(base, offset)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%d", ip.String(), bridge.IPv4.CIDR), nil
}

func (s *Server) choosePlacementNode(ctx context.Context, clusterKey string, cluster config.Cluster, cpuKey string, requestedCores int, requestedMemoryGB int, templateReal json.RawMessage) (string, error) {
	cpu, ok := cluster.CPUByKey(cpuKey)
	if !ok {
		return "", fmt.Errorf("unknown cpu_key")
	}
	candidates := normalizeStringList(cpu.Node)
	if len(candidates) == 0 {
		return templateNodeFromRealStatus(templateReal), nil
	}

	templateNode := templateNodeFromRealStatus(templateReal)
	best, found, err := s.lowestStaticAllocationNode(ctx, clusterKey, candidates, cpu, requestedCores, requestedMemoryGB, templateNode)
	if err != nil {
		return "", err
	}
	if found {
		return best, nil
	}
	if containsString(candidates, templateNode) {
		return templateNode, nil
	}
	return candidates[0], nil
}

func (s *Server) lowestStaticAllocationNode(ctx context.Context, clusterKey string, nodes []string, cpu config.CPUClass, requestedCores int, requestedMemoryGB int, preferredTie string) (string, bool, error) {
	var best nodePlacementCandidate
	found := false
	for _, node := range nodes {
		allocatedCPU, allocatedMem, err := s.nodeStaticAllocation(ctx, clusterKey, node)
		if err != nil {
			return "", false, err
		}
		current := nodePlacementCandidate{
			Node:         node,
			AllocatedCPU: allocatedCPU,
			AllocatedMem: allocatedMem,
			RequestedCPU: requestedCores,
			RequestedMem: requestedMemoryGB,
			CPULimit:     cpu.Limit,
			MemoryLimit:  cpu.MemoryLimit,
		}
		if !found || betterStaticAllocationNode(current, best, preferredTie) {
			best = current
			found = true
		}
	}
	return best.Node, found, nil
}

func (s *Server) nodeStaticAllocation(ctx context.Context, clusterKey string, node string) (cpu int, memory int, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT config_json
		FROM vms
		WHERE cluster_key = ?
		  AND deleted_at IS NULL
		  AND managed = 1
		  AND (
		    node = ?
		    OR (COALESCE(node, '') = '' AND target_node = ?)
		  )
	`, clusterKey, node, node)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return 0, 0, err
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			continue
		}
		cpu += intFromOrZero(cfg, "cpu_cores")
		memory += intFromOrZero(cfg, "memory_gb")
	}
	return cpu, memory, rows.Err()
}

func betterStaticAllocationNode(current, best nodePlacementCandidate, preferredTie string) bool {
	currentScore := staticAllocationScore(current)
	bestScore := staticAllocationScore(best)
	if currentScore < bestScore-1e-9 {
		return true
	}
	if currentScore > bestScore+1e-9 {
		return false
	}
	if projectedMemRatio(current) < projectedMemRatio(best)-1e-9 {
		return true
	}
	if projectedMemRatio(current) > projectedMemRatio(best)+1e-9 {
		return false
	}
	if projectedCPURatio(current) < projectedCPURatio(best)-1e-9 {
		return true
	}
	if projectedCPURatio(current) > projectedCPURatio(best)+1e-9 {
		return false
	}
	if current.Node == preferredTie && best.Node != preferredTie {
		return true
	}
	if current.Node != preferredTie && best.Node == preferredTie {
		return false
	}
	return current.Node < best.Node
}

func staticAllocationScore(candidate nodePlacementCandidate) float64 {
	return projectedCPURatio(candidate)*nodePlacementCPUWeight + projectedMemRatio(candidate)*nodePlacementMemWeight
}

func projectedCPURatio(candidate nodePlacementCandidate) float64 {
	return resourceRatio(candidate.AllocatedCPU+candidate.RequestedCPU, candidate.CPULimit)
}

func projectedMemRatio(candidate nodePlacementCandidate) float64 {
	return resourceRatio(candidate.AllocatedMem+candidate.RequestedMem, candidate.MemoryLimit)
}

func resourceRatio(value int, limit int) float64 {
	if value <= 0 || limit <= 0 {
		return 0
	}
	return float64(value) / float64(limit)
}

func templateNodeFromRealStatus(raw json.RawMessage) string {
	var real map[string]any
	if err := json.Unmarshal(raw, &real); err != nil {
		return ""
	}
	node, _ := real["node"].(string)
	return strings.TrimSpace(node)
}

func (s *Server) loadVMRow(ctx context.Context, id int64) (*vmSummary, error) {
	return loadVMRow(ctx, s.db, id)
}

type vmRowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadVMRow(ctx context.Context, q vmRowQuerier, id int64) (*vmSummary, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, owner_username, cluster_key, vmid, vmname, ip, node, target_node, password, sshkeys_json, shared_usernames_json,
		       security_group_name, uestc_restricted, managed, task_queue_paused, config_json, prefer_status_json, real_status_json,
		       sync_state, sync_error, version, created_at, updated_at, deleted_at, delete_requested_at, delete_execute_after, metrics_json
		FROM vms
		WHERE id = ?
	`, id)

	var item vmSummary
	var sshkeys, shared string
	var configRaw, preferRaw, realRaw, metricsRaw string
	var node, targetNode sql.NullString
	var deletedAt, deleteRequestedAt, deleteExecuteAfter sql.NullString
	if err := row.Scan(
		&item.ID,
		&item.OwnerUsername,
		&item.ClusterKey,
		&item.VMID,
		&item.VMName,
		&item.IP,
		&node,
		&targetNode,
		&item.Password,
		&sshkeys,
		&shared,
		&item.SecurityGroupName,
		&item.UESTCRestricted,
		&item.Managed,
		&item.TaskQueuePaused,
		&configRaw,
		&preferRaw,
		&realRaw,
		&item.SyncState,
		&item.SyncError,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
		&deletedAt,
		&deleteRequestedAt,
		&deleteExecuteAfter,
		&metricsRaw,
	); err != nil {
		return nil, err
	}
	if node.Valid {
		item.Node = node.String
	}
	if targetNode.Valid {
		item.TargetNode = targetNode.String
	}
	item.Config = rawJSONFromString(configRaw)
	item.PreferStatus = rawJSONFromString(preferRaw)
	item.RealStatus = rawJSONFromString(realRaw)
	item.SSHKeys = parseJSONStrings(sshkeys)
	item.SharedUsernames = parseJSONStrings(shared)
	item.DeletedAt = parseNullableTime(deletedAt)
	item.DeleteRequestedAt = parseNullableTime(deleteRequestedAt)
	item.DeleteExecuteAfter = parseNullableTime(deleteExecuteAfter)
	item.MetricsHistory = parseVMMetricPoints(metricsRaw)
	return &item, nil
}

func (s *Server) visibleVM(ctx context.Context, id int64, user *model.User) (*vmSummary, error) {
	item, err := s.loadVMRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if user.IsAdmin || user.Username == item.OwnerUsername {
		return item, nil
	}
	if containsString(item.SharedUsernames, user.Username) {
		return item, nil
	}
	return nil, sql.ErrNoRows
}

func (s *Server) loadSecurityGroupRow(ctx context.Context, owner, name string) (*securityGroupSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, owner_username, name, rules_json, policy_in, policy_out, created_at, updated_at
		FROM security_groups
		WHERE owner_username = ? AND name = ?
	`, owner, name)

	var item securityGroupSummary
	var rulesRaw string
	if err := row.Scan(&item.ID, &item.OwnerUsername, &item.Name, &rulesRaw, &item.PolicyIn, &item.PolicyOut, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.PolicyIn = normalizeFirewallPolicyDisplay(item.PolicyIn)
	item.PolicyOut = normalizeFirewallPolicyDisplay(item.PolicyOut)
	item.Rules = rawJSONFromString(rulesRaw)
	return &item, nil
}

func (s *Server) loadTemplateRow(ctx context.Context, clusterKey string, templateVMID int) (*templateSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, cluster_key, template_vmid, name, description, os_type, real_status_json, last_seen_at, created_at, updated_at
		FROM templates
		WHERE cluster_key = ? AND template_vmid = ?
	`, clusterKey, templateVMID)

	var item templateSummary
	var realRaw string
	if err := row.Scan(
		&item.ID,
		&item.ClusterKey,
		&item.TemplateVMID,
		&item.Name,
		&item.Description,
		&item.OSType,
		&realRaw,
		&item.LastSeenAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	item.RealStatus = rawJSONFromString(realRaw)
	return &item, nil
}

func (s *Server) loadSSHKeyRow(ctx context.Context, ownerUsername string, id int64) (*sshKeySummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, owner_username, name, public_key, created_at, updated_at
		FROM ssh_keys
		WHERE owner_username = ? AND id = ?
	`, ownerUsername, id)

	var item sshKeySummary
	if err := row.Scan(&item.ID, &item.OwnerUsername, &item.Name, &item.PublicKey, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Server) loadSSHKeyRows(ctx context.Context, ownerUsername string) ([]sshKeySummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_username, name, public_key, created_at, updated_at
		FROM ssh_keys
		WHERE owner_username = ?
		ORDER BY name, id
	`, ownerUsername)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]sshKeySummary, 0)
	for rows.Next() {
		var item sshKeySummary
		if err := rows.Scan(&item.ID, &item.OwnerUsername, &item.Name, &item.PublicKey, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadSSHKeysByIDs(ctx context.Context, ownerUsername string, ids []int64) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		item, err := s.loadSSHKeyRow(ctx, ownerUsername, id)
		if err != nil {
			return nil, err
		}
		keys = append(keys, item.PublicKey)
	}
	return normalizeSSHKeyList(keys)
}

func (s *Server) loadSSHKeysByIDsAny(ctx context.Context, ids []int64) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		item, err := s.loadSSHKeyRowAny(ctx, id)
		if err != nil {
			return nil, err
		}
		keys = append(keys, item.PublicKey)
	}
	return normalizeSSHKeyList(keys)
}

func (s *Server) loadSSHKeyRowAny(ctx context.Context, id int64) (*sshKeySummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, owner_username, name, public_key, created_at, updated_at
		FROM ssh_keys
		WHERE id = ?
	`, id)

	var item sshKeySummary
	if err := row.Scan(&item.ID, &item.OwnerUsername, &item.Name, &item.PublicKey, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Server) visibleSecurityGroup(ctx context.Context, ownerUsername, name string, current *model.User) (*securityGroupSummary, error) {
	item, err := s.loadSecurityGroupRow(ctx, ownerUsername, name)
	if err != nil {
		return nil, err
	}
	if current.IsAdmin || current.Username == ownerUsername {
		return item, nil
	}
	return nil, sql.ErrNoRows
}

func (s *Server) listVMsForUser(ctx context.Context, current *model.User) ([]vmSummary, error) {
	return s.listVMs(ctx, current, false)
}

func (s *Server) listAllVMs(ctx context.Context) ([]vmSummary, error) {
	return s.listVMs(ctx, nil, true)
}

func (s *Server) listVMs(ctx context.Context, current *model.User, includeAll bool) ([]vmSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_username, cluster_key, vmid, vmname, ip, node, target_node, password, sshkeys_json, shared_usernames_json,
		       security_group_name, uestc_restricted, managed, config_json, prefer_status_json, real_status_json,
		       sync_state, sync_error, version, created_at, updated_at, deleted_at, delete_requested_at, delete_execute_after, metrics_json
		FROM vms
		WHERE deleted_at IS NULL
		ORDER BY cluster_key, vmid
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]vmSummary, 0)
	for rows.Next() {
		var item vmSummary
		var sshkeys, shared string
		var configRaw, preferRaw, realRaw, metricsRaw string
		var node, targetNode sql.NullString
		var deletedAt, deleteRequestedAt, deleteExecuteAfter sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.OwnerUsername,
			&item.ClusterKey,
			&item.VMID,
			&item.VMName,
			&item.IP,
			&node,
			&targetNode,
			&item.Password,
			&sshkeys,
			&shared,
			&item.SecurityGroupName,
			&item.UESTCRestricted,
			&item.Managed,
			&configRaw,
			&preferRaw,
			&realRaw,
			&item.SyncState,
			&item.SyncError,
			&item.Version,
			&item.CreatedAt,
			&item.UpdatedAt,
			&deletedAt,
			&deleteRequestedAt,
			&deleteExecuteAfter,
			&metricsRaw,
		); err != nil {
			return nil, err
		}
		if node.Valid {
			item.Node = node.String
		}
		if targetNode.Valid {
			item.TargetNode = targetNode.String
		}
		item.Config = rawJSONFromString(configRaw)
		item.PreferStatus = rawJSONFromString(preferRaw)
		item.RealStatus = rawJSONFromString(realRaw)
		item.SSHKeys = parseJSONStrings(sshkeys)
		item.SharedUsernames = parseJSONStrings(shared)
		item.DeletedAt = parseNullableTime(deletedAt)
		item.DeleteRequestedAt = parseNullableTime(deleteRequestedAt)
		item.DeleteExecuteAfter = parseNullableTime(deleteExecuteAfter)
		item.MetricsHistory = parseVMMetricPoints(metricsRaw)
		if includeAll || (item.Managed && current != nil && (current.Username == item.OwnerUsername || containsString(item.SharedUsernames, current.Username))) {
			items = append(items, item)
		}
	}
	return items, rows.Err()
}

func (vm *vmSummary) SharedUsernamesToJSON() string {
	if len(vm.SharedUsernames) == 0 {
		return "[]"
	}
	return mustJSON(vm.SharedUsernames)
}

func (s *Server) vmVisibleToCurrentUser(vm *vmSummary, current *model.User) bool {
	return current.IsAdmin || current.Username == vm.OwnerUsername || containsString(vm.SharedUsernames, current.Username)
}

func parseVMMetricPoints(raw string) []vmMetricPoint {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var points []vmMetricPoint
	if err := json.Unmarshal([]byte(raw), &points); err != nil {
		return nil
	}
	return points
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
