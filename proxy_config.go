package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type ProxyConfig struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Remark string `json:"remark"`
}

type ProxyBinding struct {
	Provider        string `json:"provider"`
	Account         string `json:"account"`
	ProxyID         string `json:"proxyId"`
	FallbackProxyID string `json:"fallbackProxyId"`
}

const (
	proxyFallbackNone   = "none"
	proxyFallbackDirect = "direct"
)

var proxyConfigMu sync.Mutex

func LoadProxyConfig(path string, cfg *Config) error {
	cfg.Proxies = nil
	cfg.ProxyBindings = nil
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	sections := map[string]map[string]map[string]string{
		"proxy":         {},
		"proxy_binding": {},
	}
	sectionOrder := map[string][]string{
		"proxy":         {},
		"proxy_binding": {},
	}
	currentBlock := ""
	currentSection := ""
	for lineNo, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasSuffix(lower, "=begin") {
			currentBlock = strings.TrimSpace(strings.TrimSuffix(lower, "=begin"))
			currentSection = ""
			if _, ok := sections[currentBlock]; !ok {
				return fmt.Errorf("line %d: unsupported block %q", lineNo+1, currentBlock)
			}
			continue
		}
		if strings.HasSuffix(lower, "=end") {
			endBlock := strings.TrimSpace(strings.TrimSuffix(lower, "=end"))
			if currentBlock != endBlock {
				return fmt.Errorf("line %d: mismatched end block %q", lineNo+1, endBlock)
			}
			currentBlock = ""
			currentSection = ""
			continue
		}
		if currentBlock == "" {
			return fmt.Errorf("line %d: setting outside block", lineNo+1)
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			if currentSection == "" {
				return fmt.Errorf("line %d: empty section name", lineNo+1)
			}
			if _, exists := sections[currentBlock][currentSection]; exists {
				return fmt.Errorf("line %d: duplicate section %q", lineNo+1, currentSection)
			}
			sections[currentBlock][currentSection] = map[string]string{}
			sectionOrder[currentBlock] = append(sectionOrder[currentBlock], currentSection)
			continue
		}
		if currentSection == "" {
			return fmt.Errorf("line %d: setting before section", lineNo+1)
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("line %d: invalid setting", lineNo+1)
		}
		sections[currentBlock][currentSection][strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(value), `"`)
	}

	for _, id := range sectionOrder["proxy"] {
		values := sections["proxy"][id]
		normalizedURL, err := normalizeProxyURL(values["url"])
		if err != nil {
			return fmt.Errorf("proxy %s: %w", id, err)
		}
		cfg.Proxies = append(cfg.Proxies, ProxyConfig{ID: id, URL: normalizedURL, Remark: values["remark"]})
	}
	for _, name := range sectionOrder["proxy_binding"] {
		values := sections["proxy_binding"][name]
		cfg.ProxyBindings = append(cfg.ProxyBindings, ProxyBinding{
			Provider:        strings.ToLower(values["provider"]),
			Account:         values["account"],
			ProxyID:         values["proxy"],
			FallbackProxyID: normalizeProxyFallback(values["fallback"]),
		})
	}
	return validateProxyState(cfg.Proxies, cfg.ProxyBindings)
}

func SaveProxyConfig(path string, proxies []ProxyConfig, bindings []ProxyBinding) error {
	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()

	if path == "" {
		return fmt.Errorf("proxy config path is empty")
	}
	if err := validateProxyState(proxies, bindings); err != nil {
		return err
	}

	var lines []string
	lines = append(lines, "proxy=begin")
	for _, proxy := range proxies {
		lines = append(lines,
			"["+proxy.ID+"]",
			"url="+proxy.URL,
			"remark="+proxy.Remark,
		)
	}
	lines = append(lines, "proxy=end", "", "proxy_binding=begin")
	for i, binding := range bindings {
		lines = append(lines,
			fmt.Sprintf("[binding-%d]", i+1),
			"provider="+strings.ToLower(binding.Provider),
			"account="+binding.Account,
			"proxy="+binding.ProxyID,
			"fallback="+normalizeProxyFallback(binding.FallbackProxyID),
		)
	}
	lines = append(lines, "proxy_binding=end", "")

	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600)
}

func validateProxyState(proxies []ProxyConfig, bindings []ProxyBinding) error {
	proxyIDs := make(map[string]struct{}, len(proxies))
	for i := range proxies {
		proxy := &proxies[i]
		if proxy.ID == "" || hasUnsafeConfigValue(proxy.ID) || strings.ContainsAny(proxy.ID, `/\\`) {
			return fmt.Errorf("invalid proxy id %q", proxy.ID)
		}
		if isReservedProxyFallback(proxy.ID) {
			return fmt.Errorf("proxy id %q is reserved for fallback routing", proxy.ID)
		}
		if _, exists := proxyIDs[proxy.ID]; exists {
			return fmt.Errorf("duplicate proxy id %q", proxy.ID)
		}
		if _, err := normalizeProxyURL(proxy.URL); err != nil {
			return fmt.Errorf("proxy %s: %w", proxy.ID, err)
		}
		if hasUnsafeConfigValue(proxy.Remark) {
			return fmt.Errorf("proxy %s remark contains unsupported characters", proxy.ID)
		}
		proxyIDs[proxy.ID] = struct{}{}
	}

	bindingKeys := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if binding.Provider == "" || binding.Account == "" || binding.ProxyID == "" {
			return fmt.Errorf("proxy binding requires provider, account and proxy")
		}
		if hasUnsafeConfigValue(binding.Provider) || hasUnsafeConfigValue(binding.Account) {
			return fmt.Errorf("proxy binding contains unsupported characters")
		}
		if _, ok := proxyIDs[binding.ProxyID]; !ok {
			return fmt.Errorf("proxy binding references missing proxy %q", binding.ProxyID)
		}
		fallback := normalizeProxyFallback(binding.FallbackProxyID)
		if fallback != proxyFallbackNone && fallback != proxyFallbackDirect {
			if fallback == binding.ProxyID {
				return fmt.Errorf("fallback proxy must differ from primary proxy")
			}
			if _, ok := proxyIDs[fallback]; !ok {
				return fmt.Errorf("proxy binding references missing fallback proxy %q", fallback)
			}
		}
		key := proxyBindingKey(binding.Provider, binding.Account)
		if _, exists := bindingKeys[key]; exists {
			return fmt.Errorf("duplicate proxy binding for %s/%s", binding.Provider, binding.Account)
		}
		bindingKeys[key] = struct{}{}
	}
	return nil
}

func proxyBindingKey(provider, account string) string {
	return strings.ToLower(provider) + "\x00" + account
}

func normalizeProxyFallback(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case value == "", strings.EqualFold(value, proxyFallbackNone):
		return proxyFallbackNone
	case strings.EqualFold(value, proxyFallbackDirect):
		return proxyFallbackDirect
	default:
		return value
	}
}

func isReservedProxyFallback(value string) bool {
	return strings.EqualFold(value, proxyFallbackNone) || strings.EqualFold(value, proxyFallbackDirect)
}
