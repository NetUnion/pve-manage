package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NetUnion/pve-manage/internal/config"
	"github.com/NetUnion/pve-manage/internal/pve"
	"golang.org/x/crypto/ssh"
)

type Worker struct {
	logger           *slog.Logger
	db               *sql.DB
	config           *config.App
	pve              *pve.Client
	lastTemplateScan time.Time
}

type vmRow struct {
	ID                  int64
	OwnerUsername       string
	ClusterKey          string
	VMID                int
	VMName              string
	IP                  string
	Node                string
	Password            string
	SSHKeysJSON         string
	SharedUsernamesJSON string
	SecurityGroupName   string
	UESTCRestricted     bool
	ConfigJSON          string
	PreferStatusJSON    string
	RealStatusJSON      string
	SyncState           string
	Managed             bool
	Version             int
	DeleteExecuteAfter  sql.NullString
}

type securityRule struct {
	Direction string `json:"direction"`
	Action    string `json:"action"`
	Ethertype string `json:"ethertype"`
	Protocol  string `json:"protocol"`
	CIDR      string `json:"cidr"`
	PortStart *int   `json:"port_start"`
	PortEnd   *int   `json:"port_end"`
}

type templateRecord struct {
	ClusterKey   string
	TemplateVMID int
	Name         string
	Description  *string
	OSType       *string
	RealStatus   string
}

type nodeLoad struct {
	cpuRatio float64
	memRatio float64
	samples  int
}

func New(logger *slog.Logger, db *sql.DB, cfg *config.App, pveClient *pve.Client) *Worker {
	return &Worker{
		logger: logger,
		db:     db,
		config: cfg,
		pve:    pveClient,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	w.logger.Info("worker started")

	for {
		if err := w.syncOnce(ctx); err != nil {
			w.logger.Error("worker sync failed", "error", err)
		}

		select {
		case <-ctx.Done():
			w.logger.Info("worker stopped")
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) syncOnce(ctx context.Context) error {
	if time.Since(w.lastTemplateScan) > time.Minute {
		if err := w.scanPVE(ctx); err != nil {
			w.logger.WarnContext(ctx, "pve scan failed", "error", err)
		} else {
			w.lastTemplateScan = time.Now()
		}
	}

	items, err := w.pendingVMs(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := w.syncVM(ctx, item); err != nil {
			w.logger.ErrorContext(ctx, "sync vm failed", "id", item.ID, "cluster", item.ClusterKey, "vmid", item.VMID, "error", err)
			if updateErr := w.markFailed(ctx, item.ID, err.Error()); updateErr != nil {
				w.logger.ErrorContext(ctx, "update failed vm state", "id", item.ID, "error", updateErr)
			}
			continue
		}
	}
	return nil
}

func (w *Worker) scanPVE(ctx context.Context) error {
	var errs []error
	if err := w.syncTemplates(ctx); err != nil {
		errs = append(errs, fmt.Errorf("templates: %w", err))
	}
	if err := w.syncNodeMetrics(ctx); err != nil {
		errs = append(errs, fmt.Errorf("node metrics: %w", err))
	}
	if err := w.syncUnmanagedVMs(ctx); err != nil {
		errs = append(errs, fmt.Errorf("existing vms: %w", err))
	}
	return errors.Join(errs...)
}

func (w *Worker) syncTemplates(ctx context.Context) error {
	now := timestamp()
	for clusterKey := range w.config.Root.Cluster {
		templates, err := w.pve.ListTemplates(ctx, clusterKey)
		if err != nil {
			return fmt.Errorf("cluster %s: %w", clusterKey, err)
		}
		for _, tpl := range templates {
			real, _ := json.Marshal(map[string]any{
				"node":          tpl.Node,
				"template_vmid": tpl.VMID,
				"config":        tpl.Config,
				"last_seen_at":  now,
			})
			name := tpl.Name
			if name == "" {
				name = fmt.Sprintf("template-%d", tpl.VMID)
			}
			var osType *string
			if tpl.OSType != "" {
				osType = &tpl.OSType
			}
			record := templateRecord{
				ClusterKey:   clusterKey,
				TemplateVMID: tpl.VMID,
				Name:         name,
				OSType:       osType,
				RealStatus:   string(real),
			}
			if err := w.upsertTemplate(ctx, record, now); err != nil {
				return err
			}
		}
		w.logger.InfoContext(ctx, "templates scanned", "cluster", clusterKey, "count", len(templates))
	}
	return nil
}

func (w *Worker) syncUnmanagedVMs(ctx context.Context) error {
	now := timestamp()
	for clusterKey := range w.config.Root.Cluster {
		resources, err := w.pve.ListVMResources(ctx, clusterKey)
		if err != nil {
			return fmt.Errorf("cluster %s: %w", clusterKey, err)
		}
		count := 0
		for _, resource := range resources {
			if resource.Type != "qemu" || resource.VMID <= 0 {
				continue
			}
			if err := w.upsertUnmanagedVM(ctx, clusterKey, resource, now); err != nil {
				return err
			}
			count++
		}
		w.logger.InfoContext(ctx, "existing pve vms scanned", "cluster", clusterKey, "count", count)
	}
	return nil
}

func (w *Worker) pendingVMs(ctx context.Context) ([]vmRow, error) {
	rows, err := w.db.QueryContext(ctx, `
		SELECT id, owner_username, cluster_key, vmid, vmname, ip, node, password, sshkeys_json, shared_usernames_json,
		       security_group_name, uestc_restricted, config_json, prefer_status_json, real_status_json,
		       sync_state, managed, version, delete_execute_after
		FROM vms
		WHERE deleted_at IS NULL
		  AND managed = 1
		  AND sync_state IN ('pending', 'failed', 'deleting')
		ORDER BY updated_at, id
		LIMIT 20
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]vmRow, 0)
	for rows.Next() {
		var current vmRow
		if err := rows.Scan(
			&current.ID,
			&current.OwnerUsername,
			&current.ClusterKey,
			&current.VMID,
			&current.VMName,
			&current.IP,
			&current.Node,
			&current.Password,
			&current.SSHKeysJSON,
			&current.SharedUsernamesJSON,
			&current.SecurityGroupName,
			&current.UESTCRestricted,
			&current.ConfigJSON,
			&current.PreferStatusJSON,
			&current.RealStatusJSON,
			&current.SyncState,
			&current.Managed,
			&current.Version,
			&current.DeleteExecuteAfter,
		); err != nil {
			return nil, err
		}
		items = append(items, current)
	}
	return items, rows.Err()
}

func (w *Worker) syncVM(ctx context.Context, vm vmRow) error {
	var prefer map[string]any
	if err := json.Unmarshal([]byte(vm.PreferStatusJSON), &prefer); err != nil {
		return fmt.Errorf("invalid prefer_status_json: %w", err)
	}

	intent, _ := prefer["intent"].(string)
	if intent == "delete_pending" || vm.SyncState == "deleting" {
		return w.syncDelete(ctx, vm, prefer)
	}
	return w.syncPresent(ctx, vm, prefer)
}

func (w *Worker) syncPresent(ctx context.Context, vm vmRow, prefer map[string]any) error {
	cluster, ok := w.config.Root.Cluster[vm.ClusterKey]
	if !ok {
		return fmt.Errorf("unknown cluster %s", vm.ClusterKey)
	}
	node, exists, err := w.pve.FindVMNode(ctx, vm.ClusterKey, vm.VMID)
	if err != nil {
		return err
	}
	if !exists {
		node, err = w.createVM(ctx, vm, prefer, cluster)
		if err != nil {
			return err
		}
	}
	if node != "" && vm.Node != node {
		if err := w.markNode(ctx, vm.ID, node); err != nil {
			return err
		}
	}
	if err := w.configureVM(ctx, vm, prefer, cluster, node); err != nil {
		return err
	}
	if err := w.applyPower(ctx, vm, node, stringFrom(prefer, "power", "running")); err != nil {
		return err
	}
	status, _ := w.pve.VMStatus(ctx, vm.ClusterKey, node, vm.VMID)
	power, _ := status["status"].(string)
	switch stringFrom(prefer, "power", "running") {
	case "reboot":
		power = "running"
	case "running", "stopped":
		if power == "" {
			power = stringFrom(prefer, "power", "unknown")
		}
	}
	if power == "" {
		power = stringFrom(prefer, "power", "unknown")
	}
	return w.markSynced(ctx, vm.ID, map[string]any{
		"intent":         "present",
		"power":          power,
		"vmid":           vm.VMID,
		"node":           node,
		"ip":             vm.IP,
		"last_synced_at": timestamp(),
	})
}

func (w *Worker) createVM(ctx context.Context, vm vmRow, prefer map[string]any, cluster config.Cluster) (string, error) {
	templateVMID := intFrom(prefer, "template_vmid")
	if templateVMID <= 0 {
		return "", fmt.Errorf("template_vmid is required")
	}
	templates, err := w.pve.ListTemplates(ctx, vm.ClusterKey)
	if err != nil {
		return "", err
	}
	var template pve.Template
	found := false
	for _, tpl := range templates {
		if tpl.VMID == templateVMID {
			template = tpl
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("template %d not found in template-pool", templateVMID)
	}
	targetNode, err := w.chooseNode(ctx, vm.ClusterKey, cluster, stringFrom(prefer, "cpu_key", ""), template.Node, intFrom(prefer, "cpu_cores"), intFrom(prefer, "memory_gb"))
	if err != nil {
		return "", err
	}
	storage := stringFrom(prefer, "storage_key", "")
	if storage == "" {
		return "", fmt.Errorf("storage_key is required")
	}
	poolID := userPoolID(vm.OwnerUsername)
	if err := w.pve.EnsurePool(ctx, vm.ClusterKey, poolID); err != nil {
		return "", err
	}
	if err := w.pve.CloneFull(ctx, vm.ClusterKey, template.Node, template.VMID, targetNode, vm.VMID, vm.VMName, storage, poolID); err != nil {
		return "", err
	}
	if node, ok, err := w.pve.FindVMNode(ctx, vm.ClusterKey, vm.VMID); err != nil {
		return "", err
	} else if ok {
		return node, nil
	}
	if targetNode != "" {
		return targetNode, nil
	}
	return template.Node, nil
}

func (w *Worker) configureVM(ctx context.Context, vm vmRow, prefer map[string]any, cluster config.Cluster, node string) error {
	bridgeKey := stringFrom(prefer, "bridge_key", "custom")
	bridge, ok := cluster.BridgeByKey(bridgeKey)
	if !ok {
		return fmt.Errorf("unknown bridge %s", bridgeKey)
	}
	cfg, err := w.pve.GetVMConfig(ctx, vm.ClusterKey, node, vm.VMID)
	if err != nil {
		return err
	}
	if err := w.applyVMConfigIfNeeded(ctx, vm, prefer, bridgeKey, bridge, cfg, node); err != nil {
		return err
	}
	if err := w.resizeDisk(ctx, vm, prefer, cfg, node); err != nil {
		return err
	}
	return w.syncFirewall(ctx, vm, prefer, bridge, node)
}

func (w *Worker) applyVMConfigIfNeeded(ctx context.Context, vm vmRow, prefer map[string]any, bridgeKey string, bridge config.BridgeConfig, cfg map[string]any, node string) error {
	params := url.Values{}
	desiredName := vm.VMName
	if stringFromMap(cfg, "name", "") != desiredName {
		params.Set("name", desiredName)
	}
	if intFromMapAny(cfg, "cores") != intFrom(prefer, "cpu_cores") {
		params.Set("cores", strconv.Itoa(intFrom(prefer, "cpu_cores")))
	}
	if intFromMapAny(cfg, "memory") != intFrom(prefer, "memory_gb")*1024 {
		params.Set("memory", strconv.Itoa(intFrom(prefer, "memory_gb")*1024))
	}
	if stringFromMap(cfg, "ciuser", "") != "root" {
		params.Set("ciuser", "root")
	}
	if stringFromMap(cfg, "cipassword", "") != vm.Password {
		params.Set("cipassword", vm.Password)
	}
	if stringFromMap(cfg, "ipconfig0", "") != fmt.Sprintf("ip=%s,gw=%s,ip6=auto", vm.IP, bridge.IPv4.Gateway) {
		params.Set("ipconfig0", fmt.Sprintf("ip=%s,gw=%s,ip6=auto", vm.IP, bridge.IPv4.Gateway))
	}
	if truthy(cfg["agent"]) {
		params.Set("agent", "0")
	}
	sshKeys, err := normalizeSSHKeyList(parseJSONStrings(vm.SSHKeysJSON))
	if err != nil {
		return fmt.Errorf("invalid ssh key list: %w", err)
	}
	desiredSSH := strings.Join(sshKeys, "\n")
	if stringFromMap(cfg, "sshkeys", "") != desiredSSH {
		if desiredSSH == "" {
			params.Set("delete", "sshkeys")
		} else {
			params.Set("sshkeys", url.PathEscape(desiredSSH))
		}
	}
	if currentNet0, _ := cfg["net0"].(string); currentNet0 != "" {
		desiredNet0 := rewriteNet0(currentNet0, bridgeKey)
		if desiredNet0 != "" && currentNet0 != desiredNet0 {
			params.Set("net0", desiredNet0)
		}
	} else {
		params.Set("net0", fmt.Sprintf("virtio,bridge=%s,firewall=1", bridgeKey))
	}
	if len(params) == 0 {
		return nil
	}
	return w.pve.SetVMConfig(ctx, vm.ClusterKey, node, vm.VMID, params)
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
	pub, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(value))
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		line += " " + comment
	}
	return line, nil
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	case string:
		return t == "1" || strings.EqualFold(t, "true") || strings.EqualFold(t, "yes")
	default:
		return false
	}
}

func intFromMapAny(obj map[string]any, key string) int {
	if obj == nil {
		return 0
	}
	v, ok := obj[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

func (w *Worker) resizeDisk(ctx context.Context, vm vmRow, prefer map[string]any, cfg map[string]any, node string) error {
	targetGB := intFrom(prefer, "disk_gb")
	if targetGB <= 0 {
		return fmt.Errorf("disk_gb is required")
	}
	diskKey, diskRaw := primaryDiskConfig(cfg)
	if diskKey == "" {
		return fmt.Errorf("cannot determine primary disk")
	}
	currentMiB := diskMiB(diskRaw)
	if currentMiB == 0 {
		return fmt.Errorf("cannot determine current %s size", diskKey)
	}
	targetMiB := targetGB * 1024
	if targetMiB < currentMiB {
		return fmt.Errorf("disk resize cannot shrink: current=%dMiB target=%dMiB", currentMiB, targetMiB)
	}
	return w.pve.ResizeDisk(ctx, vm.ClusterKey, node, vm.VMID, diskKey, targetMiB-currentMiB)
}

func (w *Worker) syncFirewall(ctx context.Context, vm vmRow, prefer map[string]any, bridge config.BridgeConfig, node string) error {
	rules, err := w.loadSecurityRules(ctx, vm.OwnerUsername, vm.SecurityGroupName)
	if err != nil {
		return err
	}
	var groups []string
	if boolFrom(prefer, "uestc_restricted") && w.config.Root.Cluster[vm.ClusterKey].Network.UESTC != "" {
		groups = append(groups, w.config.Root.Cluster[vm.ClusterKey].Network.UESTC)
	}
	ipfilter := append([]string{exactIPv4CIDR(vm.IP)}, bridge.IPFilter...)
	spec := pve.FirewallSpec{
		Rules:    convertRules(rules),
		IPFilter: ipfilter,
		Groups:   groups,
	}
	return w.pve.EnsureFirewall(ctx, vm.ClusterKey, node, vm.VMID, spec)
}

func (w *Worker) applyPower(ctx context.Context, vm vmRow, node string, power string) error {
	switch power {
	case "running":
		status, err := w.pve.VMStatus(ctx, vm.ClusterKey, node, vm.VMID)
		if err != nil {
			return err
		}
		if stringFromMap(status, "status", "") == "running" {
			return nil
		}
		return w.pve.StartVM(ctx, vm.ClusterKey, node, vm.VMID)
	case "stopped":
		status, err := w.pve.VMStatus(ctx, vm.ClusterKey, node, vm.VMID)
		if err != nil {
			return err
		}
		if stringFromMap(status, "status", "") == "stopped" {
			return nil
		}
		return w.pve.ShutdownVM(ctx, vm.ClusterKey, node, vm.VMID)
	case "reboot":
		return w.pve.RebootVM(ctx, vm.ClusterKey, node, vm.VMID)
	case "", "unknown":
		return nil
	default:
		return fmt.Errorf("unknown power %q", power)
	}
}

func (w *Worker) syncDelete(ctx context.Context, vm vmRow, prefer map[string]any) error {
	node, exists, err := w.pve.FindVMNode(ctx, vm.ClusterKey, vm.VMID)
	if err != nil {
		return err
	}
	if !exists {
		return w.markDeleted(ctx, vm.ID)
	}
	if err := w.pve.ShutdownVM(ctx, vm.ClusterKey, node, vm.VMID); err != nil {
		w.logger.WarnContext(ctx, "shutdown before delete failed", "id", vm.ID, "error", err)
	}
	execAt := vm.DeleteExecuteAfter.String
	if execAt == "" {
		execAt, _ = prefer["delete_execute_after"].(string)
	}
	deleteAt, err := parseTime(execAt)
	if err != nil {
		return fmt.Errorf("invalid delete_execute_after: %w", err)
	}
	if time.Now().Before(deleteAt) {
		return w.markDeletePending(ctx, vm.ID, map[string]any{
			"intent":               "delete_pending",
			"power":                "stopped",
			"node":                 node,
			"vmid":                 vm.VMID,
			"delete_execute_after": deleteAt.Format(time.RFC3339Nano),
			"last_synced_at":       timestamp(),
		})
	}
	if err := w.cleanupDeletionArtifacts(ctx, vm, node); err != nil {
		return err
	}
	if err := w.pve.DeleteVM(ctx, vm.ClusterKey, node, vm.VMID); err != nil {
		return err
	}
	return w.markDeleted(ctx, vm.ID)
}

func (w *Worker) cleanupDeletionArtifacts(ctx context.Context, vm vmRow, node string) error {
	if err := w.removeSnapshots(ctx, vm, node); err != nil {
		return err
	}
	if err := w.removeBackups(ctx, vm); err != nil {
		return err
	}
	return nil
}

func (w *Worker) removeSnapshots(ctx context.Context, vm vmRow, node string) error {
	snaps, err := w.pve.ListVMSnapshots(ctx, vm.ClusterKey, node, vm.VMID)
	if err != nil {
		return err
	}
	for _, snap := range snaps {
		if snap.Name == "" || snap.Name == "current" {
			continue
		}
		if err := w.pve.DeleteVMSnapshot(ctx, vm.ClusterKey, node, vm.VMID, snap.Name); err != nil {
			return fmt.Errorf("delete snapshot %s: %w", snap.Name, err)
		}
	}
	return nil
}

func (w *Worker) removeBackups(ctx context.Context, vm vmRow) error {
	cluster, ok := w.config.Root.Cluster[vm.ClusterKey]
	if !ok {
		return fmt.Errorf("unknown cluster %s", vm.ClusterKey)
	}
	for storageKey := range cluster.Storage {
		items, err := w.pve.ListStorageContent(ctx, vm.ClusterKey, storageKey, vm.VMID, "backup")
		if err != nil {
			if isIgnorableBackupScanError(err) {
				continue
			}
			return fmt.Errorf("storage %s: %w", storageKey, err)
		}
		for _, item := range items {
			if item.VMID != vm.VMID || item.VolID == "" {
				continue
			}
			if err := w.pve.DeleteStorageContent(ctx, vm.ClusterKey, storageKey, item.VolID); err != nil {
				return fmt.Errorf("delete backup %s/%s: %w", storageKey, item.VolID, err)
			}
		}
	}
	return nil
}

func (w *Worker) syncNodeMetrics(ctx context.Context) error {
	now := timestamp()
	var errs []error
	for clusterKey, cluster := range w.config.Root.Cluster {
		nodes := clusterNodes(cluster)
		if len(nodes) == 0 {
			continue
		}
		for _, node := range nodes {
			status, err := w.pve.GetNodeStatus(ctx, clusterKey, node)
			if err != nil {
				errs = append(errs, fmt.Errorf("cluster %s node %s: %w", clusterKey, node, err))
				continue
			}
			if err := w.recordNodeMetric(ctx, clusterKey, node, status, now); err != nil {
				errs = append(errs, fmt.Errorf("cluster %s node %s: %w", clusterKey, node, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (w *Worker) chooseNode(ctx context.Context, clusterKey string, cluster config.Cluster, cpuKey string, fallback string, requestedCores int, requestedMemoryGB int) (string, error) {
	cpu, ok := cluster.CPUByKey(cpuKey)
	if !ok || len(cpu.Node) == 0 {
		return fallback, nil
	}

	candidates := uniqueStrings(cpu.Node)
	if len(candidates) == 0 {
		return fallback, nil
	}

	type candidate struct {
		name     string
		score    float64
		cpuScore float64
		memScore float64
	}
	best := candidate{score: math.Inf(1)}
	for _, node := range candidates {
		status, err := w.pve.GetNodeStatus(ctx, clusterKey, node)
		if err != nil {
			w.logger.WarnContext(ctx, "node status unavailable", "cluster", clusterKey, "node", node, "error", err)
			continue
		}
		load, err := w.nodeLoad(ctx, clusterKey, node, status)
		if err != nil {
			w.logger.WarnContext(ctx, "node load unavailable", "cluster", clusterKey, "node", node, "error", err)
			continue
		}
		score, cpuScore, memScore := scoreNodeLoad(load, status, requestedCores, requestedMemoryGB)
		if score < best.score-1e-9 ||
			(math.Abs(score-best.score) <= 1e-9 && (cpuScore < best.cpuScore-1e-9 ||
				(math.Abs(cpuScore-best.cpuScore) <= 1e-9 && (memScore < best.memScore-1e-9 ||
					(math.Abs(memScore-best.memScore) <= 1e-9 && node == fallback))))) {
			best = candidate{name: node, score: score, cpuScore: cpuScore, memScore: memScore}
		}
	}
	if best.name != "" {
		return best.name, nil
	}
	for _, node := range candidates {
		if node == fallback {
			return fallback, nil
		}
	}
	return candidates[0], nil
}

func (w *Worker) nodeLoad(ctx context.Context, clusterKey, node string, fallback pve.NodeStatus) (nodeLoad, error) {
	avg, err := w.averageNodeLoad(ctx, clusterKey, node)
	if err == nil && avg.samples > 0 {
		return avg, nil
	}
	return nodeLoad{
		cpuRatio: nodeCPUPercent(fallback) / 100.0,
		memRatio: nodeMemRatio(fallback),
		samples:  1,
	}, nil
}

func (w *Worker) averageNodeLoad(ctx context.Context, clusterKey, node string) (nodeLoad, error) {
	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	var cpuAvg, memAvg sql.NullFloat64
	var samples sql.NullInt64
	if err := w.db.QueryRowContext(ctx, `
		SELECT AVG(cpu_ratio), AVG(mem_ratio), COUNT(1)
		FROM node_metrics
		WHERE cluster_key = ? AND node = ? AND recorded_at >= ?
	`, clusterKey, node, since).Scan(&cpuAvg, &memAvg, &samples); err != nil {
		return nodeLoad{}, err
	}
	if !samples.Valid || samples.Int64 == 0 {
		return nodeLoad{}, sql.ErrNoRows
	}
	load := nodeLoad{samples: int(samples.Int64)}
	if cpuAvg.Valid {
		load.cpuRatio = cpuAvg.Float64
	}
	if memAvg.Valid {
		load.memRatio = memAvg.Float64
	}
	return load, nil
}

func clusterNodes(cluster config.Cluster) []string {
	return uniqueStrings(flattenCPUNodeLists(cluster))
}

func flattenCPUNodeLists(cluster config.Cluster) []string {
	out := make([]string, 0)
	for _, cpu := range cluster.CPU {
		out = append(out, cpu.Node...)
	}
	return out
}

func uniqueStrings(values []string) []string {
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

func nodeCPUPercent(status pve.NodeStatus) float64 {
	if status.MaxCPU <= 0 {
		return 0
	}
	return status.CPU * 100
}

func nodeMemRatio(status pve.NodeStatus) float64 {
	if status.MaxMem <= 0 {
		return 0
	}
	return float64(status.Mem) / float64(status.MaxMem)
}

func (w *Worker) recordNodeMetric(ctx context.Context, clusterKey, node string, status pve.NodeStatus, now string) error {
	cpuRatio := nodeCPUPercent(status) / 100.0
	memRatio := nodeMemRatio(status)
	_, err := w.db.ExecContext(ctx, `
		INSERT INTO node_metrics(cluster_key, node, cpu_ratio, mem_ratio, recorded_at, created_at)
		VALUES(?,?,?,?,?,?)
	`, clusterKey, node, cpuRatio, memRatio, now, now)
	return err
}

func scoreNodeLoad(load nodeLoad, status pve.NodeStatus, requestedCores, requestedMemoryGB int) (score float64, cpuScore float64, memScore float64) {
	if requestedCores <= 0 {
		requestedCores = 1
	}
	if requestedMemoryGB <= 0 {
		requestedMemoryGB = 1
	}
	cpuCap := float64(status.MaxCPU)
	if cpuCap <= 0 {
		cpuCap = 1
	}
	memCap := float64(status.MaxMem)
	if memCap <= 0 {
		memCap = 1
	}
	cpuScore = load.cpuRatio + float64(requestedCores)/cpuCap
	memScore = load.memRatio + float64(requestedMemoryGB)*1024*1024*1024/memCap
	if cpuScore > memScore {
		score = cpuScore
	} else {
		score = memScore
	}
	return score, cpuScore, memScore
}

func (w *Worker) loadSecurityRules(ctx context.Context, owner, name string) ([]securityRule, error) {
	var raw string
	if err := w.db.QueryRowContext(ctx, `
		SELECT rules_json
		FROM security_groups
		WHERE owner_username = ? AND name = ?
	`, owner, name).Scan(&raw); err != nil {
		return nil, err
	}
	var rules []securityRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func (w *Worker) upsertTemplate(ctx context.Context, tpl templateRecord, now string) error {
	var id int64
	err := w.db.QueryRowContext(ctx, `
		SELECT id
		FROM templates
		WHERE cluster_key = ? AND template_vmid = ?
	`, tpl.ClusterKey, tpl.TemplateVMID).Scan(&id)
	if err == sql.ErrNoRows {
		_, err = w.db.ExecContext(ctx, `
			INSERT INTO templates(cluster_key, template_vmid, name, description, os_type, real_status_json, last_seen_at, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?)
		`, tpl.ClusterKey, tpl.TemplateVMID, tpl.Name, tpl.Description, tpl.OSType, tpl.RealStatus, now, now, now)
		return err
	}
	if err != nil {
		return err
	}
	_, err = w.db.ExecContext(ctx, `
		UPDATE templates
		SET name = ?, description = ?, os_type = ?, real_status_json = ?, last_seen_at = ?, updated_at = ?
		WHERE id = ?
	`, tpl.Name, tpl.Description, tpl.OSType, tpl.RealStatus, now, now, id)
	return err
}

func (w *Worker) upsertUnmanagedVM(ctx context.Context, clusterKey string, resource pve.Resource, now string) error {
	var id int64
	var managed bool
	err := w.db.QueryRowContext(ctx, `
		SELECT id, managed
		FROM vms
		WHERE cluster_key = ? AND vmid = ? AND deleted_at IS NULL
	`, clusterKey, resource.VMID).Scan(&id, &managed)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	name := resource.Name
	if name == "" {
		name = fmt.Sprintf("pve-%d", resource.VMID)
	}
	real, _ := json.Marshal(map[string]any{
		"intent":          "unmanaged",
		"power":           resource.Status,
		"node":            resource.Node,
		"vmid":            resource.VMID,
		"name":            name,
		"last_seen_at":    now,
		"source":          "pve-scan",
		"pve_resource_id": resource.ID,
	})

	if err == nil {
		if managed {
			return nil
		}
		_, err = w.db.ExecContext(ctx, `
			UPDATE vms
			SET vmname = ?, node = ?, real_status_json = ?, sync_state = 'unmanaged', sync_error = NULL, updated_at = ?
			WHERE id = ?
		`, name, resource.Node, string(real), now, id)
		return err
	}

	cfg, _ := json.Marshal(map[string]any{
		"source": "pve-scan",
		"node":   resource.Node,
		"name":   name,
	})
	prefer, _ := json.Marshal(map[string]any{
		"intent": "unmanaged",
	})
	_, err = w.db.ExecContext(ctx, `
		INSERT INTO vms(
			owner_username, cluster_key, vmid, vmname, ip, node, password,
			sshkeys_json, shared_usernames_json, security_group_name, uestc_restricted,
			config_json, prefer_status_json, real_status_json, sync_state, version,
			created_at, updated_at, managed
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`, "__pve_unmanaged__", clusterKey, resource.VMID, name, "", resource.Node, "", "[]", "[]", "", 0, string(cfg), string(prefer), string(real), "unmanaged", 1, now, now, 0)
	return err
}

func (w *Worker) markSynced(ctx context.Context, id int64, real map[string]any) error {
	data, _ := json.Marshal(real)
	_, err := w.db.ExecContext(ctx, `
		UPDATE vms
		SET real_status_json = ?, sync_state = 'synced', sync_error = NULL, updated_at = ?
		WHERE id = ?
	`, string(data), timestamp(), id)
	return err
}

func (w *Worker) markNode(ctx context.Context, id int64, node string) error {
	if node == "" {
		return nil
	}
	_, err := w.db.ExecContext(ctx, `
		UPDATE vms
		SET node = ?, updated_at = ?
		WHERE id = ?
	`, node, timestamp(), id)
	return err
}

func (w *Worker) markDeletePending(ctx context.Context, id int64, real map[string]any) error {
	data, _ := json.Marshal(real)
	_, err := w.db.ExecContext(ctx, `
		UPDATE vms
		SET real_status_json = ?, sync_state = 'deleting', sync_error = NULL, updated_at = ?
		WHERE id = ?
	`, string(data), timestamp(), id)
	return err
}

func (w *Worker) markFailed(ctx context.Context, id int64, message string) error {
	_, err := w.db.ExecContext(ctx, `
		UPDATE vms
		SET sync_state = 'failed', sync_error = ?, updated_at = ?
		WHERE id = ?
	`, message, timestamp(), id)
	return err
}

func (w *Worker) markDeleted(ctx context.Context, id int64) error {
	now := timestamp()
	real, _ := json.Marshal(map[string]any{
		"intent":         "absent",
		"power":          "deleted",
		"last_synced_at": now,
	})
	_, err := w.db.ExecContext(ctx, `
		UPDATE vms
		SET deleted_at = ?, real_status_json = ?, sync_state = 'synced', sync_error = NULL, updated_at = ?
		WHERE id = ?
	`, now, string(real), now, id)
	return err
}

func parseJSONStrings(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func convertRules(rules []securityRule) []pve.SecurityRule {
	out := make([]pve.SecurityRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, pve.SecurityRule{
			Direction: rule.Direction,
			Action:    rule.Action,
			Ethertype: rule.Ethertype,
			Protocol:  rule.Protocol,
			CIDR:      rule.CIDR,
			PortStart: rule.PortStart,
			PortEnd:   rule.PortEnd,
		})
	}
	return out
}

func stringFrom(obj map[string]any, key, fallback string) string {
	if v, ok := obj[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func intFrom(obj map[string]any, key string) int {
	v, ok := obj[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

func boolFrom(obj map[string]any, key string) bool {
	v, ok := obj[key]
	if !ok {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case float64:
		return b != 0
	case string:
		return b == "1" || strings.EqualFold(b, "true") || strings.EqualFold(b, "yes")
	default:
		return false
	}
}

func stringFromMap(obj map[string]any, key, fallback string) string {
	if v, ok := obj[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func rewriteNet0(raw any, bridge string) string {
	current, _ := raw.(string)
	if current == "" {
		return ""
	}
	parts := strings.Split(current, ",")
	seenBridge := false
	seenFirewall := false
	for i, part := range parts {
		if strings.HasPrefix(part, "bridge=") {
			parts[i] = "bridge=" + bridge
			seenBridge = true
		}
		if strings.HasPrefix(part, "firewall=") {
			parts[i] = "firewall=1"
			seenFirewall = true
		}
	}
	if !seenBridge {
		parts = append(parts, "bridge="+bridge)
	}
	if !seenFirewall {
		parts = append(parts, "firewall=1")
	}
	return strings.Join(parts, ",")
}

var sizeRE = regexp.MustCompile(`(?i)\bsize=([0-9]+(?:\.[0-9]+)?)([KMGTP]?)\b`)

func primaryDiskConfig(cfg map[string]any) (string, string) {
	for _, key := range []string{"scsi0", "virtio0", "sata0", "ide0"} {
		if raw, ok := cfg[key]; ok {
			value, _ := raw.(string)
			if value == "" {
				continue
			}
			if strings.Contains(value, "media=cdrom") {
				continue
			}
			return key, value
		}
	}
	for _, key := range []string{"scsi0", "virtio0", "sata0", "ide0"} {
		if raw, ok := cfg[key]; ok {
			value, _ := raw.(string)
			if value == "" || strings.Contains(value, "media=cdrom") {
				continue
			}
			return key, value
		}
	}
	return "", ""
}

func diskMiB(raw any) int {
	value, _ := raw.(string)
	match := sizeRE.FindStringSubmatch(value)
	if len(match) != 3 {
		return 0
	}
	n, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(match[2]) {
	case "T":
		n *= 1024 * 1024
	case "G", "":
		n *= 1024
	case "M":
		// already in MiB
	case "K":
		n /= 1024
	default:
		return 0
	}
	return int(math.Round(n))
}

func exactIPv4CIDR(ipWithCIDR string) string {
	ip, _, err := net.ParseCIDR(ipWithCIDR)
	if err != nil {
		ip = net.ParseIP(strings.Split(ipWithCIDR, "/")[0])
	}
	if ip == nil {
		return ipWithCIDR
	}
	return ip.String() + "/32"
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, value)
}

func timestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func isIgnorableBackupScanError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unsupported") ||
		strings.Contains(msg, "not configured") ||
		strings.Contains(msg, "content type") ||
		strings.Contains(msg, "not implemented") ||
		strings.Contains(msg, "status 501")
}

func userPoolID(username string) string {
	return fmt.Sprintf("user_%s_pool", username)
}
