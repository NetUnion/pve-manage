package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NetUnion/pve-manage/internal/vmname"
	"github.com/go-chi/chi/v5"
)

type createVMRequest struct {
	ClusterKey        string   `json:"cluster_key"`
	VMName            string   `json:"vmname"`
	Password          *string  `json:"password"`
	CPUKey            string   `json:"cpu_key"`
	CPUCores          int      `json:"cpu_cores"`
	MemoryGB          int      `json:"memory_gb"`
	StorageKey        string   `json:"storage_key"`
	DiskGB            int      `json:"disk_gb"`
	BridgeKey         string   `json:"bridge_key"`
	BootOrder         string   `json:"boot_order"`
	TemplateVMID      int      `json:"template_vmid"`
	SSHKeys           []string `json:"sshkeys"`
	SSHKeyIDs         []int64  `json:"ssh_key_ids"`
	SharedUsernames   []string `json:"shared_usernames"`
	SecurityGroupName string   `json:"security_group_name"`
	UESTCRestricted   *bool    `json:"uestc_restricted"`
	Power             string   `json:"power"`
}

type patchVMRequest struct {
	OwnerUsername     *string   `json:"owner_username"`
	VMName            *string   `json:"vmname"`
	Password          *string   `json:"password"`
	CPUKey            *string   `json:"cpu_key"`
	CPUCores          *int      `json:"cpu_cores"`
	MemoryGB          *int      `json:"memory_gb"`
	StorageKey        *string   `json:"storage_key"`
	DiskGB            *int      `json:"disk_gb"`
	BridgeKey         *string   `json:"bridge_key"`
	BootOrder         *string   `json:"boot_order"`
	TemplateVMID      *int      `json:"template_vmid"`
	SSHKeys           *[]string `json:"sshkeys"`
	SSHKeyIDs         *[]int64  `json:"ssh_key_ids"`
	SharedUsernames   *[]string `json:"shared_usernames"`
	SecurityGroupName *string   `json:"security_group_name"`
	UESTCRestricted   *bool     `json:"uestc_restricted"`
	QuotaExempt       *bool     `json:"quota_exempt"`
	Power             *string   `json:"power"`
}

type powerVMRequest struct {
	Power string `json:"power"`
}

func patchVMHasNonPowerChanges(req patchVMRequest) bool {
	return req.OwnerUsername != nil ||
		req.VMName != nil ||
		req.Password != nil ||
		req.CPUKey != nil ||
		req.CPUCores != nil ||
		req.MemoryGB != nil ||
		req.StorageKey != nil ||
		req.DiskGB != nil ||
		req.BridgeKey != nil ||
		req.BootOrder != nil ||
		req.TemplateVMID != nil ||
		req.SSHKeys != nil ||
		req.SSHKeyIDs != nil ||
		req.SharedUsernames != nil ||
		req.SecurityGroupName != nil ||
		req.UESTCRestricted != nil ||
		req.QuotaExempt != nil
}

type adoptVMRequest struct {
	OwnerUsername      string   `json:"owner_username"`
	VMName             string   `json:"vmname"`
	IP                 string   `json:"ip"`
	Password           *string  `json:"password"`
	CPUKey             string   `json:"cpu_key"`
	CPUCores           int      `json:"cpu_cores"`
	MemoryGB           int      `json:"memory_gb"`
	StorageKey         string   `json:"storage_key"`
	DiskGB             int      `json:"disk_gb"`
	BridgeKey          string   `json:"bridge_key"`
	BootOrder          string   `json:"boot_order"`
	SSHKeys            []string `json:"sshkeys"`
	SSHKeyIDs          []int64  `json:"ssh_key_ids"`
	SharedUsernames    []string `json:"shared_usernames"`
	SecurityGroupOwner string   `json:"security_group_owner"`
	SecurityGroupName  string   `json:"security_group_name"`
	UESTCRestricted    *bool    `json:"uestc_restricted"`
	Power              string   `json:"power"`
}

type createVMResponse struct {
	VM
	Password string `json:"password"`
}

type vmDetailResponse struct {
	VM
	Password   string          `json:"password"`
	ConsoleURL string          `json:"console_url,omitempty"`
	Tasks      []vmTaskSummary `json:"tasks"`
}

func queueVMPowerTaskTx(ctx context.Context, tx *sql.Tx, vm *vmSummary, action string) error {
	prefer := map[string]any{}
	_ = json.Unmarshal(vm.PreferStatus, &prefer)
	prefer["power"] = action
	prefer["generation"] = intFromOrZero(prefer, "generation") + 1
	preferBytes, _ := json.Marshal(prefer)
	now := timestamp()
	if _, err := tx.ExecContext(ctx, `
		UPDATE vms
		SET prefer_status_json = $1, sync_state = 'pending', sync_error = NULL, updated_at = $2
		WHERE id = $3
	`, string(preferBytes), now, vm.ID); err != nil {
		return err
	}
	return queueVMTaskTx(ctx, tx, vm.ID, "power", map[string]any{"vm_id": vm.ID, "power": action})
}

type VM struct {
	ID                 int64           `json:"id"`
	OwnerUsername      string          `json:"owner_username"`
	ClusterKey         string          `json:"cluster_key"`
	VMID               int             `json:"vmid"`
	VMName             string          `json:"vmname"`
	IP                 string          `json:"ip"`
	Node               string          `json:"node"`
	TargetNode         string          `json:"target_node"`
	SSHKeys            []string        `json:"sshkeys"`
	SharedUsernames    []string        `json:"shared_usernames"`
	SecurityGroupName  string          `json:"security_group_name"`
	UESTCRestricted    bool            `json:"uestc_restricted"`
	QuotaExempt        bool            `json:"quota_exempt"`
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

func (s *Server) handleListVMs(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	items, err := s.listVMsForUser(r.Context(), current)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetVM(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
	if !s.vmVisibleToCurrentUser(vm, current) {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	tasks, err := s.loadVMTasks(r.Context(), vm.ID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, vmDetailResponse{
		VM: VM{
			ID:                 vm.ID,
			OwnerUsername:      vm.OwnerUsername,
			ClusterKey:         vm.ClusterKey,
			VMID:               vm.VMID,
			VMName:             vm.VMName,
			IP:                 vm.IP,
			Node:               vm.Node,
			TargetNode:         vm.TargetNode,
			SSHKeys:            vm.SSHKeys,
			SharedUsernames:    vm.SharedUsernames,
			SecurityGroupName:  vm.SecurityGroupName,
			UESTCRestricted:    vm.UESTCRestricted,
			QuotaExempt:        vm.QuotaExempt,
			Managed:            vm.Managed,
			TaskQueuePaused:    vm.TaskQueuePaused,
			Config:             vm.Config,
			PreferStatus:       vm.PreferStatus,
			RealStatus:         vm.RealStatus,
			SyncState:          vm.SyncState,
			SyncError:          vm.SyncError,
			Version:            vm.Version,
			CreatedAt:          vm.CreatedAt,
			UpdatedAt:          vm.UpdatedAt,
			DeletedAt:          vm.DeletedAt,
			DeleteRequestedAt:  vm.DeleteRequestedAt,
			DeleteExecuteAfter: vm.DeleteExecuteAfter,
			Metrics:            vm.Metrics,
			MetricsHistory:     vm.MetricsHistory,
		},
		Password: func() string {
			if current.Username == vm.OwnerUsername {
				return vm.Password
			}
			return ""
		}(),
		ConsoleURL: func() string {
			if current.Username == vm.OwnerUsername {
				return s.vmConsoleURL(vm)
			}
			return ""
		}(),
		Tasks: tasks,
	})
}

func (s *Server) handleCreateVM(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var req createVMRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.VMName = strings.TrimSpace(req.VMName)
	if req.ClusterKey == "" || req.VMName == "" || req.CPUKey == "" || req.StorageKey == "" || req.BridgeKey == "" || req.SecurityGroupName == "" {
		s.jsonError(w, http.StatusBadRequest, "missing required fields")
		return
	}
	if err := vmname.ValidateManaged(current.Username, req.VMName); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.CPUCores <= 0 || req.MemoryGB <= 0 || req.DiskGB <= 0 {
		s.jsonError(w, http.StatusBadRequest, "cpu_cores, memory_gb and disk_gb must be positive")
		return
	}
	if req.DiskGB < 20 {
		s.jsonError(w, http.StatusBadRequest, "disk_gb must be at least 20")
		return
	}
	if !validSecurityGroupName(req.SecurityGroupName) {
		s.jsonError(w, http.StatusBadRequest, "invalid security_group_name")
		return
	}
	if req.Power == "" {
		req.Power = "running"
	}
	if req.Power != "running" && req.Power != "stopped" {
		s.jsonError(w, http.StatusBadRequest, "power must be running or stopped")
		return
	}
	bootOrder, err := normalizeBootOrder(req.BootOrder)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Password != nil {
		if strings.TrimSpace(*req.Password) == "" {
			req.Password = nil
		}
	}
	if len(req.SSHKeys) == 0 {
		req.SSHKeys = []string{}
	}
	if len(req.SSHKeyIDs) > 0 {
		sshKeys, err := s.loadSSHKeysByIDs(r.Context(), current.Username, req.SSHKeyIDs)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				s.jsonError(w, http.StatusBadRequest, "ssh key not found")
				return
			}
			s.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.SSHKeys = sshKeys
	} else {
		if normalized, err := normalizeSSHKeyList(req.SSHKeys); err != nil {
			s.jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid ssh key: %v", err))
			return
		} else {
			req.SSHKeys = normalized
		}
	}
	req.SharedUsernames = normalizeStringList(req.SharedUsernames)
	for _, shared := range req.SharedUsernames {
		if !validUsernameLike(shared) {
			s.jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid shared username %q", shared))
			return
		}
	}
	if containsString(req.SharedUsernames, current.Username) {
		s.jsonError(w, http.StatusBadRequest, "owner cannot be in shared_usernames")
		return
	}

	cluster, err := s.getClusterConfig(req.ClusterKey)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cpuOpt, ok := cluster.CPUByKey(req.CPUKey)
	if !ok {
		s.jsonError(w, http.StatusBadRequest, "unknown cpu_key")
		return
	}
	storageOpt, ok := cluster.StorageByKey(req.StorageKey)
	if !ok {
		s.jsonError(w, http.StatusBadRequest, "unknown storage_key")
		return
	}
	bridgeOpt, ok := cluster.BridgeByKey(req.BridgeKey)
	if !ok {
		s.jsonError(w, http.StatusBadRequest, "unknown bridge_key")
		return
	}
	if req.CPUCores > cpuOpt.Limit || req.MemoryGB > cpuOpt.MemoryLimit || req.DiskGB > storageOpt.Limit {
		s.jsonError(w, http.StatusBadRequest, "requested resources exceed selected cluster option limit")
		return
	}

	var template templateSummary
	var realRaw string
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT id, cluster_key, template_vmid, name, description, os_type, real_status_json, last_seen_at, created_at, updated_at
		FROM templates
		WHERE cluster_key = $1 AND template_vmid = $2
	`, req.ClusterKey, req.TemplateVMID).Scan(
		&template.ID,
		&template.ClusterKey,
		&template.TemplateVMID,
		&template.Name,
		&template.Description,
		&template.OSType,
		&realRaw,
		&template.LastSeenAt,
		&template.CreatedAt,
		&template.UpdatedAt,
	); err != nil {
		s.jsonError(w, http.StatusBadRequest, "template not found")
		return
	}
	template.RealStatus = rawJSONFromString(realRaw)

	sg, err := s.loadSecurityGroupRow(r.Context(), current.Username, req.SecurityGroupName)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "security group not found or not owned by user")
		return
	}
	_ = sg

	quota := s.effectiveQuota(current)
	if quota.Number <= 0 || quota.CPU <= 0 || quota.Memory <= 0 || quota.Storage <= 0 || quota.SecurityGroup <= 0 {
		s.jsonError(w, http.StatusForbidden, "user quota is not configured")
		return
	}
	count, usedCPU, usedMemory, usedStorage, err := s.listUserVMUsage(r.Context(), current.Username)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count+1 > quota.Number || usedCPU+req.CPUCores > quota.CPU || usedMemory+req.MemoryGB > quota.Memory || usedStorage+req.DiskGB > quota.Storage {
		s.jsonError(w, http.StatusBadRequest, "quota exceeded")
		return
	}

	chooseUESTC := true
	if req.UESTCRestricted != nil {
		chooseUESTC = *req.UESTCRestricted
	}
	if quota.UESTC == "force" {
		chooseUESTC = true
	}

	conn, err := s.db.Conn(r.Context())
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer conn.Close()

	if _, err := conn.ExecContext(r.Context(), "BEGIN"); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	vmid, err := nextClusterVMID(r.Context(), conn, req.ClusterKey, cluster.StartVMID, cluster.Limit)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	ip, err := s.deriveIPv4(cluster, req.BridgeKey, vmid)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetNode, err := s.choosePlacementNode(r.Context(), conn, req.ClusterKey, cluster, req.CPUKey, req.CPUCores, req.MemoryGB, template.RealStatus)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	configJSON, _ := json.Marshal(map[string]any{
		"cluster_key":         req.ClusterKey,
		"cpu_key":             req.CPUKey,
		"cpu_cores":           req.CPUCores,
		"memory_gb":           req.MemoryGB,
		"storage_key":         req.StorageKey,
		"disk_gb":             req.DiskGB,
		"bridge_key":          req.BridgeKey,
		"bridge_ipfilter":     bridgeOpt.IPFilter,
		"template_vmid":       req.TemplateVMID,
		"template_name":       template.Name,
		"target_node":         targetNode,
		"gateway":             bridgeOpt.IPv4.Gateway,
		"ip":                  ip,
		"sshkeys":             req.SSHKeys,
		"shared_usernames":    req.SharedUsernames,
		"security_group_name": req.SecurityGroupName,
		"uestc_restricted":    chooseUESTC,
		"boot_order":          bootOrder,
		"power":               req.Power,
		"root_user":           "root",
		"full_clone_storage":  req.StorageKey,
	})
	preferJSON, _ := json.Marshal(map[string]any{
		"intent":              "present",
		"power":               req.Power,
		"generation":          1,
		"vmname":              req.VMName,
		"template_vmid":       req.TemplateVMID,
		"target_node":         targetNode,
		"cpu_key":             req.CPUKey,
		"cpu_cores":           req.CPUCores,
		"memory_gb":           req.MemoryGB,
		"storage_key":         req.StorageKey,
		"disk_gb":             req.DiskGB,
		"bridge_key":          req.BridgeKey,
		"ip":                  ip,
		"gateway":             bridgeOpt.IPv4.Gateway,
		"sshkeys":             req.SSHKeys,
		"shared_usernames":    req.SharedUsernames,
		"security_group_name": req.SecurityGroupName,
		"uestc_restricted":    chooseUESTC,
		"boot_order":          bootOrder,
		"password_synced":     false,
	})
	realJSON, _ := json.Marshal(map[string]any{
		"intent":         "present",
		"power":          "unknown",
		"vmid":           vmid,
		"target_node":    targetNode,
		"ip":             ip,
		"last_synced_at": nil,
	})
	password := randomPassword(20)
	if req.Password != nil {
		password = strings.TrimSpace(*req.Password)
	}
	now := timestamp()
	sshkeysJSON := mustJSON(req.SSHKeys)
	sharedJSON := mustJSON(req.SharedUsernames)
	var uestcRestricted int
	if chooseUESTC {
		uestcRestricted = 1
	}

	var id int64
	err = conn.QueryRowContext(r.Context(), `
		INSERT INTO vms(
			owner_username, cluster_key, vmid, vmname, ip, node, target_node, password,
			sshkeys_json, shared_usernames_json, security_group_name, uestc_restricted,
			config_json, prefer_status_json, real_status_json, sync_state, version,
			created_at, updated_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING id
	`, current.Username, req.ClusterKey, vmid, req.VMName, ip, "", targetNode, password, sshkeysJSON, sharedJSON, req.SecurityGroupName, uestcRestricted, string(configJSON), string(preferJSON), string(realJSON), "pending", 1, now, now).Scan(&id)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := queueVMTaskConn(r.Context(), conn, id, "provision", map[string]any{"vm_id": id}); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := conn.ExecContext(r.Context(), "COMMIT"); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed = true

	vm, err := loadVMRow(r.Context(), conn, id)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, createVMResponse{
		VM: VM{
			ID:                 vm.ID,
			OwnerUsername:      vm.OwnerUsername,
			ClusterKey:         vm.ClusterKey,
			VMID:               vm.VMID,
			VMName:             vm.VMName,
			IP:                 vm.IP,
			Node:               vm.Node,
			TargetNode:         vm.TargetNode,
			SSHKeys:            vm.SSHKeys,
			SharedUsernames:    vm.SharedUsernames,
			SecurityGroupName:  vm.SecurityGroupName,
			UESTCRestricted:    vm.UESTCRestricted,
			QuotaExempt:        vm.QuotaExempt,
			Managed:            vm.Managed,
			TaskQueuePaused:    vm.TaskQueuePaused,
			Config:             vm.Config,
			PreferStatus:       vm.PreferStatus,
			RealStatus:         vm.RealStatus,
			SyncState:          vm.SyncState,
			SyncError:          vm.SyncError,
			Version:            vm.Version,
			CreatedAt:          vm.CreatedAt,
			UpdatedAt:          vm.UpdatedAt,
			DeletedAt:          vm.DeletedAt,
			DeleteRequestedAt:  vm.DeleteRequestedAt,
			DeleteExecuteAfter: vm.DeleteExecuteAfter,
		},
		Password: password,
	})
}

func (s *Server) handlePatchVM(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
	if !s.vmVisibleToCurrentUser(vm, current) {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	if !vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "unmanaged vm cannot be modified")
		return
	}
	if vm.SyncState == "deleting" || vm.DeleteRequestedAt != nil {
		s.jsonError(w, http.StatusBadRequest, "vm is pending deletion")
		return
	}

	var req patchVMRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	sharedOnly := current.Username != vm.OwnerUsername && !current.IsAdmin
	if sharedOnly {
		if req.VMName != nil || req.Password != nil || req.CPUKey != nil || req.CPUCores != nil || req.MemoryGB != nil || req.StorageKey != nil || req.DiskGB != nil || req.BridgeKey != nil || req.BootOrder != nil || req.TemplateVMID != nil || req.SSHKeys != nil || req.SSHKeyIDs != nil || req.SharedUsernames != nil || req.SecurityGroupName != nil || req.UESTCRestricted != nil {
			s.jsonError(w, http.StatusForbidden, "shared users can only change power")
			return
		}
	}
	if req.Power != nil {
		if patchVMHasNonPowerChanges(req) {
			s.jsonError(w, http.StatusBadRequest, "power changes must use the dedicated power actions")
			return
		}
		action := strings.TrimSpace(strings.ToLower(*req.Power))
		if action != "running" && action != "stopped" && action != "reboot" {
			s.jsonError(w, http.StatusBadRequest, "invalid power")
			return
		}
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer func() { _ = tx.Rollback() }()
		if err := queueVMPowerTaskTx(r.Context(), tx, vm, action); err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "power": action})
		return
	}

	targetOwner := vm.OwnerUsername
	if req.OwnerUsername != nil {
		if !current.IsAdmin {
			s.jsonError(w, http.StatusForbidden, "permission denied")
			return
		}
		owner := strings.TrimSpace(*req.OwnerUsername)
		if owner == "" || !validUsernameLike(owner) {
			s.jsonError(w, http.StatusBadRequest, "invalid owner_username")
			return
		}
		if _, err := s.loadUserRow(r.Context(), owner); err != nil {
			s.jsonError(w, http.StatusBadRequest, "owner user not found")
			return
		}
		targetOwner = owner
	}
	targetVMName := vm.VMName
	if req.VMName != nil {
		targetVMName = strings.TrimSpace(*req.VMName)
	}
	if err := vmname.ValidateManaged(targetOwner, targetVMName); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := map[string]any{}
	_ = json.Unmarshal(vm.Config, &cfg)
	oldCfg := map[string]any{}
	_ = json.Unmarshal(vm.Config, &oldCfg)
	prefer := map[string]any{}
	_ = json.Unmarshal(vm.PreferStatus, &prefer)
	oldCPUKey := stringFromMap(oldCfg, "cpu_key")
	oldBridgeKey := stringFromMap(oldCfg, "bridge_key")
	oldTemplateVMID := intFromOrZero(oldCfg, "template_vmid")
	oldDiskGB := intFromOrZero(oldCfg, "disk_gb")
	changed := false
	rebootNeeded := false
	currentPower := vmPowerFromRaw(vm.RealStatus)
	pendingPower := currentPower

	if req.VMName != nil {
		name := strings.TrimSpace(*req.VMName)
		if name == "" {
			s.jsonError(w, http.StatusBadRequest, "vmname is required")
			return
		}
		vm.VMName = name
		prefer["vmname"] = vm.VMName
		changed = true
	}
	if req.OwnerUsername != nil {
		vm.OwnerUsername = targetOwner
		if req.VMName == nil {
			prefer["vmname"] = vm.VMName
		}
		if req.SecurityGroupName == nil {
			if _, err := s.loadSecurityGroupRow(r.Context(), targetOwner, vm.SecurityGroupName); err != nil {
				s.jsonError(w, http.StatusBadRequest, "current security group not found for new owner")
				return
			}
		}
		cfg["owner_username"] = vm.OwnerUsername
		prefer["owner_username"] = vm.OwnerUsername
		changed = true
	}
	if req.Password != nil {
		password := strings.TrimSpace(*req.Password)
		if password == "" {
			s.jsonError(w, http.StatusBadRequest, "password is required")
			return
		}
		vm.Password = password
		cfg["password_synced"] = false
		rebootNeeded = true
		changed = true
	}
	if req.CPUKey != nil && strings.TrimSpace(*req.CPUKey) != oldCPUKey {
		s.jsonError(w, http.StatusBadRequest, "cpu_key is immutable after creation")
		return
	}
	if req.BridgeKey != nil && strings.TrimSpace(*req.BridgeKey) != oldBridgeKey {
		s.jsonError(w, http.StatusBadRequest, "bridge_key is immutable after creation")
		return
	}
	if req.TemplateVMID != nil && *req.TemplateVMID != oldTemplateVMID {
		s.jsonError(w, http.StatusBadRequest, "template_vmid is immutable after creation")
		return
	}
	if (req.SSHKeys != nil || req.SSHKeyIDs != nil) && !sharedOnly {
		if req.SSHKeyIDs != nil {
			if len(*req.SSHKeyIDs) == 0 {
				vm.SSHKeys = []string{}
			} else {
				keys, err := s.loadSSHKeysByIDs(r.Context(), targetOwner, *req.SSHKeyIDs)
				if err != nil {
					s.jsonError(w, http.StatusBadRequest, err.Error())
					return
				}
				vm.SSHKeys = keys
			}
		} else if req.SSHKeys != nil {
			normalized, err := normalizeSSHKeyList(*req.SSHKeys)
			if err != nil {
				s.jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid ssh key: %v", err))
				return
			}
			vm.SSHKeys = normalized
		}
		cfg["sshkeys"] = vm.SSHKeys
		prefer["sshkeys"] = vm.SSHKeys
		changed = true
	}
	if req.SharedUsernames != nil && !sharedOnly {
		vm.SharedUsernames = normalizeStringList(*req.SharedUsernames)
		for _, shared := range vm.SharedUsernames {
			if !validUsernameLike(shared) {
				s.jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid shared username %q", shared))
				return
			}
		}
		if containsString(vm.SharedUsernames, vm.OwnerUsername) {
			s.jsonError(w, http.StatusBadRequest, "owner cannot be in shared_usernames")
			return
		}
		cfg["shared_usernames"] = vm.SharedUsernames
		prefer["shared_usernames"] = vm.SharedUsernames
		changed = true
	}
	if req.SecurityGroupName != nil && !sharedOnly {
		if !validSecurityGroupName(*req.SecurityGroupName) {
			s.jsonError(w, http.StatusBadRequest, "invalid security_group_name")
			return
		}
		if _, err := s.loadSecurityGroupRow(r.Context(), targetOwner, *req.SecurityGroupName); err != nil {
			s.jsonError(w, http.StatusBadRequest, "security group not found or not owned by owner")
			return
		}
		vm.SecurityGroupName = *req.SecurityGroupName
		cfg["security_group_name"] = vm.SecurityGroupName
		prefer["security_group_name"] = vm.SecurityGroupName
		changed = true
	}
	if req.UESTCRestricted != nil && !sharedOnly {
		vm.UESTCRestricted = *req.UESTCRestricted
		cfg["uestc_restricted"] = vm.UESTCRestricted
		prefer["uestc_restricted"] = vm.UESTCRestricted
		changed = true
	}
	if req.Power != nil {
		if *req.Power != "running" && *req.Power != "stopped" && *req.Power != "reboot" {
			s.jsonError(w, http.StatusBadRequest, "invalid power")
			return
		}
		prefer["power"] = *req.Power
		pendingPower = *req.Power
		changed = true
	}
	if req.CPUKey != nil || req.StorageKey != nil || req.BridgeKey != nil || req.TemplateVMID != nil || req.CPUCores != nil || req.MemoryGB != nil || req.DiskGB != nil {
		if sharedOnly {
			s.jsonError(w, http.StatusForbidden, "shared users can only change power")
			return
		}
		cluster, err := s.getClusterConfig(vm.ClusterKey)
		if err != nil {
			s.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.CPUKey != nil {
			if _, ok := cluster.CPUByKey(*req.CPUKey); !ok {
				s.jsonError(w, http.StatusBadRequest, "unknown cpu_key")
				return
			}
			cfg["cpu_key"] = *req.CPUKey
			prefer["cpu_key"] = *req.CPUKey
			rebootNeeded = true
		}
		if req.StorageKey != nil {
			if _, ok := cluster.StorageByKey(*req.StorageKey); !ok {
				s.jsonError(w, http.StatusBadRequest, "unknown storage_key")
				return
			}
			cfg["storage_key"] = *req.StorageKey
			prefer["storage_key"] = *req.StorageKey
		}
		if req.BridgeKey != nil {
			bridge, ok := cluster.BridgeByKey(*req.BridgeKey)
			if !ok {
				s.jsonError(w, http.StatusBadRequest, "unknown bridge_key")
				return
			}
			cfg["bridge_key"] = *req.BridgeKey
			cfg["gateway"] = bridge.IPv4.Gateway
			prefer["bridge_key"] = *req.BridgeKey
			prefer["gateway"] = bridge.IPv4.Gateway
			rebootNeeded = true
		}
		if req.BootOrder != nil {
			bootOrder, err := normalizeBootOrder(*req.BootOrder)
			if err != nil {
				s.jsonError(w, http.StatusBadRequest, err.Error())
				return
			}
			cfg["boot_order"] = bootOrder
			prefer["boot_order"] = bootOrder
			rebootNeeded = true
		}
		if req.TemplateVMID != nil {
			if _, err := s.loadTemplateRow(r.Context(), vm.ClusterKey, *req.TemplateVMID); err != nil {
				s.jsonError(w, http.StatusBadRequest, "template not found")
				return
			}
			cfg["template_vmid"] = *req.TemplateVMID
			prefer["template_vmid"] = *req.TemplateVMID
			rebootNeeded = true
		}
		if req.CPUCores != nil {
			cfg["cpu_cores"] = *req.CPUCores
			prefer["cpu_cores"] = *req.CPUCores
			rebootNeeded = true
		}
		if req.MemoryGB != nil {
			cfg["memory_gb"] = *req.MemoryGB
			prefer["memory_gb"] = *req.MemoryGB
			rebootNeeded = true
		}
		if req.DiskGB != nil {
			if *req.DiskGB < oldDiskGB {
				s.jsonError(w, http.StatusBadRequest, "disk_gb can only increase")
				return
			}
			cfg["disk_gb"] = *req.DiskGB
			prefer["disk_gb"] = *req.DiskGB
			rebootNeeded = true
		}
		changed = true
	}

	if !changed {
		s.jsonError(w, http.StatusBadRequest, "no changes requested")
		return
	}

	if req.UESTCRestricted != nil && !current.IsAdmin && current.Username == vm.OwnerUsername {
		quota := s.effectiveQuota(current)
		if quota.UESTC == "force" && !vm.UESTCRestricted {
			s.jsonError(w, http.StatusBadRequest, "uestc is forced for this user")
			return
		}
	}

	if !vm.QuotaExempt && (req.CPUCores != nil || req.MemoryGB != nil || req.DiskGB != nil || req.StorageKey != nil || req.OwnerUsername != nil) {
		finalCPUKey, _ := cfg["cpu_key"].(string)
		finalCPUCores := intFromOrZero(cfg, "cpu_cores")
		finalMemoryGB := intFromOrZero(cfg, "memory_gb")
		finalStorageKey, _ := cfg["storage_key"].(string)
		finalDiskGB := intFromOrZero(cfg, "disk_gb")
		clusterCfg, err := s.getClusterConfig(vm.ClusterKey)
		if err != nil {
			s.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if cpuOpt, ok := clusterCfg.CPUByKey(finalCPUKey); ok && finalCPUCores > cpuOpt.Limit {
			s.jsonError(w, http.StatusBadRequest, "cpu_cores exceed cluster option limit")
			return
		}
		if cpuOpt, ok := clusterCfg.CPUByKey(finalCPUKey); ok && finalMemoryGB > cpuOpt.MemoryLimit {
			s.jsonError(w, http.StatusBadRequest, "memory_gb exceed cluster option limit")
			return
		}
		if storageOpt, ok := clusterCfg.StorageByKey(finalStorageKey); ok && finalDiskGB > storageOpt.Limit {
			s.jsonError(w, http.StatusBadRequest, "disk_gb exceed cluster option limit")
			return
		}
		quotaOwner := targetOwner
		quotaUser := current
		if quotaOwner != current.Username {
			quotaUser, err = s.loadUserRow(r.Context(), quotaOwner)
			if err != nil {
				s.jsonError(w, http.StatusBadRequest, "owner user not found")
				return
			}
		}
		quota := s.effectiveQuota(quotaUser)
		count, usedCPU, usedMemory, usedStorage, err := s.listUserVMUsage(r.Context(), quotaOwner)
		if err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !vm.QuotaExempt && vm.OwnerUsername == quotaOwner {
			count--
			usedCPU -= intFromOrZero(oldCfg, "cpu_cores")
			usedMemory -= intFromOrZero(oldCfg, "memory_gb")
			usedStorage -= intFromOrZero(oldCfg, "disk_gb")
		}
		count++
		usedCPU += finalCPUCores
		usedMemory += finalMemoryGB
		usedStorage += finalDiskGB
		if count > quota.Number || usedCPU > quota.CPU || usedMemory > quota.Memory || usedStorage > quota.Storage {
			s.jsonError(w, http.StatusBadRequest, "quota exceeded")
			return
		}
	}

	now := timestamp()
	prefer["generation"] = intFromOrZero(prefer, "generation") + 1
	cfgBytes, _ := json.Marshal(cfg)
	preferBytes, _ := json.Marshal(prefer)
	sharedJSON := mustJSON(vm.SharedUsernames)
	sshKeysJSON := mustJSON(vm.SSHKeys)

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(r.Context(), `
		UPDATE vms
		SET owner_username = $1, vmname = $2, ip = $3, node = COALESCE(node, $4), password = $5, sshkeys_json = $6, shared_usernames_json = $7, security_group_name = $8,
		    uestc_restricted = $9, quota_exempt = $10, config_json = $11, prefer_status_json = $12, sync_state = 'pending',
		    sync_error = NULL, version = version + 1, updated_at = $13
		WHERE id = $14
	`, vm.OwnerUsername, vm.VMName, vm.IP, vm.Node, vm.Password, sshKeysJSON, sharedJSON, vm.SecurityGroupName, boolToInt(vm.UESTCRestricted), boolToInt(vm.QuotaExempt), string(cfgBytes), string(preferBytes), now, vm.ID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		s.jsonError(w, http.StatusNotFound, "vm not found")
		return
	}
	if err := queueVMTaskTx(r.Context(), tx, vm.ID, "apply", map[string]any{"vm_id": vm.ID}); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rebootNeeded && currentPower == "running" && pendingPower != "stopped" && pendingPower != "reboot" {
		if err := queueVMTaskTx(r.Context(), tx, vm.ID, "reboot", map[string]any{"vm_id": vm.ID, "reason": "config_change"}); err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	vm, err = s.loadVMRow(r.Context(), vm.ID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if current.Username != vm.OwnerUsername {
		vm.Password = ""
	}
	writeJSON(w, http.StatusOK, vm)
}

func (s *Server) handlePowerVM(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
	if !s.vmVisibleToCurrentUser(vm, current) {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	if !vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "unmanaged vm cannot be modified")
		return
	}
	if vm.SyncState == "deleting" || vm.DeleteRequestedAt != nil {
		s.jsonError(w, http.StatusBadRequest, "vm is pending deletion")
		return
	}

	var req powerVMRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	action := strings.TrimSpace(strings.ToLower(req.Power))
	if action != "running" && action != "stopped" && action != "reboot" {
		s.jsonError(w, http.StatusBadRequest, "invalid power")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()

	if err := queueVMPowerTaskTx(r.Context(), tx, vm, action); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"power": action,
	})
}

func (s *Server) handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
	if !current.IsAdmin && current.Username != vm.OwnerUsername {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	if !vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "unmanaged vm cannot be deleted")
		return
	}
	if vm.SyncState == "deleting" || vm.DeleteRequestedAt != nil {
		s.jsonError(w, http.StatusBadRequest, "vm is already pending deletion")
		return
	}
	now := time.Now().UTC()
	execAt := now.Add(24 * time.Hour)
	prefer := map[string]any{}
	_ = json.Unmarshal(vm.PreferStatus, &prefer)
	prevPower, _ := prefer["power"].(string)
	if prevPower == "" {
		prevPower = "running"
	}
	prefer["intent"] = "delete_pending"
	prefer["power"] = "stopped"
	prefer["power_before_delete"] = prevPower
	prefer["delete_execute_after"] = execAt.Format(time.RFC3339Nano)
	prefer["generation"] = intFromOrZero(prefer, "generation") + 1
	preferBytes, _ := json.Marshal(prefer)
	nowStr := now.Format(time.RFC3339Nano)
	execStr := execAt.Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE vms
		SET prefer_status_json = $1, sync_state = 'deleting', sync_error = NULL,
		    delete_requested_at = $2, delete_execute_after = $3, updated_at = $4, version = version + 1
		WHERE id = $5
	`, string(preferBytes), nowStr, execStr, nowStr, vm.ID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := cancelVMTasksTx(r.Context(), tx, vm.ID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := queueVMTaskTx(r.Context(), tx, vm.ID, "delete", map[string]any{"vm_id": vm.ID, "execute_after": execStr}); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "delete_execute_after": execStr})
}

func (s *Server) handleRestoreVM(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
	if !current.IsAdmin && current.Username != vm.OwnerUsername {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	if !vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "unmanaged vm cannot be restored")
		return
	}
	if vm.SyncState != "deleting" && vm.DeleteRequestedAt == nil {
		s.jsonError(w, http.StatusBadRequest, "vm is not pending deletion")
		return
	}

	prefer := map[string]any{}
	_ = json.Unmarshal(vm.PreferStatus, &prefer)
	restorePower, _ := prefer["power_before_delete"].(string)
	if restorePower == "" {
		restorePower = "running"
	}
	prefer["intent"] = "present"
	prefer["power"] = restorePower
	delete(prefer, "delete_execute_after")
	delete(prefer, "power_before_delete")
	prefer["generation"] = intFromOrZero(prefer, "generation") + 1
	preferBytes, _ := json.Marshal(prefer)
	nowStr := timestamp()

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE vms
		SET prefer_status_json = $1, sync_state = 'pending', sync_error = NULL,
		    delete_requested_at = NULL, delete_execute_after = NULL, updated_at = $2, version = version + 1
		WHERE id = $3
	`, string(preferBytes), nowStr, vm.ID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE vm_tasks
		SET status = 'canceled', updated_at = $1
		WHERE vm_id = $2 AND kind = 'delete' AND status IN ('pending', 'running')
	`, nowStr, vm.ID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := queueVMTaskTx(r.Context(), tx, vm.ID, "apply", map[string]any{"vm_id": vm.ID, "restore": true}); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteNowVM(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
	if !current.IsAdmin && current.Username != vm.OwnerUsername {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	if !vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "unmanaged vm cannot be deleted")
		return
	}
	if vm.SyncState != "deleting" && vm.DeleteRequestedAt == nil {
		s.jsonError(w, http.StatusBadRequest, "vm is not pending deletion")
		return
	}

	prefer := map[string]any{}
	_ = json.Unmarshal(vm.PreferStatus, &prefer)
	prevPower, _ := prefer["power_before_delete"].(string)
	if prevPower == "" {
		prevPower = "running"
	}
	now := time.Now().UTC()
	prefer["intent"] = "delete_pending"
	prefer["power"] = "stopped"
	prefer["power_before_delete"] = prevPower
	prefer["delete_execute_after"] = now.Format(time.RFC3339Nano)
	prefer["generation"] = intFromOrZero(prefer, "generation") + 1
	preferBytes, _ := json.Marshal(prefer)
	nowStr := now.Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE vms
		SET prefer_status_json = $1, sync_state = 'deleting', sync_error = NULL,
		    delete_requested_at = COALESCE(delete_requested_at, $2),
		    delete_execute_after = $3, updated_at = $4, version = version + 1
		WHERE id = $5
	`, string(preferBytes), nowStr, nowStr, nowStr, vm.ID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := cancelVMTasksTx(r.Context(), tx, vm.ID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := queueVMTaskTx(r.Context(), tx, vm.ID, "delete", map[string]any{"vm_id": vm.ID, "execute_after": nowStr, "immediate": true}); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "delete_execute_after": nowStr})
}

func (s *Server) handlePauseVMTasks(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
	if !current.IsAdmin && current.Username != vm.OwnerUsername {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	if !vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "unmanaged vm cannot pause tasks")
		return
	}
	now := timestamp()
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE vms
		SET task_queue_paused = 1,
		    updated_at = $1,
		    version = version + 1
		WHERE id = $2
	`, now, id); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "paused": true})
}

func (s *Server) handleResumeVMTasks(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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
	if !current.IsAdmin && current.Username != vm.OwnerUsername {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	if !vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "unmanaged vm cannot resume tasks")
		return
	}
	now := timestamp()
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE vms
		SET task_queue_paused = 0,
		    updated_at = $1,
		    version = version + 1
		WHERE id = $2
	`, now, id); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "paused": false})
}

func (s *Server) handleRetryVMTask(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentUser(r)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	vmID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid vm id")
		return
	}
	taskID, err := strconv.ParseInt(chi.URLParam(r, "task_id"), 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	vm, err := s.loadVMRow(r.Context(), vmID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.jsonError(w, http.StatusNotFound, "vm not found")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !current.IsAdmin && current.Username != vm.OwnerUsername {
		s.jsonError(w, http.StatusForbidden, "permission denied")
		return
	}
	if !vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "unmanaged vm cannot retry tasks")
		return
	}
	task, err := s.loadVMTask(r.Context(), vmID, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.jsonError(w, http.StatusNotFound, "task not found")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if task.Status != "failed" {
		s.jsonError(w, http.StatusBadRequest, "only failed tasks can be retried")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()
	now := timestamp()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE vm_tasks
		SET status = 'pending',
		    error = NULL,
		    started_at = NULL,
		    finished_at = NULL,
		    updated_at = $1
		WHERE id = $2 AND vm_id = $3 AND status = 'failed'
	`, now, taskID, vmID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE vms
		SET sync_state = CASE WHEN delete_requested_at IS NOT NULL THEN 'deleting' ELSE 'pending' END,
		    sync_error = NULL,
		    updated_at = $1,
		    version = version + 1
		WHERE id = $2
	`, now, vmID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "requeued": true})
}

func (s *Server) handleAdminAdoptVM(w http.ResponseWriter, r *http.Request) {
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
	if vm.Managed {
		s.jsonError(w, http.StatusBadRequest, "vm is already managed")
		return
	}
	if vm.SyncState == "deleting" || vm.DeleteRequestedAt != nil {
		s.jsonError(w, http.StatusBadRequest, "vm is pending deletion")
		return
	}

	var req adoptVMRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.OwnerUsername = strings.TrimSpace(req.OwnerUsername)
	req.VMName = strings.TrimSpace(req.VMName)
	if req.OwnerUsername == "" || req.VMName == "" || req.IP == "" || req.CPUKey == "" || req.StorageKey == "" || req.BridgeKey == "" || req.SecurityGroupName == "" {
		s.jsonError(w, http.StatusBadRequest, "missing required fields")
		return
	}
	if !validUsernameLike(req.OwnerUsername) {
		s.jsonError(w, http.StatusBadRequest, "invalid owner_username")
		return
	}
	if err := vmname.ValidateManaged(req.OwnerUsername, req.VMName); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.loadUserRow(r.Context(), req.OwnerUsername); err != nil {
		s.jsonError(w, http.StatusBadRequest, "owner user not found")
		return
	}
	if req.CPUCores <= 0 || req.MemoryGB <= 0 || req.DiskGB <= 0 {
		s.jsonError(w, http.StatusBadRequest, "cpu_cores, memory_gb and disk_gb must be positive")
		return
	}
	if req.DiskGB < 20 {
		s.jsonError(w, http.StatusBadRequest, "disk_gb must be at least 20")
		return
	}
	cluster, err := s.getClusterConfig(vm.ClusterKey)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cpuOpt, ok := cluster.CPUByKey(req.CPUKey)
	if !ok {
		s.jsonError(w, http.StatusBadRequest, "unknown cpu_key")
		return
	}
	storageOpt, ok := cluster.StorageByKey(req.StorageKey)
	if !ok {
		s.jsonError(w, http.StatusBadRequest, "unknown storage_key")
		return
	}
	bridgeOpt, ok := cluster.BridgeByKey(req.BridgeKey)
	if !ok {
		s.jsonError(w, http.StatusBadRequest, "unknown bridge_key")
		return
	}
	if req.CPUCores > cpuOpt.Limit || req.MemoryGB > cpuOpt.MemoryLimit || req.DiskGB > storageOpt.Limit {
		s.jsonError(w, http.StatusBadRequest, "requested resources exceed selected cluster option limit")
		return
	}
	quotaUser, err := s.loadUserRow(r.Context(), req.OwnerUsername)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "owner user not found")
		return
	}
	quota := s.effectiveQuota(quotaUser)
	count, usedCPU, usedMemory, usedStorage, err := s.listUserVMUsage(r.Context(), req.OwnerUsername)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	count++
	usedCPU += req.CPUCores
	usedMemory += req.MemoryGB
	usedStorage += req.DiskGB
	if count > quota.Number || usedCPU > quota.CPU || usedMemory > quota.Memory || usedStorage > quota.Storage {
		s.jsonError(w, http.StatusBadRequest, "quota exceeded")
		return
	}
	if _, _, err := net.ParseCIDR(req.IP); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid cidr")
		return
	}
	if req.Power == "" {
		req.Power = "running"
	}
	if req.Power != "running" && req.Power != "stopped" && req.Power != "reboot" {
		s.jsonError(w, http.StatusBadRequest, "invalid power")
		return
	}
	bootOrder, err := normalizeBootOrder(req.BootOrder)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	password := randomPassword(20)
	if req.Password != nil {
		if trimmed := strings.TrimSpace(*req.Password); trimmed != "" {
			password = trimmed
		}
	}
	var sshKeys []string
	if len(req.SSHKeyIDs) > 0 {
		sshKeys, err = s.loadSSHKeysByIDs(r.Context(), req.OwnerUsername, req.SSHKeyIDs)
		if err != nil {
			s.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		sshKeys, err = normalizeSSHKeyList(req.SSHKeys)
		if err != nil {
			s.jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid ssh key: %v", err))
			return
		}
	}
	sharedUsernames := normalizeStringList(req.SharedUsernames)
	for _, shared := range sharedUsernames {
		if !validUsernameLike(shared) {
			s.jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid shared username %q", shared))
			return
		}
	}
	if containsString(sharedUsernames, req.OwnerUsername) {
		s.jsonError(w, http.StatusBadRequest, "owner cannot be in shared_usernames")
		return
	}
	sgOwner := req.SecurityGroupOwner
	if sgOwner == "" {
		sgOwner = req.OwnerUsername
	}
	if !validUsernameLike(sgOwner) {
		s.jsonError(w, http.StatusBadRequest, "invalid security_group_owner")
		return
	}
	if _, err := s.loadSecurityGroupRow(r.Context(), sgOwner, req.SecurityGroupName); err != nil {
		s.jsonError(w, http.StatusBadRequest, "security group not found or not owned by owner")
		return
	}
	var uestcRestricted bool
	if req.UESTCRestricted != nil {
		uestcRestricted = *req.UESTCRestricted
	}
	if cluster.Network.UESTC != "" && req.UESTCRestricted == nil {
		uestcRestricted = true
	}

	cfgJSON, _ := json.Marshal(map[string]any{
		"owner_username":       req.OwnerUsername,
		"vmname":               req.VMName,
		"cpu_key":              req.CPUKey,
		"cpu_cores":            req.CPUCores,
		"memory_gb":            req.MemoryGB,
		"storage_key":          req.StorageKey,
		"disk_gb":              req.DiskGB,
		"bridge_key":           req.BridgeKey,
		"bridge_ipfilter":      bridgeOpt.IPFilter,
		"gateway":              bridgeOpt.IPv4.Gateway,
		"ip":                   req.IP,
		"sshkeys":              sshKeys,
		"shared_usernames":     sharedUsernames,
		"security_group_owner": sgOwner,
		"security_group_name":  req.SecurityGroupName,
		"uestc_restricted":     uestcRestricted,
		"quota_exempt":         false,
		"boot_order":           bootOrder,
		"password_synced":      false,
		"power":                req.Power,
		"root_user":            "root",
	})
	preferJSON, _ := json.Marshal(map[string]any{
		"intent":               "present",
		"power":                req.Power,
		"generation":           1,
		"vmname":               req.VMName,
		"cpu_key":              req.CPUKey,
		"cpu_cores":            req.CPUCores,
		"memory_gb":            req.MemoryGB,
		"storage_key":          req.StorageKey,
		"disk_gb":              req.DiskGB,
		"bridge_key":           req.BridgeKey,
		"ip":                   req.IP,
		"gateway":              bridgeOpt.IPv4.Gateway,
		"sshkeys":              sshKeys,
		"shared_usernames":     sharedUsernames,
		"security_group_owner": sgOwner,
		"security_group_name":  req.SecurityGroupName,
		"uestc_restricted":     uestcRestricted,
		"quota_exempt":         false,
		"boot_order":           bootOrder,
		"password_synced":      false,
	})
	realJSON := vm.RealStatus
	if len(realJSON) == 0 {
		realJSON = json.RawMessage(`{"intent":"unmanaged"}`)
	}
	currentPower := vmPowerFromRaw(realJSON)
	if currentPower == "" {
		currentPower = "unknown"
	}
	rebootNeeded := currentPower == "running" && req.Power == "running"
	now := timestamp()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE vms
		SET owner_username = $1, vmname = $2, ip = $3, password = $4, sshkeys_json = $5, shared_usernames_json = $6,
		    security_group_name = $7, uestc_restricted = $8, quota_exempt = $9, config_json = $10, prefer_status_json = $11, real_status_json = $12,
		    managed = 1, sync_state = 'pending', sync_error = NULL, delete_requested_at = NULL,
		    delete_execute_after = NULL, updated_at = $13, version = version + 1
		WHERE id = $14
	`, req.OwnerUsername, req.VMName, req.IP, password, mustJSON(sshKeys), mustJSON(sharedUsernames),
		req.SecurityGroupName, boolToInt(uestcRestricted), 0, string(cfgJSON), string(preferJSON), string(realJSON), now, vm.ID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := queueVMTaskTx(r.Context(), tx, vm.ID, "apply", map[string]any{"vm_id": vm.ID, "adopt": true}); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rebootNeeded && currentPower != "stopped" {
		if err := queueVMTaskTx(r.Context(), tx, vm.ID, "reboot", map[string]any{"vm_id": vm.ID, "reason": "adopt"}); err != nil {
			s.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	vm, err = s.loadVMRow(r.Context(), vm.ID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if current.Username != vm.OwnerUsername {
		vm.Password = ""
	}
	writeJSON(w, http.StatusOK, vm)
}

func intFromOrZero(obj map[string]any, key string) int {
	if obj == nil {
		return 0
	}
	v, ok := obj[key]
	if !ok {
		return 0
	}
	n, ok := intFromAny(v)
	if !ok {
		return 0
	}
	return n
}

func stringFromMap(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	v, ok := obj[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func vmPowerFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "unknown"
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return "unknown"
	}
	if power, ok := data["power"].(string); ok && power != "" {
		return power
	}
	if power, ok := data["status"].(string); ok && power != "" {
		return power
	}
	return "unknown"
}

func normalizeBootOrder(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "", "order=scsi0;ide0", "scsi0;ide0":
		return "order=scsi0;ide0", nil
	case "order=ide0;scsi0", "ide0;scsi0":
		return "order=ide0;scsi0", nil
	default:
		return "", fmt.Errorf("boot_order must be order=scsi0;ide0 or order=ide0;scsi0")
	}
}

func stripVMNamePrefix(owner, name string) string {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if owner == "" || name == "" {
		return name
	}
	prefix := owner + "-"
	if strings.HasPrefix(name, prefix) {
		return strings.TrimPrefix(name, prefix)
	}
	return name
}
