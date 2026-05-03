package pve

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NetUnion/pve-manage/internal/config"
)

const templatePoolID = "template-pool"

type Client struct {
	logger     *slog.Logger
	httpClient *http.Client
	tokens     map[string]config.ClusterToken
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
	Mem       int64   `json:"mem"`
	MaxMem    int64   `json:"maxmem"`
	DiskRead  float64 `json:"diskread"`
	DiskWrite float64 `json:"diskwrite"`
	NetIn     float64 `json:"netin"`
	NetOut    float64 `json:"netout"`
}

func NewClient(logger *slog.Logger, tokens config.TokenFile) *Client {
	return &Client{
		logger: logger,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // PVE clusters often use private CA certificates.
			},
		},
		tokens: tokens.Cluster,
	}
}

func (c *Client) PingCluster(ctx context.Context, clusterKey string) error {
	var out any
	return c.request(ctx, clusterKey, http.MethodGet, "/version", nil, &out)
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
	var out any
	if err := c.request(ctx, clusterKey, http.MethodGet, "/pools/"+url.PathEscape(poolID), nil, &out); err == nil {
		return nil
	} else {
		var apiErr APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			return err
		}
	}
	return c.request(ctx, clusterKey, http.MethodPost, "/pools", url.Values{"poolid": {poolID}}, nil)
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
	if err := c.request(ctx, clusterKey, http.MethodPut, "/pools/"+url.PathEscape(poolID), url.Values{"vms": {strings.Join(parts, ",")}}, nil); err != nil {
		if isAlreadyExistsErr(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Client) ListVMResources(ctx context.Context, clusterKey string) ([]Resource, error) {
	var resources []Resource
	if err := c.request(ctx, clusterKey, http.MethodGet, "/cluster/resources", url.Values{"type": {"vm"}}, &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func (c *Client) ListVMSnapshots(ctx context.Context, clusterKey, node string, vmid int) ([]Snapshot, error) {
	var raw json.RawMessage
	if err := c.request(ctx, clusterKey, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%d/snapshot", url.PathEscape(node), vmid), nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '[' {
		var snaps []Snapshot
		if err := json.Unmarshal(raw, &snaps); err != nil {
			return nil, err
		}
		return snaps, nil
	}
	var resp struct {
		Snapshots []Snapshot `json:"snapshots"`
		Data      []Snapshot `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	if len(resp.Snapshots) > 0 {
		return resp.Snapshots, nil
	}
	return resp.Data, nil
}

func (c *Client) DeleteVMSnapshot(ctx context.Context, clusterKey, node string, vmid int, snapName string) error {
	if snapName == "" || snapName == "current" {
		return nil
	}
	if err := c.request(ctx, clusterKey, http.MethodDelete, fmt.Sprintf("/nodes/%s/qemu/%d/snapshot/%s", url.PathEscape(node), vmid, url.PathEscape(snapName)), nil, nil); err != nil {
		var apiErr APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil
		}
		return err
	}
	return nil
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
		var apiErr APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
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
	params := url.Values{
		"newid":   {strconv.Itoa(newVMID)},
		"name":    {name},
		"full":    {"1"},
		"storage": {storage},
	}
	if pool != "" {
		params.Set("pool", pool)
	}
	if targetNode != "" && targetNode != templateNode {
		params.Set("target", targetNode)
	}
	var upid string
	if err := c.request(ctx, clusterKey, http.MethodPost, fmt.Sprintf("/nodes/%s/qemu/%d/clone", url.PathEscape(templateNode), templateVMID), params, &upid); err != nil {
		return err
	}
	return c.WaitTask(ctx, clusterKey, taskNodeFromUPID(upid, templateNode), upid, 30*time.Minute)
}

func (c *Client) SetVMConfig(ctx context.Context, clusterKey, node string, vmid int, params url.Values) error {
	var out any
	return c.request(ctx, clusterKey, http.MethodPost, fmt.Sprintf("/nodes/%s/qemu/%d/config", url.PathEscape(node), vmid), params, &out)
}

func (c *Client) ResizeDisk(ctx context.Context, clusterKey, node string, vmid int, disk string, addMiB int) error {
	if addMiB <= 0 {
		return nil
	}
	var upid string
	params := url.Values{
		"disk": {disk},
		"size": {fmt.Sprintf("+%dM", addMiB)},
	}
	if err := c.request(ctx, clusterKey, http.MethodPut, fmt.Sprintf("/nodes/%s/qemu/%d/resize", url.PathEscape(node), vmid), params, &upid); err != nil {
		return err
	}
	return c.WaitTask(ctx, clusterKey, taskNodeFromUPID(upid, node), upid, 30*time.Minute)
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

func (c *Client) DeleteVM(ctx context.Context, clusterKey, node string, vmid int) error {
	var upid string
	if err := c.request(ctx, clusterKey, http.MethodDelete, fmt.Sprintf("/nodes/%s/qemu/%d", url.PathEscape(node), vmid), url.Values{"purge": {"1"}}, &upid); err != nil {
		var apiErr APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil
		}
		return err
	}
	return c.WaitTask(ctx, clusterKey, taskNodeFromUPID(upid, node), upid, 30*time.Minute)
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
	var upid string
	err := c.request(ctx, clusterKey, http.MethodPost, fmt.Sprintf("/nodes/%s/qemu/%d/status/%s", url.PathEscape(node), vmid, action), nil, &upid)
	if err != nil {
		return err
	}
	return c.WaitTask(ctx, clusterKey, taskNodeFromUPID(upid, node), upid, 10*time.Minute)
}

func (c *Client) WaitTask(ctx context.Context, clusterKey, node, upid string, timeout time.Duration) error {
	if upid == "" {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		var status struct {
			Status     string `json:"status"`
			ExitStatus string `json:"exitstatus"`
		}
		if err := c.request(ctx, clusterKey, http.MethodGet, fmt.Sprintf("/nodes/%s/tasks/%s/status", url.PathEscape(node), url.PathEscape(upid)), nil, &status); err != nil {
			return err
		}
		if status.Status == "stopped" {
			if status.ExitStatus == "" || status.ExitStatus == "OK" {
				return nil
			}
			return fmt.Errorf("pve task failed: %s", status.ExitStatus)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for task %s", upid)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e APIError) Error() string {
	return fmt.Sprintf("pve api status %d: %s", e.StatusCode, e.Message)
}

type envelope struct {
	Data   json.RawMessage `json:"data"`
	Errors any             `json:"errors,omitempty"`
}

func (c *Client) request(ctx context.Context, clusterKey, method, path string, params url.Values, out any) error {
	token, ok := c.tokens[clusterKey]
	if !ok {
		return fmt.Errorf("unknown cluster %s", clusterKey)
	}
	base := token.Site
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	endpoint := strings.TrimRight(base, "/") + "/api2/json" + path
	var body io.Reader
	if method == http.MethodGet || method == http.MethodDelete {
		if len(params) > 0 {
			endpoint += "?" + params.Encode()
		}
	} else if len(params) > 0 {
		body = bytes.NewBufferString(params.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader(token.Token))
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return APIError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(respBody))}
	}
	if out == nil {
		return nil
	}
	var env envelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return err
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func authHeader(token string) string {
	if strings.HasPrefix(token, "PVEAPIToken=") {
		return token
	}
	return "PVEAPIToken=" + token
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

func taskNodeFromUPID(upid, fallback string) string {
	parts := strings.Split(upid, ":")
	if len(parts) > 1 && parts[1] != "" {
		return parts[1]
	}
	return fallback
}
