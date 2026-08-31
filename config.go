package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Azure         AzureConfig        `yaml:"azure"`
	Default       DefaultConfig      `yaml:"default"`
	SourcePath    string             `yaml:"-"`
	DNSPath       string             `yaml:"-"`
	ProxyPath     string             `yaml:"-"`
	AzureAccounts []AzureConfig      `yaml:"-"`
	GCPAccounts   []GCPConfig        `yaml:"-"`
	OCIAccounts   []OCIConfig        `yaml:"-"`
	Cloudflare    []CloudflareConfig `yaml:"-"`
	DNSBindings   []DNSBinding       `yaml:"-"`
	Proxies       []ProxyConfig      `yaml:"-"`
	ProxyBindings []ProxyBinding     `yaml:"-"`
	Auth          AuthConfig         `yaml:"auth"`
	Update        UpdateConfig       `yaml:"update"`
}

type UpdateConfig struct {
	DownloadProxy string `yaml:"download_proxy"`
}

type AzureConfig struct {
	Name          string `yaml:"-"`
	Group         string `yaml:"-"`
	TenantId      string `yaml:"tenant_id"`
	ClientId      string `yaml:"client_id"`
	ClientSecret  string `yaml:"client_secret"`
	SubId         string `yaml:"subscription_id"`
	ResourceGroup string `yaml:"resource_group"`
	Location      string `yaml:"location"`
}

type GCPConfig struct {
	Name        string
	Group       string
	ProjectID   string
	ClientEmail string
	KeyFile     string
}

type OCIConfig struct {
	Name          string
	Group         string
	User          string
	Fingerprint   string
	Tenancy       string
	CompartmentID string
	Region        string
	KeyFile       string
	DTMonitor     DataTransferConfig
}

type DefaultConfig struct {
	ResourceGroup string `yaml:"resource_group"`
	Location      string `yaml:"location"`
}

type CloudflareConfig struct {
	Name     string
	Remark   string
	APIToken string
	ZoneID   string
}

type DNSBinding struct {
	Name       string
	Cloudflare string
	Provider   string
	Account    string
	VM         string
	Domain     string
	Type       string
	TTL        int
	Proxied    bool
}

type AuthConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Username      string `yaml:"username"`
	PasswordHash  string `yaml:"password_hash"`
	SessionSecret string `yaml:"session_secret"`
	SessionHours  int    `yaml:"session_hours"`
	CookieSecure  bool   `yaml:"cookie_secure"`
}

// configSearchPaths returns the list of paths to search for the main config file.
func configSearchPaths() []string {
	return []string{
		"config/config.conf",
		"config/config.ini",
		"config/config.yaml",
		"config.conf",
		"config.ini",
		"config.yaml",
	}
}

// dnsConfigPath returns the DNS config file path relative to the main config.
func dnsConfigPath(mainPath string) string {
	if strings.HasPrefix(mainPath, "config/") || strings.HasPrefix(mainPath, "config\\") {
		return "config/dns.conf"
	}
	return "dns.conf"
}

func proxyConfigPath(mainPath string) string {
	if strings.HasPrefix(mainPath, "config/") || strings.HasPrefix(mainPath, "config\\") {
		return "config/proxy.conf"
	}
	return "proxy.conf"
}

func LoadConfig() (*Config, error) {
	cfg, _, err := LoadConfigWithPath()
	return cfg, err
}

func LoadConfigWithPath() (*Config, string, error) {
	var data []byte
	var err error
	configPath := ""

	for _, path := range configSearchPaths() {
		data, err = os.ReadFile(path)
		if err == nil {
			configPath = path
			break
		}
	}
	if err != nil {
		return nil, "", fmt.Errorf("read config: %w", err)
	}

	var cfg *Config
	if strings.Contains(string(data), "=begin") {
		cfg, err = parseBlockConfig(string(data))
		if err != nil {
			return nil, "", err
		}
		cfg.SourcePath = configPath
	} else {
		var yamlCfg Config
		if err := yaml.Unmarshal(data, &yamlCfg); err != nil {
			return nil, "", err
		}
		yamlCfg.normalize()
		yamlCfg.SourcePath = configPath
		cfg = &yamlCfg
	}

	// Load DNS config from separate file
	dnsPath := dnsConfigPath(configPath)
	cfg.DNSPath = dnsPath
	if dnsData, dnsErr := os.ReadFile(dnsPath); dnsErr == nil {
		if err := mergeDNSConfig(cfg, string(dnsData)); err != nil {
			return nil, "", fmt.Errorf("parse dns config: %w", err)
		}
	}

	proxyPath := proxyConfigPath(configPath)
	cfg.ProxyPath = proxyPath
	if proxyErr := LoadProxyConfig(proxyPath, cfg); proxyErr != nil {
		return nil, "", fmt.Errorf("parse proxy config: %w", proxyErr)
	}

	return cfg, configPath, nil
}

// mergeDNSConfig parses cloudflare and dns blocks from dns.conf and merges into cfg.
// If the main config already had cloudflare/dns from the old format, dns.conf takes precedence.
func mergeDNSConfig(cfg *Config, content string) error {
	sections := map[string]map[string]map[string]string{
		"cloudflare": {},
		"dns":        {},
	}

	currentProvider := ""
	currentName := ""

	for lineNo, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		lower := strings.ToLower(line)
		if strings.HasSuffix(lower, "=begin") {
			provider := strings.TrimSpace(strings.TrimSuffix(lower, "=begin"))
			if provider != "cloudflare" && provider != "dns" {
				return fmt.Errorf("dns.conf line %d: unsupported block %q (only cloudflare and dns allowed)", lineNo+1, provider)
			}
			currentProvider = provider
			currentName = ""
			continue
		}
		if strings.HasSuffix(lower, "=end") {
			endProvider := strings.TrimSpace(strings.TrimSuffix(lower, "=end"))
			if currentProvider != endProvider {
				return fmt.Errorf("dns.conf line %d: mismatched end block %q", lineNo+1, endProvider)
			}
			currentProvider = ""
			currentName = ""
			continue
		}

		if currentProvider == "" {
			continue // skip lines outside blocks
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentName = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			if currentName == "" {
				return fmt.Errorf("dns.conf line %d: empty section name", lineNo+1)
			}
			if _, ok := sections[currentProvider][currentName]; !ok {
				sections[currentProvider][currentName] = map[string]string{}
			}
			continue
		}

		if currentName == "" {
			return fmt.Errorf("dns.conf line %d: setting before section", lineNo+1)
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("dns.conf line %d: invalid setting", lineNo+1)
		}
		sections[currentProvider][currentName][strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}

	// Replace cloudflare from dns.conf
	if len(sections["cloudflare"]) > 0 {
		cfg.Cloudflare = nil
		for _, name := range sortedSectionNames(sections["cloudflare"]) {
			values := sections["cloudflare"][name]
			cfg.Cloudflare = append(cfg.Cloudflare, CloudflareConfig{
				Name:     name,
				Remark:   values["remark"],
				APIToken: values["api_token"],
				ZoneID:   values["zone_id"],
			})
		}
	}

	// Replace dns bindings from dns.conf
	if len(sections["dns"]) > 0 {
		cfg.DNSBindings = nil
		for _, name := range sortedSectionNames(sections["dns"]) {
			values := sections["dns"][name]
			ttl := intValueOrDefault(values["ttl"], 1)
			cfg.DNSBindings = append(cfg.DNSBindings, DNSBinding{
				Name:       name,
				Cloudflare: valueOrDefault(values["cloudflare"], values["cf"]),
				Provider:   strings.ToLower(values["provider"]),
				Account:    values["account"],
				VM:         valueOrDefault(values["vm"], valueOrDefault(values["vm_id"], values["instance"])),
				Domain:     values["domain"],
				Type:       valueOrDefault(strings.ToUpper(values["type"]), "A"),
				TTL:        ttl,
				Proxied:    boolValue(values["proxied"]),
			})
		}
	}

	return nil
}

func (cfg *Config) normalize() {
	if cfg.Azure.ResourceGroup == "" {
		cfg.Azure.ResourceGroup = cfg.Default.ResourceGroup
	}
	if cfg.Azure.Location == "" {
		cfg.Azure.Location = cfg.Default.Location
	}

	if cfg.Azure.SubId != "" || cfg.Azure.ClientId != "" || cfg.Azure.ClientSecret != "" || cfg.Azure.TenantId != "" {
		if cfg.Azure.Name == "" {
			cfg.Azure.Name = "default"
		}
		if cfg.Azure.Group == "" {
			cfg.Azure.Group = "azure"
		}
		cfg.AzureAccounts = append(cfg.AzureAccounts, cfg.Azure)
	}

	if len(cfg.AzureAccounts) > 0 {
		cfg.Azure = cfg.AzureAccounts[0]
	}
}

func parseBlockConfig(content string) (*Config, error) {
	sections := map[string]map[string]map[string]string{
		"azure":      {},
		"gcp":        {},
		"oci":        {},
		"cloudflare": {},
		"dns":        {},
		"auth":       {},
		"update":     {},
	}

	currentProvider := ""
	currentName := ""

	for lineNo, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		lower := strings.ToLower(line)
		if strings.HasSuffix(lower, "=begin") {
			currentProvider = strings.TrimSpace(strings.TrimSuffix(lower, "=begin"))
			currentName = ""
			if _, ok := sections[currentProvider]; !ok {
				return nil, fmt.Errorf("line %d: unsupported provider %q", lineNo+1, currentProvider)
			}
			continue
		}
		if strings.HasSuffix(lower, "=end") {
			endProvider := strings.TrimSpace(strings.TrimSuffix(lower, "=end"))
			if currentProvider != endProvider {
				return nil, fmt.Errorf("line %d: mismatched end block %q", lineNo+1, endProvider)
			}
			currentProvider = ""
			currentName = ""
			continue
		}

		if currentProvider == "" {
			return nil, fmt.Errorf("line %d: setting outside provider block", lineNo+1)
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentName = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			if currentName == "" {
				return nil, fmt.Errorf("line %d: empty account name", lineNo+1)
			}
			if _, ok := sections[currentProvider][currentName]; !ok {
				sections[currentProvider][currentName] = map[string]string{}
			}
			continue
		}

		if currentName == "" {
			return nil, fmt.Errorf("line %d: setting before account section", lineNo+1)
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: invalid setting", lineNo+1)
		}
		sections[currentProvider][currentName][strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}

	cfg := &Config{}
	for _, name := range sortedSectionNames(sections["azure"]) {
		values := sections["azure"][name]
		account := AzureConfig{
			Name:          name,
			Group:         valueOrDefault(values["group"], "azure"),
			SubId:         values["subscription_id"],
			ClientId:      valueOrDefault(values["client_id"], values["appId"]),
			ClientSecret:  valueOrDefault(values["client_secret"], values["password"]),
			TenantId:      valueOrDefault(values["tenant_id"], values["tenant"]),
			ResourceGroup: values["resource_group"],
			Location:      values["location"],
		}
		cfg.AzureAccounts = append(cfg.AzureAccounts, account)
	}
	for _, name := range sortedSectionNames(sections["gcp"]) {
		values := sections["gcp"][name]
		cfg.GCPAccounts = append(cfg.GCPAccounts, GCPConfig{
			Name:        name,
			Group:       valueOrDefault(values["group"], "gcp"),
			ProjectID:   values["project_id"],
			ClientEmail: values["client_email"],
			KeyFile:     values["key_file"],
		})
	}
	for _, name := range sortedSectionNames(sections["oci"]) {
		values := sections["oci"][name]
		cfg.OCIAccounts = append(cfg.OCIAccounts, OCIConfig{
			Name:          name,
			Group:         valueOrDefault(values["group"], "oci"),
			User:          values["user"],
			Fingerprint:   values["fingerprint"],
			Tenancy:       values["tenancy"],
			CompartmentID: valueOrDefault(values["compartment_id"], values["tenancy"]),
			Region:        values["region"],
			KeyFile:       values["key_file"],
			DTMonitor: DataTransferConfig{
				Enabled:    boolValue(values["dt_monitor_enabled"]),
				Interval:   intValueOrDefault(values["dt_monitor_interval"], 300),
				Threshold:  floatValueOrDefault(values["dt_monitor_threshold"], 9000),
				AutoStop:   boolValue(values["dt_monitor_auto_stop"]),
				StopMethod: valueOrDefault(values["dt_monitor_stop_method"], "soft"),
			},
		})
	}
	for _, name := range sortedSectionNames(sections["cloudflare"]) {
		values := sections["cloudflare"][name]
		cfg.Cloudflare = append(cfg.Cloudflare, CloudflareConfig{
			Name:     name,
			Remark:   values["remark"],
			APIToken: values["api_token"],
			ZoneID:   values["zone_id"],
		})
	}
	for _, name := range sortedSectionNames(sections["dns"]) {
		values := sections["dns"][name]
		ttl := intValueOrDefault(values["ttl"], 1)
		cfg.DNSBindings = append(cfg.DNSBindings, DNSBinding{
			Name:       name,
			Cloudflare: valueOrDefault(values["cloudflare"], values["cf"]),
			Provider:   strings.ToLower(values["provider"]),
			Account:    values["account"],
			VM:         valueOrDefault(values["vm"], valueOrDefault(values["vm_id"], values["instance"])),
			Domain:     values["domain"],
			Type:       valueOrDefault(strings.ToUpper(values["type"]), "A"),
			TTL:        ttl,
			Proxied:    boolValue(values["proxied"]),
		})
	}
	if values, ok := sections["auth"]["main"]; ok {
		cfg.Auth = AuthConfig{
			Enabled:       boolValue(values["enabled"]),
			Username:      values["username"],
			PasswordHash:  values["password_hash"],
			SessionSecret: values["session_secret"],
			SessionHours:  intValueOrDefault(values["session_hours"], 12),
			CookieSecure:  boolValue(values["cookie_secure"]),
		}
	}
	if values, ok := sections["update"]["main"]; ok {
		cfg.Update = UpdateConfig{
			DownloadProxy: values["download_proxy"],
		}
	}

	cfg.normalize()
	return cfg, nil
}

func intValueOrDefault(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func floatValueOrDefault(value string, fallback float64) float64 {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolValue(value string) bool {
	parsed, _ := strconv.ParseBool(strings.ToLower(strings.TrimSpace(value)))
	return parsed
}

func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func sortedSectionNames(section map[string]map[string]string) []string {
	names := make([]string, 0, len(section))
	for name := range section {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
