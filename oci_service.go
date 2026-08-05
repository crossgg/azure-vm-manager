package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type OCIService struct {
	account OCIConfig
	client  *http.Client
	key     *rsa.PrivateKey
}

func NewOCIService(account OCIConfig) *OCIService {
	return &OCIService{
		account: account,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (o *OCIService) ListVMs() ([]map[string]interface{}, error) {
	var instances []map[string]interface{}
	err := o.requestJSON("GET", "/instances", url.Values{
		"compartmentId": {o.account.CompartmentID},
	}, nil, &instances, false)
	if err != nil {
		return nil, err
	}

	vms := make([]map[string]interface{}, 0, len(instances))
	for _, instance := range instances {
		vms = append(vms, o.normalizeVM(instance))
	}
	return vms, nil
}

func (o *OCIService) GetVM(id string) (map[string]interface{}, error) {
	var instance map[string]interface{}
	if err := o.requestJSON("GET", "/instances/"+url.PathEscape(id), nil, nil, &instance, false); err != nil {
		return nil, err
	}
	return o.normalizeVM(instance), nil
}

func (o *OCIService) StartVM(id string) error {
	return o.instanceAction(id, "START")
}

func (o *OCIService) StopVM(id string) error {
	return o.instanceAction(id, "STOP")
}

func (o *OCIService) RestartVM(id string) error {
	return o.instanceAction(id, "RESET")
}

func (o *OCIService) ChangeIP(id string) (*ChangeIPResult, error) {
	privateIP, vnic, err := o.primaryPrivateIP(id)
	if err != nil {
		return nil, err
	}

	privateIPID := stringValue(privateIP["id"])
	if privateIPID == "" {
		return nil, fmt.Errorf("OCI instance %s has no primary private IP id", id)
	}

	logs := []string{
		fmt.Sprintf("[1/4] 已定位主 VNIC: %s", valueOrDefault(stringValue(vnic["displayName"]), stringValue(vnic["id"]))),
		fmt.Sprintf("[1/4] 已定位主私有 IP: %s", valueOrDefault(stringValue(privateIP["ipAddress"]), privateIPID)),
	}

	publicIP, err := o.publicIPByPrivateIP(privateIPID)
	if err != nil {
		return nil, err
	}

	if publicIP != nil && stringValue(publicIP["id"]) != "" {
		oldPublicIPID := stringValue(publicIP["id"])
		oldPublicIP := stringValue(publicIP["ipAddress"])
		lifetime := stringValue(publicIP["lifetime"])
		logs = append(logs, fmt.Sprintf("[2/4] 正在更新为没有公共 IP: %s...", oldPublicIP))

		if strings.EqualFold(lifetime, "RESERVED") {
			if err := o.unassignReservedPublicIP(oldPublicIPID); err != nil {
				return nil, fmt.Errorf("解绑旧保留公网 IP 失败: %w", err)
			}
			logs = append(logs, "[2/4] 旧保留公网 IP 已解绑")
		} else {
			if err := o.deletePublicIP(oldPublicIPID); err != nil {
				return nil, fmt.Errorf("删除旧临时公网 IP 失败: %w", err)
			}
			logs = append(logs, "[2/4] 旧临时公网 IP 已删除")
		}

		if err := o.waitNoPublicIP(privateIPID); err != nil {
			return nil, err
		}
		logs = append(logs, "[3/4] 已确认当前状态为没有公共 IP")
	} else {
		logs = append(logs, "[2/4] 当前已经是没有公共 IP")
		logs = append(logs, "[3/4] 跳过等待")
	}

	body := map[string]interface{}{
		"compartmentId": o.account.CompartmentID,
		"lifetime":      "EPHEMERAL",
		"privateIpId":   privateIPID,
		"displayName":   "ephemeral-ip-" + time.Now().Format("20060102150405"),
	}
	var created map[string]interface{}
	logs = append(logs, "[4/4] 正在更新为临时公共 IP...")
	if err := o.requestJSON("POST", "/publicIps", nil, body, &created, false); err != nil {
		return nil, fmt.Errorf("分配新临时公网 IP 失败: %w", err)
	}

	newIP := stringValue(created["ipAddress"])
	logs = append(logs, fmt.Sprintf("[4/4] 新临时公共 IP 分配成功: %s", valueOrDefault(newIP, "pending")))

	return &ChangeIPResult{
		Success:      true,
		Message:      "IP更换成功",
		NewIPAddress: newIP,
		Logs:         logs,
	}, nil
}

func (o *OCIService) deletePublicIP(publicIPID string) error {
	return o.requestJSON("DELETE", "/publicIps/"+url.PathEscape(publicIPID), nil, nil, nil, false)
}

func (o *OCIService) unassignReservedPublicIP(publicIPID string) error {
	return o.requestJSON("PUT", "/publicIps/"+url.PathEscape(publicIPID), nil, map[string]interface{}{
		"privateIpId": nil,
	}, nil, false)
}

func (o *OCIService) waitNoPublicIP(privateIPID string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		publicIP, err := o.publicIPByPrivateIP(privateIPID)
		if err != nil {
			return err
		}
		if publicIP == nil || stringValue(publicIP["id"]) == "" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("等待 OCI 私有 IP 解除公网 IP 绑定超时")
}

func (o *OCIService) instanceAction(id, action string) error {
	return o.requestJSON("POST", "/instances/"+url.PathEscape(id), url.Values{"action": {action}}, nil, nil, false)
}

func (o *OCIService) normalizeVM(instance map[string]interface{}) map[string]interface{} {
	id := stringValue(instance["id"])
	name := valueOrDefault(stringValue(instance["displayName"]), id)
	shape := stringValue(instance["shape"])
	shapeConfig, _ := instance["shapeConfig"].(map[string]interface{})
	sourceDetails, _ := instance["sourceDetails"].(map[string]interface{})
	region := o.account.Region
	privateIP := "未分配"
	publicIP := map[string]interface{}{"ipAddress": "未分配", "name": "N/A"}

	if primaryIP, vnic, err := o.primaryPrivateIP(id); err == nil {
		privateIP = valueOrDefault(stringValue(primaryIP["ipAddress"]), valueOrDefault(stringValue(vnic["privateIp"]), "未分配"))
		if pub, err := o.publicIPByPrivateIP(stringValue(primaryIP["id"])); err == nil && pub != nil {
			publicIP["ipAddress"] = valueOrDefault(stringValue(pub["ipAddress"]), "未分配")
			publicIP["name"] = valueOrDefault(stringValue(pub["displayName"]), valueOrDefault(stringValue(pub["id"]), "N/A"))
		} else if stringValue(vnic["publicIp"]) != "" {
			publicIP["ipAddress"] = stringValue(vnic["publicIp"])
			publicIP["name"] = "ephemeral"
		}
	}

	return map[string]interface{}{
		"provider":           "oci",
		"accountId":          o.account.Name,
		"group":              o.account.Group,
		"id":                 id,
		"name":               name,
		"location":           region,
		"status":             ociStatusText(stringValue(instance["lifecycleState"])),
		"vmSize":             shape,
		"ocpus":              float64InterfaceValue(shapeConfig["ocpus"]),
		"memoryInGBs":        float64InterfaceValue(shapeConfig["memoryInGBs"]),
		"availabilityDomain": stringValue(instance["availabilityDomain"]),
		"imageId":            stringValue(sourceDetails["imageId"]),
		"privateIP":          privateIP,
		"publicIP":           publicIP,
		"resourceGroup":      o.account.CompartmentID,
	}
}

func (o *OCIService) primaryPrivateIP(instanceID string) (map[string]interface{}, map[string]interface{}, error) {
	var attachments []map[string]interface{}
	err := o.requestJSON("GET", "/vnicAttachments", url.Values{
		"compartmentId": {o.account.CompartmentID},
		"instanceId":    {instanceID},
	}, nil, &attachments, false)
	if err != nil {
		return nil, nil, err
	}

	for _, attachment := range attachments {
		if state := stringValue(attachment["lifecycleState"]); state != "" && state != "ATTACHED" {
			continue
		}
		vnicID := stringValue(attachment["vnicId"])
		if vnicID == "" {
			continue
		}

		var vnic map[string]interface{}
		if err := o.requestJSON("GET", "/vnics/"+url.PathEscape(vnicID), nil, nil, &vnic, false); err != nil {
			return nil, nil, err
		}

		var privateIPs []map[string]interface{}
		if err := o.requestJSON("GET", "/privateIps", url.Values{"vnicId": {vnicID}}, nil, &privateIPs, false); err != nil {
			return nil, nil, err
		}
		if len(privateIPs) == 0 {
			continue
		}
		for _, privateIP := range privateIPs {
			if primary, _ := privateIP["isPrimary"].(bool); primary {
				return privateIP, vnic, nil
			}
		}
		return privateIPs[0], vnic, nil
	}

	return nil, nil, fmt.Errorf("no attached VNIC found for OCI instance %s", instanceID)
}

func (o *OCIService) publicIPByPrivateIP(privateIPID string) (map[string]interface{}, error) {
	var publicIP map[string]interface{}
	err := o.requestJSON("POST", "/publicIps/actions/getByPrivateIpId", nil, map[string]interface{}{
		"privateIpId": privateIPID,
	}, &publicIP, true)
	if err != nil {
		return nil, err
	}
	return publicIP, nil
}

func (o *OCIService) requestJSON(method, path string, query url.Values, body interface{}, out interface{}, allowNotFound bool) error {
	endpoint := o.endpoint(path, query)
	return o.doRequest(method, endpoint, body, out, allowNotFound)
}

// doRequest sends a signed request to the given full endpoint URL.
// This is the shared implementation used by both iaas and monitoring APIs.
func (o *OCIService) doRequest(method, endpoint string, body interface{}, out interface{}, allowNotFound bool) error {
	var payload []byte
	var reader io.Reader
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	} else if method == "POST" || method == "PUT" {
		payload = []byte{}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return err
	}
	if err := o.sign(req, payload); err != nil {
		return err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if allowNotFound && resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OCI API %s %s returned %d: %s", method, endpoint, resp.StatusCode, string(data))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (o *OCIService) endpoint(path string, query url.Values) string {
	u := url.URL{
		Scheme: "https",
		Host:   "iaas." + o.account.Region + ".oraclecloud.com",
		Path:   "/20160918" + path,
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func (o *OCIService) sign(req *http.Request, payload []byte) error {
	if o.key == nil {
		key, err := loadRSAPrivateKey(o.account.KeyFile)
		if err != nil {
			return err
		}
		o.key = key
	}

	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("Host", req.URL.Host)

	headers := []string{"(request-target)", "host", "date"}
	if req.Method == "POST" || req.Method == "PUT" {
		sum := sha256.Sum256(payload)
		req.Header.Set("x-content-sha256", base64.StdEncoding.EncodeToString(sum[:]))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		headers = append(headers, "x-content-sha256", "content-type", "content-length")
	}

	signingString := o.signingString(req, headers)
	sum := sha256.Sum256([]byte(signingString))
	sig, err := rsa.SignPKCS1v15(rand.Reader, o.key, crypto.SHA256, sum[:])
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf(
		`Signature version="1",keyId="%s/%s/%s",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		o.account.Tenancy,
		o.account.User,
		o.account.Fingerprint,
		strings.Join(headers, " "),
		base64.StdEncoding.EncodeToString(sig),
	))
	return nil
}

func (o *OCIService) signingString(req *http.Request, headers []string) string {
	lines := make([]string, 0, len(headers))
	for _, header := range headers {
		switch header {
		case "(request-target)":
			target := strings.ToLower(req.Method) + " " + req.URL.EscapedPath()
			if req.URL.RawQuery != "" {
				target += "?" + normalizeQuery(req.URL.RawQuery)
			}
			lines = append(lines, "(request-target): "+target)
		case "host":
			lines = append(lines, "host: "+req.URL.Host)
		default:
			lines = append(lines, header+": "+req.Header.Get(header))
		}
	}
	return strings.Join(lines, "\n")
}

func normalizeQuery(rawQuery string) string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0)
	for _, key := range keys {
		vals := values[key]
		sort.Strings(vals)
		for _, value := range vals {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func ociStatusText(status string) string {
	switch status {
	case "RUNNING":
		return "VM running"
	case "STOPPED", "TERMINATED", "TERMINATING":
		return "VM stopped"
	default:
		if status == "" {
			return "Unknown"
		}
		return status
	}
}
