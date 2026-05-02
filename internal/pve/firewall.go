package pve

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type FirewallSpec struct {
	UserGroupName string
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
	have := make(map[string]struct{}, len(current))
	for _, entry := range current {
		have[entry.CIDR] = struct{}{}
	}
	for _, cidr := range entries {
		if cidr == "" {
			continue
		}
		if _, ok := have[cidr]; ok {
			continue
		}
		if err := c.request(ctx, clusterKey, http.MethodPost, setPath, url.Values{"cidr": {cidr}}, nil); err != nil {
			if isAlreadyExistsErr(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func (c *Client) ensureVMGroupRules(ctx context.Context, clusterKey, node string, vmid int, groups []string) error {
	base := fmt.Sprintf("/nodes/%s/qemu/%d/firewall/rules", url.PathEscape(node), vmid)
	if err := c.clearFirewallRules(ctx, clusterKey, base); err != nil {
		return err
	}
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
	case "allow":
		return "ACCEPT"
	case "deny":
		return "DROP"
	default:
		return action
	}
}

func isAlreadyExistsErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "already a pool member")
}
