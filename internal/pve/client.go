package pve

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NetUnion/pve-manage/internal/config"
	proxmox "github.com/luthermonson/go-proxmox"
)

const templatePoolID = "template-pool"

type Client struct {
	logger     *slog.Logger
	httpClient *http.Client
	tokens     map[string]config.ClusterToken
	clients    map[string]*proxmox.Client
}

type Resource struct {
	ID       string `json:"id"`
	Node     string `json:"node"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Name     string `json:"name"`
	Template any    `json:"template"`
	VMID     int    `json:"vmid"`
}

type PoolMember struct {
	ID       string `json:"id"`
	Node     string `json:"node"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	VMID     int    `json:"vmid"`
	Template any    `json:"template"`
}

type Template struct {
	ClusterKey string
	Node       string
	VMID       int
	Name       string
	OSType     string
	Config     map[string]any
}

type Snapshot struct {
	Name string `json:"name"`
}

type StorageContent struct {
	VolID  string `json:"volid"`
	VMID   int    `json:"vmid"`
	Format string `json:"format"`
}

type NodeStatus struct {
	Status string  `json:"status"`
	CPU    float64 `json:"cpu"`
	MaxCPU int     `json:"maxcpu"`
	Mem    int64   `json:"mem"`
	MaxMem int64   `json:"maxmem"`
}

type VMRRDPoint struct {
	Time      int64   `json:"time"`
	CPU       float64 `json:"cpu"`
	Mem       float64 `json:"mem"`
	MaxMem    float64 `json:"maxmem"`
	DiskRead  float64 `json:"diskread"`
	DiskWrite float64 `json:"diskwrite"`
	NetIn     float64 `json:"netin"`
	NetOut    float64 `json:"netout"`
}

func NewClient(logger *slog.Logger, tokens config.TokenFile) *Client {
	httpClient := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // PVE clusters often use private CA certificates.
		},
	}
	client := &Client{
		logger:     logger,
		httpClient: httpClient,
		tokens:     tokens.Cluster,
		clients:    make(map[string]*proxmox.Client, len(tokens.Cluster)),
	}
	for clusterKey, token := range tokens.Cluster {
		baseURL := proxmoxBaseURL(token.Site)
		tokenID, secret := splitAPIToken(token.Token)
		client.clients[clusterKey] = proxmox.NewClient(baseURL,
			proxmox.WithHTTPClient(httpClient),
			proxmox.WithAPIToken(tokenID, secret),
		)
	}
	return client
}

func (c *Client) PingCluster(ctx context.Context, clusterKey string) error {
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	_, err = client.Version(ctx)
	return err
}

func (c *Client) ListTemplates(ctx context.Context, clusterKey string) ([]Template, error) {
	var resp struct {
		PoolID  string       `json:"poolid"`
		Members []PoolMember `json:"members"`
	}
	if err := c.request(ctx, clusterKey, http.MethodGet, "/pools/"+url.PathEscape(templatePoolID), nil, &resp); err != nil {
		return nil, err
	}

	templates := make([]Template, 0, len(resp.Members))
	for _, member := range resp.Members {
		if member.Type != "qemu" || member.VMID <= 0 || member.Node == "" {
			continue
		}
		cfg, err := c.GetVMConfig(ctx, clusterKey, member.Node, member.VMID)
		if err != nil {
			c.logger.WarnContext(ctx, "skip template with unreadable config", "cluster", clusterKey, "node", member.Node, "vmid", member.VMID, "error", err)
			continue
		}
		if !truthy(cfg["template"]) && !truthy(member.Template) {
			continue
		}
		name := member.Name
		if name == "" {
			name, _ = cfg["name"].(string)
		}
		osType, _ := cfg["ostype"].(string)
		templates = append(templates, Template{
			ClusterKey: clusterKey,
			Node:       member.Node,
			VMID:       member.VMID,
			Name:       name,
			OSType:     osType,
			Config:     cfg,
		})
	}
	return templates, nil
}

func (c *Client) EnsurePool(ctx context.Context, clusterKey, poolID string) error {
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	if _, err := client.Pool(ctx, poolID); err == nil {
		return nil
	} else {
		if !isNotFoundErr(err) {
			return err
		}
	}
	return client.NewPool(ctx, poolID, "")
}

func (c *Client) SetPoolVMs(ctx context.Context, clusterKey, poolID string, vmids ...int) error {
	if len(vmids) == 0 {
		return nil
	}
	parts := make([]string, 0, len(vmids))
	for _, vmid := range vmids {
		if vmid <= 0 {
			continue
		}
		parts = append(parts, strconv.Itoa(vmid))
	}
	if len(parts) == 0 {
		return nil
	}
	if err := c.request(ctx, clusterKey, http.MethodPut, "/pools/"+url.PathEscape(poolID), url.Values{
		"vms":        {strings.Join(parts, ",")},
		"allow-move": {"1"},
	}, nil); err != nil {
		if isAlreadyExistsErr(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Client) ListVMResources(ctx context.Context, clusterKey string) ([]Resource, error) {
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return nil, err
	}
	cluster := (&proxmox.Cluster{}).New(client)
	items, err := cluster.Resources(ctx, "vm")
	if err != nil {
		return nil, err
	}
	resources := make([]Resource, 0, len(items))
	for _, item := range items {
		resources = append(resources, Resource{
			ID:       item.ID,
			Node:     item.Node,
			Type:     item.Type,
			Status:   item.Status,
			Name:     item.Name,
			Template: item.Template,
			VMID:     int(item.VMID),
		})
	}
	return resources, nil
}

func (c *Client) ListVMSnapshots(ctx context.Context, clusterKey, node string, vmid int) ([]Snapshot, error) {
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return nil, err
	}
	vm := proxmox.VirtualMachine{}
	vm.New(client, node, vmid)
	items, err := vm.Snapshots(ctx)
	if err != nil {
		return nil, err
	}
	snaps := make([]Snapshot, 0, len(items))
	for _, item := range items {
		snaps = append(snaps, Snapshot{Name: item.Name})
	}
	return snaps, nil
}

func (c *Client) DeleteVMSnapshot(ctx context.Context, clusterKey, node string, vmid int, snapName string) error {
	if snapName == "" || snapName == "current" {
		return nil
	}
	var upid proxmox.UPID
	if err := c.request(ctx, clusterKey, http.MethodDelete, fmt.Sprintf("/nodes/%s/qemu/%d/snapshot/%s", url.PathEscape(node), vmid, url.PathEscape(snapName)), nil, &upid); err != nil {
		if isNotFoundErr(err) {
			return nil
		}
		return err
	}
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	return waitProxmoxTask(ctx, proxmox.NewTask(upid, client), 30*time.Minute)
}

func (c *Client) ListStorageContent(ctx context.Context, clusterKey, storage string, vmid int, content string) ([]StorageContent, error) {
	values := url.Values{}
	if vmid > 0 {
		values.Set("vmid", strconv.Itoa(vmid))
	}
	if content != "" {
		values.Set("content", content)
	}
	var items []StorageContent
	if err := c.request(ctx, clusterKey, http.MethodGet, "/storage/"+url.PathEscape(storage)+"/content", values, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *Client) DeleteStorageContent(ctx context.Context, clusterKey, storage, volid string) error {
	if volid == "" {
		return nil
	}
	if err := c.request(ctx, clusterKey, http.MethodDelete, "/storage/"+url.PathEscape(storage)+"/content/"+url.PathEscape(volid), nil, nil); err != nil {
		if isNotFoundErr(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Client) FindVMNode(ctx context.Context, clusterKey string, vmid int) (string, bool, error) {
	resources, err := c.ListVMResources(ctx, clusterKey)
	if err != nil {
		return "", false, err
	}
	for _, resource := range resources {
		if resource.Type == "qemu" && resource.VMID == vmid {
			return resource.Node, true, nil
		}
	}
	return "", false, nil
}

func (c *Client) GetVMConfig(ctx context.Context, clusterKey, node string, vmid int) (map[string]any, error) {
	var cfg map[string]any
	if err := c.request(ctx, clusterKey, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%d/config", url.PathEscape(node), vmid), nil, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Client) CloneFull(ctx context.Context, clusterKey string, templateNode string, templateVMID int, targetNode string, newVMID int, name string, storage string, pool string) error {
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	vm := proxmox.VirtualMachine{}
	vm.New(client, templateNode, templateVMID)
	params := &proxmox.VirtualMachineCloneOptions{
		NewID:   newVMID,
		Name:    name,
		Full:    1,
		Storage: storage,
	}
	if pool != "" {
		params.Pool = pool
	}
	if targetNode != "" && targetNode != templateNode {
		params.Target = targetNode
	}
	_, task, err := vm.Clone(ctx, params)
	if err != nil {
		return err
	}
	return waitProxmoxTask(ctx, task, 30*time.Minute)
}

func (c *Client) SetVMConfig(ctx context.Context, clusterKey, node string, vmid int, params url.Values) error {
	var out any
	return c.request(ctx, clusterKey, http.MethodPost, fmt.Sprintf("/nodes/%s/qemu/%d/config", url.PathEscape(node), vmid), params, &out)
}

func (c *Client) ResizeDisk(ctx context.Context, clusterKey, node string, vmid int, disk string, addMiB int) error {
	if addMiB <= 0 {
		return nil
	}
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	vm := proxmox.VirtualMachine{}
	vm.New(client, node, vmid)
	task, err := vm.ResizeDisk(ctx, disk, fmt.Sprintf("+%dM", addMiB))
	if err != nil {
		return err
	}
	return waitProxmoxTask(ctx, task, 30*time.Minute)
}

func (c *Client) MoveDisk(ctx context.Context, clusterKey, node string, vmid int, disk string, targetStorage string) error {
	if strings.TrimSpace(disk) == "" || strings.TrimSpace(targetStorage) == "" {
		return nil
	}
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	vm := proxmox.VirtualMachine{}
	vm.New(client, node, vmid)
	task, err := vm.MoveDisk(ctx, disk, &proxmox.VirtualMachineMoveDiskOptions{
		Storage: targetStorage,
		Delete:  1,
	})
	if err != nil {
		return err
	}
	return waitProxmoxTask(ctx, task, 30*time.Minute)
}

func (c *Client) VMStatus(ctx context.Context, clusterKey, node string, vmid int) (map[string]any, error) {
	var status map[string]any
	if err := c.request(ctx, clusterKey, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%d/status/current", url.PathEscape(node), vmid), nil, &status); err != nil {
		return nil, err
	}
	return status, nil
}

func (c *Client) VMRRDData(ctx context.Context, clusterKey, node string, vmid int, timeframe string, cf string) ([]VMRRDPoint, error) {
	if timeframe == "" {
		timeframe = "hour"
	}
	params := url.Values{"timeframe": {timeframe}}
	if cf != "" {
		params.Set("cf", cf)
	}
	var points []VMRRDPoint
	if err := c.request(ctx, clusterKey, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%d/rrddata", url.PathEscape(node), vmid), params, &points); err != nil {
		return nil, err
	}
	return points, nil
}

func (c *Client) GetNodeStatus(ctx context.Context, clusterKey, node string) (NodeStatus, error) {
	var status NodeStatus
	if err := c.request(ctx, clusterKey, http.MethodGet, fmt.Sprintf("/nodes/%s/status", url.PathEscape(node)), nil, &status); err != nil {
		return NodeStatus{}, err
	}
	return status, nil
}

func (c *Client) StartVM(ctx context.Context, clusterKey, node string, vmid int) error {
	return c.vmTask(ctx, clusterKey, node, vmid, "start")
}

func (c *Client) StopVM(ctx context.Context, clusterKey, node string, vmid int) error {
	return c.vmTask(ctx, clusterKey, node, vmid, "stop")
}

func (c *Client) ShutdownVM(ctx context.Context, clusterKey, node string, vmid int) error {
	return c.vmTask(ctx, clusterKey, node, vmid, "shutdown")
}

func (c *Client) RebootVM(ctx context.Context, clusterKey, node string, vmid int) error {
	return c.vmTask(ctx, clusterKey, node, vmid, "reboot")
}

func (c *Client) ResetVM(ctx context.Context, clusterKey, node string, vmid int) error {
	return c.vmTask(ctx, clusterKey, node, vmid, "reset")
}

func (c *Client) DeleteVM(ctx context.Context, clusterKey, node string, vmid int) error {
	var upid proxmox.UPID
	if err := c.request(ctx, clusterKey, http.MethodDelete, fmt.Sprintf("/nodes/%s/qemu/%d", url.PathEscape(node), vmid), url.Values{"purge": {"1"}}, &upid); err != nil {
		if isNotFoundErr(err) {
			return nil
		}
		return err
	}
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	return waitProxmoxTask(ctx, proxmox.NewTask(upid, client), 30*time.Minute)
}

func (c *Client) EnsureFirewall(ctx context.Context, clusterKey, node string, vmid int, spec FirewallSpec) error {
	if err := c.applyVMFirewallRules(ctx, clusterKey, node, vmid, spec.Rules); err != nil {
		return err
	}
	if err := c.request(ctx, clusterKey, http.MethodPut, fmt.Sprintf("/nodes/%s/qemu/%d/firewall/options", url.PathEscape(node), vmid), url.Values{
		"enable":        {"1"},
		"dhcp":          {"0"},
		"ndp":           {"1"},
		"radv":          {"0"},
		"macfilter":     {"1"},
		"ipfilter":      {"1"},
		"log_level_in":  {"nolog"},
		"log_level_out": {"nolog"},
		"policy_in":     {pvePolicy(spec.PolicyIn)},
		"policy_out":    {pvePolicy(spec.PolicyOut)},
	}, nil); err != nil {
		return err
	}
	if err := c.ensureVMIPSet(ctx, clusterKey, node, vmid, "ipfilter-net0", spec.IPFilter); err != nil {
		return err
	}
	return c.ensureVMGroupRules(ctx, clusterKey, node, vmid, spec.Groups)
}

func (c *Client) vmTask(ctx context.Context, clusterKey, node string, vmid int, action string) error {
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	vm := proxmox.VirtualMachine{}
	vm.New(client, node, vmid)
	var task *proxmox.Task
	switch action {
	case "start":
		task, err = vm.Start(ctx)
	case "stop":
		task, err = vm.Stop(ctx)
	case "shutdown":
		task, err = vm.Shutdown(ctx)
	case "reboot":
		task, err = vm.Reboot(ctx)
	case "reset":
		task, err = vm.Reset(ctx)
	default:
		return fmt.Errorf("unsupported vm task %q", action)
	}
	if err != nil {
		return err
	}
	return waitProxmoxTask(ctx, task, 10*time.Minute)
}

func (c *Client) WaitTask(ctx context.Context, clusterKey, node, upid string, timeout time.Duration) error {
	if upid == "" {
		return nil
	}
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	_ = node
	return waitProxmoxTask(ctx, proxmox.NewTask(proxmox.UPID(upid), client), timeout)
}

func (c *Client) request(ctx context.Context, clusterKey, method, path string, params url.Values, out any) error {
	client, err := c.clusterClient(clusterKey)
	if err != nil {
		return err
	}
	switch method {
	case http.MethodGet:
		if len(params) > 0 {
			return client.GetWithParams(ctx, path, valuesToMap(params), out)
		}
		return client.Get(ctx, path, out)
	case http.MethodPost:
		return client.Post(ctx, path, valuesToMap(params), out)
	case http.MethodPut:
		return client.Put(ctx, path, valuesToMap(params), out)
	case http.MethodDelete:
		if len(params) > 0 {
			path += "?" + params.Encode()
		}
		return client.Delete(ctx, path, out)
	default:
		return fmt.Errorf("unsupported proxmox method %s", method)
	}
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

func (c *Client) clusterClient(clusterKey string) (*proxmox.Client, error) {
	client, ok := c.clients[clusterKey]
	if !ok {
		return nil, fmt.Errorf("unknown cluster %s", clusterKey)
	}
	return client, nil
}

func proxmoxBaseURL(site string) string {
	base := strings.TrimRight(site, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	if !strings.HasSuffix(base, "/api2/json") {
		base += "/api2/json"
	}
	return base
}

func splitAPIToken(token string) (string, string) {
	token = strings.TrimPrefix(token, "PVEAPIToken=")
	parts := strings.SplitN(token, "=", 2)
	if len(parts) != 2 {
		return token, ""
	}
	return parts[0], parts[1]
}

func valuesToMap(values url.Values) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, items := range values {
		out[key] = strings.Join(items, ",")
	}
	return out
}

func waitProxmoxTask(ctx context.Context, task *proxmox.Task, timeout time.Duration) error {
	if task == nil {
		return nil
	}
	err := task.Wait(ctx, 2*time.Second, timeout)
	if err != nil {
		return err
	}
	if task.IsFailed {
		return fmt.Errorf("pve task failed: %s", task.ExitStatus)
	}
	return nil
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	if proxmox.IsNotFound(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist")
}
