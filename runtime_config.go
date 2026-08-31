package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var runtimeState = &RuntimeState{}

type RuntimeState struct {
	mu              sync.RWMutex
	cfg             *Config
	configPath      string
	configModTime   time.Time
	azureService    *AzureService
	azureServices   map[string]*AzureService
	cloudServices   map[string]CloudService
	cloudAccounts   []gin.H
	cloudflare      *CloudflareService
	auth            *AuthService
	lastReloadError string
}

func InitRuntime(cfg *Config, path string, auth *AuthService) error {
	next, err := buildCloudRuntime(cfg)
	if err != nil {
		return err
	}
	runtimeState.mu.Lock()
	defer runtimeState.mu.Unlock()

	runtimeState.cfg = cfg
	runtimeState.configPath = path
	runtimeState.configModTime = configModTime(path)
	runtimeState.azureService = next.azureService
	runtimeState.azureServices = next.azureServices
	runtimeState.cloudServices = next.cloudServices
	runtimeState.cloudAccounts = next.cloudAccounts
	runtimeState.cloudflare = next.cloudflare
	runtimeState.auth = auth
	runtimeState.lastReloadError = ""
	return nil
}

func ReloadRuntimeConfig() error {
	cfg, path, err := LoadConfigWithPath()
	if err != nil {
		setReloadError(err)
		return err
	}
	next, err := buildCloudRuntime(cfg)
	if err != nil {
		setReloadError(err)
		return err
	}

	runtimeState.mu.RLock()
	auth := runtimeState.auth
	runtimeState.mu.RUnlock()
	if auth != nil {
		if err := auth.UpdateConfig(cfg.Auth); err != nil {
			setReloadError(err)
			return err
		}
	}

	runtimeState.mu.Lock()
	defer runtimeState.mu.Unlock()
	runtimeState.cfg = cfg
	runtimeState.configPath = path
	runtimeState.configModTime = configModTime(path)
	runtimeState.azureService = next.azureService
	runtimeState.azureServices = next.azureServices
	runtimeState.cloudServices = next.cloudServices
	runtimeState.cloudAccounts = next.cloudAccounts
	runtimeState.cloudflare = next.cloudflare
	runtimeState.lastReloadError = ""
	return nil
}

func currentConfigPath() string {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	return runtimeState.configPath
}

func currentDNSPath() string {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	if runtimeState.cfg != nil && runtimeState.cfg.DNSPath != "" {
		return runtimeState.cfg.DNSPath
	}
	return dnsConfigPath(runtimeState.configPath)
}

func currentAuthConfig() AuthConfig {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	if runtimeState.cfg == nil {
		return AuthConfig{}
	}
	return runtimeState.cfg.Auth
}

func currentAuthService() *AuthService {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	return runtimeState.auth
}

func configStatus() gin.H {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	return gin.H{
		"path":            runtimeState.configPath,
		"lastReloadError": runtimeState.lastReloadError,
		"loadedAt":        runtimeState.configModTime.Format(time.RFC3339),
	}
}

func cloudAccountsSnapshot() []gin.H {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	accounts := make([]gin.H, len(runtimeState.cloudAccounts))
	copy(accounts, runtimeState.cloudAccounts)
	return accounts
}

func serviceSnapshot(provider, account string) (CloudService, *CloudflareService, bool) {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	if account == "" {
		return runtimeState.azureService, runtimeState.cloudflare, runtimeState.azureService != nil
	}
	service, ok := runtimeState.cloudServices[serviceKey(provider, account)]
	return service, runtimeState.cloudflare, ok
}

func setReloadError(err error) {
	runtimeState.mu.Lock()
	defer runtimeState.mu.Unlock()
	runtimeState.lastReloadError = err.Error()
}

func configModTime(path string) time.Time {
	if path == "" {
		return time.Time{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

type cloudRuntime struct {
	azureService  *AzureService
	azureServices map[string]*AzureService
	cloudServices map[string]CloudService
	cloudAccounts []gin.H
	cloudflare    *CloudflareService
}

func buildCloudRuntime(cfg *Config) (cloudRuntime, error) {
	proxyRouter := NewProxyRouter(cfg.Proxies, cfg.ProxyBindings)
	next := cloudRuntime{
		azureServices: make(map[string]*AzureService),
		cloudServices: make(map[string]CloudService),
		cloudAccounts: nil,
		cloudflare:    NewCloudflareService(cfg),
	}

	for _, account := range cfg.AzureAccounts {
		service := NewAzureAccountService(cfg, account)
		client, err := proxyRouter.HTTPClient("azure", account.Name, 60*time.Second)
		if err != nil {
			return cloudRuntime{}, fmt.Errorf("configure Azure proxy for %s: %w", account.Name, err)
		}
		service.client = client
		next.azureServices[account.Name] = service
		next.cloudServices[serviceKey("azure", account.Name)] = service
		next.cloudAccounts = append(next.cloudAccounts, gin.H{
			"provider": "azure",
			"account":  account.Name,
			"group":    account.Group,
		})
		if next.azureService == nil {
			next.azureService = service
		}
	}
	if next.azureService == nil {
		next.azureService = NewAzureService(cfg)
		client, err := proxyRouter.HTTPClient("azure", cfg.Azure.Name, 60*time.Second)
		if err != nil {
			return cloudRuntime{}, fmt.Errorf("configure Azure proxy for %s: %w", cfg.Azure.Name, err)
		}
		next.azureService.client = client
	}

	for _, account := range cfg.GCPAccounts {
		service := NewGCPService(account)
		client, err := proxyRouter.HTTPClient("gcp", account.Name, 60*time.Second)
		if err != nil {
			return cloudRuntime{}, fmt.Errorf("configure GCP proxy for %s: %w", account.Name, err)
		}
		service.client = client
		next.cloudServices[serviceKey("gcp", account.Name)] = service
		next.cloudAccounts = append(next.cloudAccounts, gin.H{
			"provider": "gcp",
			"account":  account.Name,
			"group":    account.Group,
		})
	}
	for _, account := range cfg.OCIAccounts {
		service := NewOCIService(account)
		client, err := proxyRouter.HTTPClient("oci", account.Name, 60*time.Second)
		if err != nil {
			return cloudRuntime{}, fmt.Errorf("configure OCI proxy for %s: %w", account.Name, err)
		}
		service.client = client
		next.cloudServices[serviceKey("oci", account.Name)] = service
		next.cloudAccounts = append(next.cloudAccounts, gin.H{
			"provider": "oci",
			"account":  account.Name,
			"group":    account.Group,
		})
	}
	return next, nil
}
