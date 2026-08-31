package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type proxyInput struct {
	URL    string `json:"url"`
	Remark string `json:"remark"`
}

type proxyBindingInput struct {
	Provider        string `json:"provider"`
	Account         string `json:"account"`
	ProxyID         string `json:"proxyId"`
	FallbackProxyID string `json:"fallbackProxyId"`
}

const proxyPasswordMask = "********"

func listProxies(c *gin.Context) {
	proxies, bindings, path := proxyStateSnapshot()
	c.JSON(http.StatusOK, gin.H{"proxies": maskedProxies(proxies), "bindings": bindings, "configPath": path})
}

func createProxy(c *gin.Context) {
	var input proxyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proxy request"})
		return
	}
	proxy, err := normalizedProxyInput(newProxyID(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	proxies, bindings, path := proxyStateSnapshot()
	proxies = append(proxies, proxy)
	if err := saveAndReloadProxyState(path, proxies, bindings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "proxy": maskProxy(proxy)})
}

func updateProxy(c *gin.Context) {
	var input proxyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proxy request"})
		return
	}
	id := c.Param("id")
	proxies, bindings, path := proxyStateSnapshot()
	foundIndex := -1
	for i := range proxies {
		if proxies[i].ID == id {
			foundIndex = i
			break
		}
	}
	if foundIndex < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy not found"})
		return
	}
	input.URL = restoreMaskedProxyPassword(input.URL, proxies[foundIndex].URL)
	updated, err := normalizedProxyInput(id, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	proxies[foundIndex] = updated
	if err := saveAndReloadProxyState(path, proxies, bindings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "proxy": maskProxy(updated)})
}

func deleteProxy(c *gin.Context) {
	id := c.Param("id")
	proxies, bindings, path := proxyStateSnapshot()
	nextProxies := proxies[:0]
	found := false
	for _, proxy := range proxies {
		if proxy.ID == id {
			found = true
			continue
		}
		nextProxies = append(nextProxies, proxy)
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy not found"})
		return
	}
	nextBindings := bindings[:0]
	for _, binding := range bindings {
		if binding.ProxyID == id {
			continue
		}
		if binding.FallbackProxyID == id {
			binding.FallbackProxyID = proxyFallbackNone
		}
		nextBindings = append(nextBindings, binding)
	}
	if err := saveAndReloadProxyState(path, nextProxies, nextBindings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func importProxies(c *gin.Context) {
	var input struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid import request"})
		return
	}
	imported, errors := parseProxyImport(input.Content)
	if len(errors) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": strings.Join(errors, "; ")})
		return
	}
	if len(imported) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no proxies found in import content"})
		return
	}
	proxies, bindings, path := proxyStateSnapshot()
	proxies = append(proxies, imported...)
	if err := saveAndReloadProxyState(path, proxies, bindings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "imported": len(imported)})
}

func testProxy(c *gin.Context) {
	id := c.Param("id")
	proxies, _, _ := proxyStateSnapshot()
	var selected *ProxyConfig
	for i := range proxies {
		if proxies[i].ID == id {
			selected = &proxies[i]
			break
		}
	}
	if selected == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy not found"})
		return
	}
	transport, err := transportForProxy(selected.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	client := &http.Client{Timeout: 12 * time.Second, Transport: transport}
	request, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, "https://management.azure.com/", nil)
	started := time.Now()
	response, err := client.Do(request)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "latencyMs": latency, "error": err.Error()})
		return
	}
	_ = response.Body.Close()
	c.JSON(http.StatusOK, gin.H{"success": true, "latencyMs": latency, "statusCode": response.StatusCode})
}

func saveProxyBinding(c *gin.Context) {
	var input proxyBindingInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proxy binding request"})
		return
	}
	binding := ProxyBinding{
		Provider:        strings.ToLower(strings.TrimSpace(input.Provider)),
		Account:         strings.TrimSpace(input.Account),
		ProxyID:         strings.TrimSpace(input.ProxyID),
		FallbackProxyID: normalizeProxyFallback(input.FallbackProxyID),
	}
	if _, _, ok := serviceSnapshot(binding.Provider, binding.Account); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("provider/account %s/%s not found", binding.Provider, binding.Account)})
		return
	}
	proxies, bindings, path := proxyStateSnapshot()
	key := proxyBindingKey(binding.Provider, binding.Account)
	found := false
	for i := range bindings {
		if proxyBindingKey(bindings[i].Provider, bindings[i].Account) == key {
			bindings[i] = binding
			found = true
			break
		}
	}
	if !found {
		bindings = append(bindings, binding)
	}
	if err := validateProxyState(proxies, bindings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := saveAndReloadProxyState(path, proxies, bindings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "binding": binding})
}

func deleteProxyBinding(c *gin.Context) {
	provider := c.Query("provider")
	account := c.Query("account")
	if provider == "" || account == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider and account are required"})
		return
	}
	key := proxyBindingKey(provider, account)
	proxies, bindings, path := proxyStateSnapshot()
	next := bindings[:0]
	found := false
	for _, binding := range bindings {
		if proxyBindingKey(binding.Provider, binding.Account) == key {
			found = true
			continue
		}
		next = append(next, binding)
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy binding not found"})
		return
	}
	if err := saveAndReloadProxyState(path, proxies, next); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func proxyStateSnapshot() ([]ProxyConfig, []ProxyBinding, string) {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	if runtimeState.cfg == nil {
		return nil, nil, ""
	}
	proxies := append([]ProxyConfig(nil), runtimeState.cfg.Proxies...)
	bindings := append([]ProxyBinding(nil), runtimeState.cfg.ProxyBindings...)
	return proxies, bindings, runtimeState.cfg.ProxyPath
}

func saveAndReloadProxyState(path string, proxies []ProxyConfig, bindings []ProxyBinding) error {
	if err := SaveProxyConfig(path, proxies, bindings); err != nil {
		return err
	}
	if err := ReloadRuntimeConfig(); err != nil {
		return fmt.Errorf("saved but reload failed: %w", err)
	}
	return nil
}

func normalizedProxyInput(id string, input proxyInput) (ProxyConfig, error) {
	normalizedURL, err := normalizeProxyURL(input.URL)
	if err != nil {
		return ProxyConfig{}, err
	}
	remark := strings.TrimSpace(input.Remark)
	if hasUnsafeConfigValue(remark) {
		return ProxyConfig{}, fmt.Errorf("remark contains unsupported characters")
	}
	return ProxyConfig{ID: id, URL: normalizedURL, Remark: remark}, nil
}

func maskedProxies(proxies []ProxyConfig) []ProxyConfig {
	masked := make([]ProxyConfig, len(proxies))
	for i, proxy := range proxies {
		masked[i] = maskProxy(proxy)
	}
	return masked
}

func maskProxy(proxy ProxyConfig) ProxyConfig {
	parsed, err := url.Parse(proxy.URL)
	if err != nil || parsed.User == nil {
		return proxy
	}
	if _, hasPassword := parsed.User.Password(); !hasPassword {
		return proxy
	}
	parsed.User = url.UserPassword(parsed.User.Username(), proxyPasswordMask)
	proxy.URL = strings.Replace(parsed.String(), url.QueryEscape(proxyPasswordMask), proxyPasswordMask, 1)
	return proxy
}

func restoreMaskedProxyPassword(candidateURL, currentURL string) string {
	candidate, candidateErr := url.Parse(strings.TrimSpace(candidateURL))
	current, currentErr := url.Parse(currentURL)
	if candidateErr != nil || currentErr != nil || candidate.User == nil || current.User == nil {
		return candidateURL
	}
	candidatePassword, hasCandidatePassword := candidate.User.Password()
	currentPassword, hasCurrentPassword := current.User.Password()
	if !hasCandidatePassword || candidatePassword != proxyPasswordMask || !hasCurrentPassword {
		return candidateURL
	}
	candidate.User = url.UserPassword(candidate.User.Username(), currentPassword)
	return candidate.String()
}

func parseProxyImport(content string) ([]ProxyConfig, []string) {
	var proxies []ProxyConfig
	var errors []string
	for index, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		proxyURL := line
		remark := ""
		if marker := strings.Index(line, " # "); marker >= 0 {
			proxyURL = strings.TrimSpace(line[:marker])
			remark = strings.TrimSpace(line[marker+3:])
		}
		proxy, err := normalizedProxyInput(newProxyID(), proxyInput{URL: proxyURL, Remark: remark})
		if err != nil {
			errors = append(errors, fmt.Sprintf("line %d: %v", index+1, err))
			continue
		}
		proxies = append(proxies, proxy)
	}
	return proxies, errors
}

func newProxyID() string {
	data := make([]byte, 6)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("proxy-%d", time.Now().UnixNano())
	}
	return "proxy-" + hex.EncodeToString(data)
}
