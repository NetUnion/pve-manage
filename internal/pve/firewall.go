package pve

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type FirewallSpec struct {
	UserGroupName string
	PolicyIn      string
	PolicyOut     string
	Rules         []SecurityRule
	IPFilter      []string
	Groups        []string
}

type SecurityRule struct {
	Direction string
	Action    string
	Ethertype string
	Protocol  string
	CIDR      string
	PortStart *int
	PortEnd   *int
}

type firewallRule struct {
	Pos    int    `json:"pos"`
	Type   string `json:"type"`
	Action string `json:"action"`
}

type ipsetEntry struct {
	CIDR string `json:"cidr"`
}

type ipsetEntryState struct {
	Raw       string
	Canonical string
}

func (c *Client) ensureSecurityGroup(ctx context.Context, clusterKey, groupName string, rules []SecurityRule) error {
	if groupName == "" {
		return fmt.Errorf("security group name is required")
	}
	var groups []struct {
		Group string `json:"group"`
	}
	if err := c.request(ctx, clusterKey, http.MethodGet, "/cluster/firewall/groups", nil, &groups); err != nil {
		return err
	}
	exists := false
	for _, group := range groups {
		if group.Group == groupName {
			exists = true
			break
		}
	}
	if !exists {
		if err := c.request(ctx, clusterKey, http.MethodPost, "/cluster/firewall/groups", url.Values{
			"group": {groupName},
		}, nil); err != nil {
			return err
		}
	}
	if err := c.clearFirewallRules(ctx, clusterKey, "/cluster/firewall/groups/"+url.PathEscape(groupName)); err != nil {
		return err
	}
	for _, rule := range rules {
		params := url.Values{
			"type":   {rule.Direction},
			"action": {pveAction(rule.Action)},
			"enable": {"1"},
		}
		if rule.Protocol != "" {
			params.Set("proto", rule.Protocol)
		}
		if rule.CIDR != "" {
			if rule.Direction == "out" {
				params.Set("dest", rule.CIDR)
			} else {
				params.Set("source", rule.CIDR)
			}
		}
		if rule.Protocol == "tcp" || rule.Protocol == "udp" {
			if rule.PortStart != nil && rule.PortEnd != nil {
				if *rule.PortStart == *rule.PortEnd {
					params.Set("dport", strconv.Itoa(*rule.PortStart))
				} else {
					params.Set("dport", fmt.Sprintf("%d:%d", *rule.PortStart, *rule.PortEnd))
				}
			} else if rule.PortStart != nil {
				params.Set("dport", strconv.Itoa(*rule.PortStart))
			}
		}
		if err := c.request(ctx, clusterKey, http.MethodPost, "/cluster/firewall/groups/"+url.PathEscape(groupName), params, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ensureVMIPSet(ctx context.Context, clusterKey, node string, vmid int, name string, entries []string) error {
	base := fmt.Sprintf("/nodes/%s/qemu/%d/firewall/ipset", url.PathEscape(node), vmid)
	var sets []struct {
		Name string `json:"name"`
	}
	if err := c.request(ctx, clusterKey, http.MethodGet, base, nil, &sets); err != nil {
		return err
	}
	exists := false
	for _, set := range sets {
		if set.Name == name {
			exists = true
			break
		}
	}
	if !exists {
		if err := c.request(ctx, clusterKey, http.MethodPost, base, url.Values{"name": {name}}, nil); err != nil {
			return err
		}
	}
	setPath := base + "/" + url.PathEscape(name)
	var current []ipsetEntry
	if err := c.request(ctx, clusterKey, http.MethodGet, setPath, nil, &current); err != nil {
		return err
	}
	have := make(map[string]ipsetEntryState, len(current))
	for _, entry := range current {
		canonical := canonicalIPSetCIDR(entry.CIDR)
		if canonical == "" {
			continue
		}
		have[canonical] = ipsetEntryState{Raw: entry.CIDR, Canonical: canonical}
	}
	want := make(map[string]struct{}, len(entries))
	for _, cidr := range entries {
		canonical := canonicalIPSetCIDR(cidr)
		if canonical == "" {
			continue
		}
		want[canonical] = struct{}{}
		if _, ok := have[canonical]; ok {
			continue
		}
		if err := c.request(ctx, clusterKey, http.MethodPost, setPath, url.Values{"cidr": {canonical}}, nil); err != nil {
			if isAlreadyExistsErr(err) {
				continue
			}
			return err
		}
	}
	for canonical, entry := range have {
		if _, ok := want[canonical]; ok {
			continue
		}
		if err := c.request(ctx, clusterKey, http.MethodDelete, setPath+"/"+url.PathEscape(entry.Raw), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func canonicalIPSetCIDR(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		value = strings.TrimSpace(value[1:])
	}
	if ip, network, err := net.ParseCIDR(value); err == nil {
		if v4 := ip.To4(); v4 != nil {
			network.IP = v4.Mask(network.Mask)
		} else {
			network.IP = ip.Mask(network.Mask)
		}
		return network.String()
	}
	if ip := net.ParseIP(value); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4.String() + "/32"
		}
		return ip.String() + "/128"
	}
	return value
}

func (c *Client) ensureVMGroupRules(ctx context.Context, clusterKey, node string, vmid int, groups []string) error {
	base := fmt.Sprintf("/nodes/%s/qemu/%d/firewall/rules", url.PathEscape(node), vmid)
	for _, group := range groups {
		if group == "" {
			continue
		}
		if err := c.request(ctx, clusterKey, http.MethodPost, base, url.Values{
			"type":   {"group"},
			"action": {group},
			"enable": {"1"},
		}, nil); err != nil {
			if isAlreadyExistsErr(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func (c *Client) applyVMFirewallRules(ctx context.Context, clusterKey, node string, vmid int, rules []SecurityRule) error {
	base := fmt.Sprintf("/nodes/%s/qemu/%d/firewall/rules", url.PathEscape(node), vmid)
	if err := c.clearFirewallRules(ctx, clusterKey, base); err != nil {
		return err
	}
	for _, rule := range rules {
		params := url.Values{
			"type":   {rule.Direction},
			"action": {pveAction(rule.Action)},
			"enable": {"1"},
		}
		if rule.Protocol != "" {
			params.Set("proto", rule.Protocol)
		}
		if rule.CIDR != "" {
			if rule.Direction == "out" {
				params.Set("dest", rule.CIDR)
			} else {
				params.Set("source", rule.CIDR)
			}
		}
		if rule.Protocol == "tcp" || rule.Protocol == "udp" {
			if rule.PortStart != nil && rule.PortEnd != nil {
				if *rule.PortStart == *rule.PortEnd {
					params.Set("dport", strconv.Itoa(*rule.PortStart))
				} else {
					params.Set("dport", fmt.Sprintf("%d:%d", *rule.PortStart, *rule.PortEnd))
				}
			} else if rule.PortStart != nil {
				params.Set("dport", strconv.Itoa(*rule.PortStart))
			}
		}
		if err := c.request(ctx, clusterKey, http.MethodPost, base, params, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) clearFirewallRules(ctx context.Context, clusterKey, basePath string) error {
	var rules []firewallRule
	if err := c.request(ctx, clusterKey, http.MethodGet, basePath, nil, &rules); err != nil {
		return err
	}
	for i := len(rules) - 1; i >= 0; i-- {
		pos := rules[i].Pos
		if err := c.request(ctx, clusterKey, http.MethodDelete, basePath+"/"+strconv.Itoa(pos), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func pveAction(action string) string {
	switch action {
	case "accept":
		return "ACCEPT"
	case "drop":
		return "DROP"
	default:
		return action
	}
}

func pvePolicy(policy string) string {
	switch policy {
	case "", "accept":
		return "ACCEPT"
	case "drop":
		return "DROP"
	default:
		return strings.ToUpper(policy)
	}
}

func isAlreadyExistsErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "already a pool member")
}
