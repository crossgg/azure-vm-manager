package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, configPath, err := LoadConfigWithPath()
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		return
	}
	authService, err := NewAuthService(cfg.Auth)
	if err != nil {
		fmt.Printf("auth config failed: %v\n", err)
		return
	}
	if err := InitRuntime(cfg, configPath, authService); err != nil {
		fmt.Printf("runtime init failed: %v\n", err)
		return
	}

	r := gin.Default()
	authService.Register(r)
	r.Use(authService.Middleware())

	r.GET("/api/config/status", getConfigStatus)
	r.POST("/api/config/reload", reloadConfig)
	r.GET("/api/update/status", getUpdateStatus)
	r.POST("/api/update/apply", applyUpdate)
	r.GET("/api/settings/auth", getAuthSettings)
	r.POST("/api/settings/auth", updateAuthSettings)
	r.POST("/api/settings/update", updateUpdateSettings)
	r.GET("/api/accounts", listAccounts)
	r.GET("/api/proxies", listProxies)
	r.POST("/api/proxies", createProxy)
	r.POST("/api/proxies/import", importProxies)
	r.PUT("/api/proxies/:id", updateProxy)
	r.DELETE("/api/proxies/:id", deleteProxy)
	r.POST("/api/proxies/:id/test", testProxy)
	r.PUT("/api/proxy-bindings", saveProxyBinding)
	r.DELETE("/api/proxy-bindings", deleteProxyBinding)
	r.GET("/api/vms", listVMs)
	r.GET("/api/vm/:provider/:account/:name", getVM)
	r.POST("/api/vm/:provider/:account/:name/start", startVM)
	r.POST("/api/vm/:provider/:account/:name/stop", stopVM)
	r.POST("/api/vm/:provider/:account/:name/restart", restartVM)
	r.POST("/api/vm/:provider/:account/:name/change-ip", changeIP)
	r.POST("/api/vm/:provider/:account/:name/update-dns", updateDNS)
	r.GET("/api/refresh/:provider/:account/:name", refreshVM)
	r.GET("/api/vm/:provider/:account/:name/edit-options", getOCIInstanceEditOptions)
	r.POST("/api/vm/:provider/:account/:name/edit", updateOCIInstance)
	r.GET("/api/vm/:provider/:account/:name/security-lists", listOCISecurityLists)
	r.POST("/api/vm/:provider/:account/:name/security-lists/:listID/rules", saveOCISecurityListRules)
	r.GET("/api/vm/:provider/:account/:name/network-security-groups", listOCINetworkSecurityGroups)
	r.POST("/api/vm/:provider/:account/:name/network-security-groups", createOCINetworkSecurityGroup)
	r.POST("/api/vm/:provider/:account/:name/network-security-groups/:groupID/rules", saveOCINetworkSecurityGroupRules)

	// OCI data transfer monitoring APIs
	r.GET("/api/oci/:account/data-transfer", getOCIDataTransfer)
	r.GET("/api/oci/:account/data-transfer/config", getDataTransferConfig)
	r.POST("/api/oci/:account/data-transfer/config", saveDataTransferConfig)
	r.POST("/api/oci/:account/data-transfer/start", startDataTransferMonitor)
	r.POST("/api/oci/:account/data-transfer/stop", stopDataTransferMonitor)
	r.GET("/api/oci/:account/data-transfer/status", getDataTransferMonitorStatus)

	// DNS management APIs
	r.GET("/api/dns/cloudflare", listCloudflareAccounts)
	r.POST("/api/dns/cloudflare", saveCloudflareAccounts)
	r.GET("/api/dns/bindings", listDNSBindings)
	r.POST("/api/dns/bindings", saveDNSBindings)
	r.GET("/api/dns/raw", getDNSConfigRaw)
	r.POST("/api/dns/delete-binding", deleteDNSBinding)
	r.GET("/api/vm/:provider/:account/:name/dns", getVMDNSBindings)
	r.POST("/api/vm/:provider/:account/:name/dns", saveVMDNSBindings)

	authService.RegisterPages(r)

	// Initialize data transfer monitors after all routes are registered
	initDTMonitors()

	_ = r.Run(":3000")
}

func listAccounts(c *gin.Context) {
	c.JSON(http.StatusOK, cloudAccountsSnapshot())
}

func listVMs(c *gin.Context) {
	provider := c.Query("provider")
	account := c.Query("account")
	if provider == "" || account == "" {
		c.JSON(http.StatusOK, []map[string]interface{}{})
		return
	}

	service, cloudflare, ok := serviceSnapshot(provider, account)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("provider/account %s/%s not found", provider, account)})
		return
	}

	vms, err := service.ListVMs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, vm := range vms {
		if id, ok := vm["id"].(string); ok {
			vm["dnsEnabled"] = cloudflare.HasBinding(provider, account, id)
		}
	}
	c.JSON(http.StatusOK, vms)
}

func getVM(c *gin.Context) {
	name := c.Param("name")
	service, _, ok := getCloudService(c)
	if !ok {
		return
	}
	vm, err := service.GetVM(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, vm)
}

func startVM(c *gin.Context) {
	name := c.Param("name")
	service, _, ok := getCloudService(c)
	if !ok {
		return
	}
	if err := service.StartVM(name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("starting VM: %s", name)})
}

func stopVM(c *gin.Context) {
	name := c.Param("name")
	service, _, ok := getCloudService(c)
	if !ok {
		return
	}
	if err := service.StopVM(name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("stopping VM: %s", name)})
}

func restartVM(c *gin.Context) {
	name := c.Param("name")
	service, _, ok := getCloudService(c)
	if !ok {
		return
	}
	if err := service.RestartVM(name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("restarting VM: %s", name)})
}

func changeIP(c *gin.Context) {
	provider := c.Param("provider")
	account := c.Param("account")
	name := c.Param("name")
	updateDNSAfterChange := c.DefaultQuery("update_dns", "false") == "true"
	service, cloudflare, ok := getCloudService(c)
	if !ok {
		return
	}

	result, err := service.ChangeIP(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if updateDNSAfterChange && result.NewIPAddress != "" && cloudflare.HasBinding(provider, account, name) {
		result.Logs = append(result.Logs, cloudflare.UpdateForVM(provider, account, name, result.NewIPAddress)...)
	}
	c.JSON(http.StatusOK, result)
}

func updateDNS(c *gin.Context) {
	provider := c.Param("provider")
	account := c.Param("account")
	name := c.Param("name")

	_, cloudflare, ok := serviceSnapshot(provider, account)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("provider/account %s/%s not found", provider, account)})
		return
	}
	if !cloudflare.HasBinding(provider, account, name) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no DNS binding configured for this VM"})
		return
	}

	service, _, ok := getCloudService(c)
	if !ok {
		return
	}

	vm, err := service.GetVM(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ip, ok := publicIPAddress(vm)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "VM has no public IP to bind"})
		return
	}

	logs := cloudflare.UpdateForVM(provider, account, name, ip)
	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"newIpAddress": ip,
		"logs":         logs,
	})
}

func refreshVM(c *gin.Context) {
	name := c.Param("name")
	service, cloudflare, ok := getCloudService(c)
	if !ok {
		return
	}
	vm, err := service.GetVM(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	vm["dnsEnabled"] = cloudflare.HasBinding(c.Param("provider"), c.Param("account"), name)
	c.JSON(http.StatusOK, vm)
}

func publicIPAddress(vm map[string]interface{}) (string, bool) {
	publicIP, ok := vm["publicIP"].(map[string]interface{})
	if !ok {
		return "", false
	}
	ip, ok := publicIP["ipAddress"].(string)
	if !ok || ip == "" || ip == "N/A" || ip == "unassigned" || ip == "未分配" {
		return "", false
	}
	return ip, true
}

func getCloudService(c *gin.Context) (CloudService, *CloudflareService, bool) {
	provider := c.Param("provider")
	if provider == "" {
		provider = "azure"
	}
	account := c.Param("account")
	if account == "" {
		account = c.Query("account")
	}
	if account == "" {
		service, cloudflare, ok := serviceSnapshot(provider, "")
		return service, cloudflare, ok
	}

	service, cloudflare, ok := serviceSnapshot(provider, account)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("provider/account %s/%s not found", provider, account)})
		return nil, nil, false
	}
	return service, cloudflare, true
}

func updateUpdateSettings(c *gin.Context) {
	var payload struct {
		DownloadProxy string `json:"downloadProxy"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	configPath := currentConfigPath()
	updateCfg := UpdateConfig{
		DownloadProxy: strings.TrimSpace(payload.DownloadProxy),
	}

	if err := SaveUpdateConfig(configPath, updateCfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := ReloadRuntimeConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("saved but reload failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "更新配置已保存，并已自动重载生效。",
	})
}

func serviceKey(provider, account string) string {
	return provider + "/" + account
}
