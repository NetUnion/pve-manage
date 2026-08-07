package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

type Runtime struct {
	ListenAddr string
	DBURL      string
	ConfigPath string
	OIDCPath   string
	TokenPath  string
}

type Root struct {
	Cluster map[string]Cluster `yaml:"cluster"`
	User    UserConfig         `yaml:"user"`
}

type Cluster struct {
	Name      string                  `yaml:"name"`
	Limit     int                     `yaml:"limit"`
	StartVMID int                     `yaml:"start_vmid"`
	CPU       map[string]CPUClass     `yaml:"cpu"`
	Storage   map[string]StorageClass `yaml:"storage"`
	Network   NetworkConfig           `yaml:"network"`
	TPM       string                  `yaml:"tpm"`
}

type CPUClass struct {
	Name        string   `yaml:"name"`
	Limit       int      `yaml:"limit"`
	MemoryLimit int      `yaml:"memory_limit"`
	Node        []string `yaml:"node"`
}

type StorageClass struct {
	Name  string `yaml:"name"`
	Limit int    `yaml:"limit"`
}

type NetworkConfig struct {
	UESTC  string                  `yaml:"uestc"`
	Bridge map[string]BridgeConfig `yaml:"bridge"`
}

type BridgeConfig struct {
	IPv4     IPv4Config `yaml:"ipv4"`
	IPv6     IPv6Config `yaml:"ipv6"`
	IPFilter []string   `yaml:"ipfilter"`
}

type IPv4Config struct {
	Type    string `yaml:"type"`
	StartIP string `yaml:"start_ip"`
	CIDR    int    `yaml:"cidr"`
	Gateway string `yaml:"gateway"`
}

type IPv6Config struct {
	Type string `yaml:"type"`
}

type UserConfig struct {
	AdminGroup []string             `yaml:"admin_group"`
	Limit      map[string]UserLimit `yaml:"limit"`
}

type UserLimit struct {
	Number        int    `yaml:"number"`
	CPU           int    `yaml:"cpu"`
	Memory        int    `yaml:"memory"`
	Storage       int    `yaml:"storage"`
	SecurityGroup int    `yaml:"security_group"`
	UESTC         string `yaml:"uestc"`
}

type OIDC struct {
	Issuer       string        `yaml:"issuer"`
	ClientID     string        `yaml:"client_id"`
	ClientSecret string        `yaml:"client_secret"`
	RedirectURL  string        `yaml:"redirect_url"`
	Scopes       []string      `yaml:"scopes"`
	Claims       OIDCClaims    `yaml:"claims"`
	Session      SessionConfig `yaml:"session"`
}

type OIDCClaims struct {
	Username string `yaml:"username"`
	Email    string `yaml:"email"`
	Name     string `yaml:"name"`
	Groups   string `yaml:"groups"`
}

type SessionConfig struct {
	CookieName     string `yaml:"cookie_name"`
	CookieSecure   bool   `yaml:"cookie_secure"`
	CookieSameSite string `yaml:"cookie_same_site"`
	MaxAgeSeconds  int    `yaml:"max_age_seconds"`
}

type TokenFile struct {
	Cluster map[string]ClusterToken `yaml:"cluster"`
}

type ClusterToken struct {
	Site  string `yaml:"site"`
	Token string `yaml:"token"`
}

type App struct {
	Runtime Runtime
	Root    Root
	OIDC    OIDC
	Tokens  TokenFile
}

func Load(runtime Runtime) (*App, error) {
	root, err := loadYAML[Root](runtime.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	oidc, err := loadYAML[OIDC](runtime.OIDCPath)
	if err != nil {
		return nil, fmt.Errorf("load oidc config: %w", err)
	}

	tokens, err := loadYAML[TokenFile](runtime.TokenPath)
	if err != nil {
		return nil, fmt.Errorf("load token config: %w", err)
	}

	app := &App{
		Runtime: runtime,
		Root:    root,
		OIDC:    oidc,
		Tokens:  tokens,
	}

	if err := app.Validate(); err != nil {
		return nil, err
	}

	return app, nil
}

func (a *App) Validate() error {
	if len(a.Root.Cluster) == 0 {
		return errors.New("config.cluster must not be empty")
	}
	if len(a.Root.User.Limit) == 0 {
		return errors.New("config.user.limit must not be empty")
	}

	for clusterKey, cluster := range a.Root.Cluster {
		if cluster.Name == "" {
			return fmt.Errorf("cluster %s name is required", clusterKey)
		}
		if cluster.Limit <= 0 {
			return fmt.Errorf("cluster %s limit must be positive", clusterKey)
		}
		if cluster.StartVMID <= 0 {
			return fmt.Errorf("cluster %s start_vmid must be positive", clusterKey)
		}
		if len(cluster.CPU) == 0 {
			return fmt.Errorf("cluster %s cpu must not be empty", clusterKey)
		}
		if len(cluster.Storage) == 0 {
			return fmt.Errorf("cluster %s storage must not be empty", clusterKey)
		}
		if len(cluster.Network.Bridge) == 0 {
			return fmt.Errorf("cluster %s network.bridge must not be empty", clusterKey)
		}
		for bridgeKey, bridge := range cluster.Network.Bridge {
			if bridge.IPv4.Type != "static" {
				return fmt.Errorf("cluster %s bridge %s ipv4.type must be static", clusterKey, bridgeKey)
			}
			if net.ParseIP(bridge.IPv4.StartIP) == nil {
				return fmt.Errorf("cluster %s bridge %s start_ip is invalid", clusterKey, bridgeKey)
			}
			if bridge.IPv4.CIDR < 0 || bridge.IPv4.CIDR > 32 {
				return fmt.Errorf("cluster %s bridge %s cidr must be between 0 and 32", clusterKey, bridgeKey)
			}
			if net.ParseIP(bridge.IPv4.Gateway) == nil {
				return fmt.Errorf("cluster %s bridge %s gateway is invalid", clusterKey, bridgeKey)
			}
			if bridge.IPv6.Type == "" {
				return fmt.Errorf("cluster %s bridge %s ipv6.type is required", clusterKey, bridgeKey)
			}
		}
	}

	for groupKey, limit := range a.Root.User.Limit {
		if limit.Number < 0 || limit.CPU < 0 || limit.Memory < 0 || limit.Storage < 0 || limit.SecurityGroup < 0 {
			return fmt.Errorf("user limit %s must not be negative", groupKey)
		}
		if !slices.Contains([]string{"choose", "force"}, limit.UESTC) {
			return fmt.Errorf("user limit %s uestc must be choose or force", groupKey)
		}
	}

	if a.OIDC.Issuer == "" || a.OIDC.ClientID == "" || a.OIDC.RedirectURL == "" {
		return errors.New("oidc issuer, client_id, and redirect_url are required")
	}
	if a.OIDC.Claims.Username == "" || a.OIDC.Claims.Groups == "" {
		return errors.New("oidc username and groups claims are required")
	}
	if a.OIDC.Session.CookieName == "" {
		return errors.New("oidc session cookie_name is required")
	}
	if a.OIDC.Session.MaxAgeSeconds <= 0 {
		return errors.New("oidc session max_age_seconds must be positive")
	}
	if !slices.Contains([]string{"lax", "strict", "none"}, strings.ToLower(a.OIDC.Session.CookieSameSite)) {
		return errors.New("oidc session cookie_same_site must be lax, strict, or none")
	}

	for clusterKey := range a.Root.Cluster {
		if _, ok := a.Tokens.Cluster[clusterKey]; !ok {
			return fmt.Errorf("token config missing cluster %s", clusterKey)
		}
	}
	for clusterKey, token := range a.Tokens.Cluster {
		if token.Site == "" || token.Token == "" {
			return fmt.Errorf("token config cluster %s site and token are required", clusterKey)
		}
	}

	return nil
}

func (a *App) AdminGroups() []string {
	return slices.Clone(a.Root.User.AdminGroup)
}

func (a *App) IsAdmin(groups []string) bool {
	adminGroups := make(map[string]struct{}, len(a.Root.User.AdminGroup))
	for _, g := range a.Root.User.AdminGroup {
		adminGroups[g] = struct{}{}
	}
	for _, g := range groups {
		if _, ok := adminGroups[g]; ok {
			return true
		}
	}
	return false
}

func (a *App) EffectiveUserLimit(groups []string) UserLimit {
	var out UserLimit
	var seen bool
	for _, group := range groups {
		limit, ok := a.Root.User.Limit[group]
		if !ok {
			continue
		}
		if !seen {
			out = limit
			seen = true
			continue
		}
		if limit.Number > out.Number {
			out.Number = limit.Number
		}
		if limit.CPU > out.CPU {
			out.CPU = limit.CPU
		}
		if limit.Memory > out.Memory {
			out.Memory = limit.Memory
		}
		if limit.Storage > out.Storage {
			out.Storage = limit.Storage
		}
		if limit.SecurityGroup > out.SecurityGroup {
			out.SecurityGroup = limit.SecurityGroup
		}
		if limit.UESTC == "choose" {
			out.UESTC = "choose"
		} else if !seen {
			out.UESTC = limit.UESTC
		}
	}
	return out
}

func (a *App) ClusterByKey(key string) (Cluster, bool) {
	cluster, ok := a.Root.Cluster[key]
	return cluster, ok
}

func (c Cluster) CPUByKey(key string) (CPUClass, bool) {
	cpu, ok := c.CPU[key]
	return cpu, ok
}

func (c Cluster) StorageByKey(key string) (StorageClass, bool) {
	storage, ok := c.Storage[key]
	return storage, ok
}

func (c Cluster) BridgeByKey(key string) (BridgeConfig, bool) {
	bridge, ok := c.Network.Bridge[key]
	return bridge, ok
}

func ValidateUsername(username string) bool {
	return usernamePattern.MatchString(username)
}

func (a *App) SessionDuration() time.Duration {
	return time.Duration(a.OIDC.Session.MaxAgeSeconds) * time.Second
}

func loadYAML[T any](path string) (T, error) {
	var out T

	data, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return out, err
	}

	return out, nil
}
