package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type OCISecurityList struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	IngressRules []OCISecurityRuleInput `json:"ingressRules"`
	EgressRules  []OCISecurityRuleInput `json:"egressRules"`
}

type OCISecurityRuleInput struct {
	ID            string `json:"id,omitempty"`
	Direction     string `json:"direction,omitempty"`
	Protocol      string `json:"protocol"`
	Source        string `json:"source,omitempty"`
	SourceType    string `json:"sourceType,omitempty"`
	Destination   string `json:"destination,omitempty"`
	DestType      string `json:"destinationType,omitempty"`
	MinPort       *int   `json:"minPort,omitempty"`
	MaxPort       *int   `json:"maxPort,omitempty"`
	ICMPType      *int   `json:"icmpType,omitempty"`
	ICMPCode      *int   `json:"icmpCode,omitempty"`
	Description   string `json:"description,omitempty"`
	IsStateless   bool   `json:"isStateless"`
	IsDestination bool   `json:"-"`
}

func listOCISecurityLists(c *gin.Context) {
	instanceID := c.Param("name")
	oci, ok := getOCIService(c)
	if !ok {
		return
	}

	lists, err := oci.ListSecurityLists(instanceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"securityLists": lists})
}

func saveOCISecurityListRules(c *gin.Context) {
	instanceID := c.Param("name")
	listID := c.Param("listID")
	oci, ok := getOCIService(c)
	if !ok {
		return
	}

	var payload struct {
		IngressRules []OCISecurityRuleInput `json:"ingressRules"`
		EgressRules  []OCISecurityRuleInput `json:"egressRules"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := oci.SaveSecurityListRules(instanceID, listID, payload.IngressRules, payload.EgressRules); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	lists, err := oci.ListSecurityLists(instanceID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "securityLists": lists})
}

func getOCIService(c *gin.Context) (*OCIService, bool) {
	provider := strings.ToLower(c.Param("provider"))
	if provider != "oci" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "this operation is only supported for OCI accounts"})
		return nil, false
	}

	account := c.Param("account")
	service, _, ok := serviceSnapshot(provider, account)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("provider/account %s/%s not found", provider, account)})
		return nil, false
	}

	oci, ok := service.(*OCIService)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "selected service is not an OCI account"})
		return nil, false
	}
	return oci, true
}

func (o *OCIService) ListSecurityLists(instanceID string) ([]OCISecurityList, error) {
	_, vnic, err := o.primaryPrivateIP(instanceID)
	if err != nil {
		return nil, err
	}

	securityListIDs, err := o.securityListIDsForVNIC(vnic)
	if err != nil {
		return nil, err
	}
	if len(securityListIDs) == 0 {
		return []OCISecurityList{}, nil
	}

	lists := make([]OCISecurityList, 0, len(securityListIDs))
	for _, listID := range securityListIDs {
		securityList, err := o.rawSecurityList(listID)
		if err != nil {
			return nil, err
		}

		lists = append(lists, OCISecurityList{
			ID:           listID,
			Name:         valueOrDefault(stringValue(securityList["displayName"]), listID),
			IngressRules: normalizeOCISecurityRules(securityList["ingressSecurityRules"], false),
			EgressRules:  normalizeOCISecurityRules(securityList["egressSecurityRules"], true),
		})
	}
	return lists, nil
}

func (o *OCIService) SaveSecurityListRules(instanceID, listID string, ingressRules, egressRules []OCISecurityRuleInput) error {
	if listID == "" {
		return fmt.Errorf("missing OCI security list id")
	}

	_, vnic, err := o.primaryPrivateIP(instanceID)
	if err != nil {
		return err
	}
	securityListIDs, err := o.securityListIDsForVNIC(vnic)
	if err != nil {
		return err
	}
	if !containsString(securityListIDs, listID) {
		return fmt.Errorf("security list %s is not attached to instance subnet %s", listID, instanceID)
	}

	rawList, err := o.rawSecurityList(listID)
	if err != nil {
		return err
	}

	ingressPayload, err := securityRulesPayload(ingressRules, false)
	if err != nil {
		return err
	}
	egressPayload, err := securityRulesPayload(egressRules, true)
	if err != nil {
		return err
	}

	body := map[string]interface{}{
		"definedTags":          valueOrEmptyMap(rawList["definedTags"]),
		"freeformTags":         valueOrEmptyMap(rawList["freeformTags"]),
		"ingressSecurityRules": ingressPayload,
		"egressSecurityRules":  egressPayload,
	}
	if displayName := stringValue(rawList["displayName"]); displayName != "" {
		body["displayName"] = displayName
	}

	return o.requestJSON("PUT", "/securityLists/"+url.PathEscape(listID), nil, body, nil, false)
}

func (o *OCIService) securityListIDsForVNIC(vnic map[string]interface{}) ([]string, error) {
	subnetID := stringValue(vnic["subnetId"])
	if subnetID == "" {
		return nil, fmt.Errorf("OCI VNIC has no subnet id")
	}

	var subnet map[string]interface{}
	if err := o.requestJSON("GET", "/subnets/"+url.PathEscape(subnetID), nil, nil, &subnet, false); err != nil {
		return nil, err
	}
	return stringSliceValue(subnet["securityListIds"]), nil
}

func (o *OCIService) rawSecurityList(listID string) (map[string]interface{}, error) {
	var securityList map[string]interface{}
	err := o.requestJSON("GET", "/securityLists/"+url.PathEscape(listID), nil, nil, &securityList, false)
	return securityList, err
}

func normalizeOCISecurityRules(value interface{}, isEgress bool) []OCISecurityRuleInput {
	rawRules, ok := value.([]interface{})
	if !ok {
		return []OCISecurityRuleInput{}
	}

	rules := make([]OCISecurityRuleInput, 0, len(rawRules))
	for _, rawRule := range rawRules {
		ruleMap, ok := rawRule.(map[string]interface{})
		if !ok {
			continue
		}
		rules = append(rules, normalizeOCISecurityRule(ruleMap, isEgress))
	}
	return rules
}

func normalizeOCISecurityRule(rule map[string]interface{}, isEgress bool) OCISecurityRuleInput {
	normalized := OCISecurityRuleInput{
		Protocol:      valueOrDefault(stringValue(rule["protocol"]), "all"),
		Description:   stringValue(rule["description"]),
		IsStateless:   boolInterfaceValue(rule["isStateless"]),
		IsDestination: isEgress,
	}
	if isEgress {
		normalized.Destination = stringValue(rule["destination"])
		normalized.DestType = valueOrDefault(stringValue(rule["destinationType"]), "CIDR_BLOCK")
	} else {
		normalized.Source = stringValue(rule["source"])
		normalized.SourceType = valueOrDefault(stringValue(rule["sourceType"]), "CIDR_BLOCK")
	}

	if min, max, ok := portRangeFromOptions(rule["tcpOptions"]); ok {
		normalized.MinPort = &min
		normalized.MaxPort = &max
	} else if min, max, ok := portRangeFromOptions(rule["udpOptions"]); ok {
		normalized.MinPort = &min
		normalized.MaxPort = &max
	}
	if icmpType, icmpCode, ok := icmpOptionsFromOptions(rule["icmpOptions"]); ok {
		normalized.ICMPType = &icmpType
		if icmpCode != nil {
			normalized.ICMPCode = icmpCode
		}
	}

	return normalized
}

func securityRulesPayload(rules []OCISecurityRuleInput, isEgress bool) ([]map[string]interface{}, error) {
	payload := make([]map[string]interface{}, 0, len(rules))
	for _, rule := range rules {
		rule.IsDestination = isEgress
		normalized, err := securityRulePayload(rule)
		if err != nil {
			return nil, err
		}
		payload = append(payload, normalized)
	}
	return payload, nil
}

func securityRulePayload(rule OCISecurityRuleInput) (map[string]interface{}, error) {
	protocol := valueOrDefault(strings.TrimSpace(strings.ToLower(rule.Protocol)), "all")
	if protocol == "tcp" {
		protocol = "6"
	}
	if protocol == "udp" {
		protocol = "17"
	}
	if protocol == "icmp" {
		protocol = "1"
	}
	if protocol != "all" {
		protocolNumber, err := strconv.Atoi(protocol)
		if err != nil || protocolNumber < 0 || protocolNumber > 255 {
			return nil, fmt.Errorf("unsupported protocol %q", rule.Protocol)
		}
	}

	endpointKey := "source"
	typeKey := "sourceType"
	endpointValue := strings.TrimSpace(rule.Source)
	typeValue := valueOrDefault(strings.TrimSpace(rule.SourceType), "CIDR_BLOCK")
	if rule.IsDestination {
		endpointKey = "destination"
		typeKey = "destinationType"
		endpointValue = strings.TrimSpace(rule.Destination)
		typeValue = valueOrDefault(strings.TrimSpace(rule.DestType), "CIDR_BLOCK")
	}
	if endpointValue == "" {
		return nil, fmt.Errorf("%s CIDR is required", endpointKey)
	}
	if typeValue != "CIDR_BLOCK" && typeValue != "SERVICE_CIDR_BLOCK" && typeValue != "NETWORK_SECURITY_GROUP" {
		return nil, fmt.Errorf("unsupported %s %q", typeKey, typeValue)
	}

	payload := map[string]interface{}{
		"protocol":    protocol,
		endpointKey:   endpointValue,
		typeKey:       typeValue,
		"isStateless": rule.IsStateless,
	}
	if description := strings.TrimSpace(rule.Description); description != "" {
		payload["description"] = description
	}

	if protocol == "6" || protocol == "17" {
		portOptions, err := portOptionsPayload(rule.MinPort, rule.MaxPort)
		if err != nil {
			return nil, err
		}
		if portOptions != nil {
			if protocol == "6" {
				payload["tcpOptions"] = portOptions
			} else {
				payload["udpOptions"] = portOptions
			}
		}
	}
	if protocol == "1" {
		icmpOptions, err := icmpOptionsPayload(rule.ICMPType, rule.ICMPCode)
		if err != nil {
			return nil, err
		}
		if icmpOptions != nil {
			payload["icmpOptions"] = icmpOptions
		}
	}

	return payload, nil
}

func portOptionsPayload(minPort, maxPort *int) (map[string]interface{}, error) {
	if minPort == nil && maxPort == nil {
		return nil, nil
	}

	min := 0
	max := 0
	if minPort != nil {
		min = *minPort
	}
	if maxPort != nil {
		max = *maxPort
	} else {
		max = min
	}

	if minPort == nil {
		min = max
	}
	if min < 1 || min > 65535 || max < 1 || max > 65535 || min > max {
		return nil, fmt.Errorf("port range must be between 1 and 65535, with min <= max")
	}

	return map[string]interface{}{
		"destinationPortRange": map[string]interface{}{
			"min": min,
			"max": max,
		},
	}, nil
}

func portRangeFromOptions(value interface{}) (int, int, bool) {
	options, ok := value.(map[string]interface{})
	if !ok {
		return 0, 0, false
	}
	rangeValue, ok := options["destinationPortRange"].(map[string]interface{})
	if !ok {
		return 0, 0, false
	}
	min, minOK := intInterfaceValue(rangeValue["min"])
	max, maxOK := intInterfaceValue(rangeValue["max"])
	if !minOK && !maxOK {
		return 0, 0, false
	}
	if !minOK {
		min = max
	}
	if !maxOK {
		max = min
	}
	return min, max, true
}

func icmpOptionsPayload(icmpType, icmpCode *int) (map[string]interface{}, error) {
	if icmpType == nil && icmpCode == nil {
		return nil, nil
	}
	if icmpType == nil {
		return nil, fmt.Errorf("ICMP type is required when ICMP code is set")
	}
	if *icmpType < 0 || *icmpType > 255 {
		return nil, fmt.Errorf("ICMP type must be between 0 and 255")
	}

	payload := map[string]interface{}{
		"type": *icmpType,
	}
	if icmpCode != nil {
		if *icmpCode < 0 || *icmpCode > 255 {
			return nil, fmt.Errorf("ICMP code must be between 0 and 255")
		}
		payload["code"] = *icmpCode
	}
	return payload, nil
}

func icmpOptionsFromOptions(value interface{}) (int, *int, bool) {
	options, ok := value.(map[string]interface{})
	if !ok {
		return 0, nil, false
	}
	icmpType, ok := intInterfaceValue(options["type"])
	if !ok {
		return 0, nil, false
	}
	var icmpCode *int
	if code, ok := intInterfaceValue(options["code"]); ok {
		icmpCode = &code
	}
	return icmpType, icmpCode, true
}

func stringSliceValue(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := stringValue(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func intInterfaceValue(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	default:
		return 0, false
	}
}

func boolInterfaceValue(value interface{}) bool {
	v, ok := value.(bool)
	return ok && v
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func valueOrEmptyMap(value interface{}) interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return value
}
