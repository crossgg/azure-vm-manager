package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

type OCIInstanceEditPayload struct {
	DisplayName             string  `json:"displayName"`
	Shape                   string  `json:"shape"`
	OCPUs                   float64 `json:"ocpus"`
	MemoryInGBs             float64 `json:"memoryInGBs"`
	BaselineOCPUUtilization string  `json:"baselineOcpuUtilization"`
	AllowDowntime           bool    `json:"allowDowntime"`
}

type OCIInstanceEditOptions struct {
	Instance      OCIInstanceEditInstance `json:"instance"`
	Shapes        []OCIShapeOption        `json:"shapes"`
	SelectedShape *OCIShapeOption         `json:"selectedShape,omitempty"`
	Limits        OCIEditLimits           `json:"limits"`
	Warnings      []string                `json:"warnings,omitempty"`
}

type OCIInstanceEditInstance struct {
	ID                      string  `json:"id"`
	DisplayName             string  `json:"displayName"`
	Shape                   string  `json:"shape"`
	AvailabilityDomain      string  `json:"availabilityDomain"`
	ImageID                 string  `json:"imageId,omitempty"`
	LifecycleState          string  `json:"lifecycleState"`
	OCPUs                   float64 `json:"ocpus"`
	MemoryInGBs             float64 `json:"memoryInGBs"`
	BaselineOCPUUtilization string  `json:"baselineOcpuUtilization,omitempty"`
}

type OCIShapeOption struct {
	Name                      string                `json:"name"`
	Family                    string                `json:"family"`
	ProcessorDescription      string                `json:"processorDescription,omitempty"`
	IsFlexible                bool                  `json:"isFlexible"`
	OCPUs                     float64               `json:"ocpus"`
	MemoryInGBs               float64               `json:"memoryInGBs"`
	NetworkingBandwidthInGbps float64               `json:"networkingBandwidthInGbps,omitempty"`
	MaxVnicAttachments        int                   `json:"maxVnicAttachments,omitempty"`
	QuotaNames                []string              `json:"quotaNames,omitempty"`
	OCPUOptions               OCIShapeOCPUOptions   `json:"ocpuOptions"`
	MemoryOptions             OCIShapeMemoryOptions `json:"memoryOptions"`
	ResizeCompatible          bool                  `json:"resizeCompatible"`
}

type OCIShapeOCPUOptions struct {
	Min            float64 `json:"min,omitempty"`
	Max            float64 `json:"max,omitempty"`
	MaxPerNumaNode float64 `json:"maxPerNumaNode,omitempty"`
}

type OCIShapeMemoryOptions struct {
	MinInGBs            float64 `json:"minInGBs,omitempty"`
	MaxInGBs            float64 `json:"maxInGBs,omitempty"`
	DefaultPerOCPUInGBs float64 `json:"defaultPerOcpuInGBs,omitempty"`
	MinPerOCPUInGBs     float64 `json:"minPerOcpuInGBs,omitempty"`
	MaxPerOCPUInGBs     float64 `json:"maxPerOcpuInGBs,omitempty"`
	DefaultMemoryInGBs  float64 `json:"defaultMemoryInGBs,omitempty"`
	DefaultOCPUs        float64 `json:"defaultOcpus,omitempty"`
}

type OCIEditLimits struct {
	OCPU   OCIResourceLimitInfo `json:"ocpu"`
	Memory OCIResourceLimitInfo `json:"memory"`
}

type OCIResourceLimitInfo struct {
	ResourceName    string  `json:"resourceName,omitempty"`
	ShapeMin        float64 `json:"shapeMin"`
	ShapeMax        float64 `json:"shapeMax"`
	Available       float64 `json:"available,omitempty"`
	Used            float64 `json:"used,omitempty"`
	Reusable        float64 `json:"reusable,omitempty"`
	EffectiveMax    float64 `json:"effectiveMax"`
	HasAvailability bool    `json:"hasAvailability"`
	Source          string  `json:"source"`
	Error           string  `json:"error,omitempty"`
}

type ociResourceAvailability struct {
	Available           float64
	Used                float64
	EffectiveQuotaValue float64
}

func getOCIInstanceEditOptions(c *gin.Context) {
	instanceID := c.Param("name")
	oci, ok := getOCIService(c)
	if !ok {
		return
	}

	options, err := oci.InstanceEditOptions(instanceID, c.Query("shape"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, options)
}

func updateOCIInstance(c *gin.Context) {
	instanceID := c.Param("name")
	oci, ok := getOCIService(c)
	if !ok {
		return
	}

	var payload OCIInstanceEditPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	vm, err := oci.UpdateInstanceConfig(instanceID, payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "OCI 实例编辑请求已提交。",
		"vm":      vm,
	})
}

func (o *OCIService) InstanceEditOptions(instanceID, selectedShapeName string) (*OCIInstanceEditOptions, error) {
	rawInstance, err := o.rawInstance(instanceID)
	if err != nil {
		return nil, err
	}

	instance := normalizeOCIInstanceEdit(rawInstance)
	if selectedShapeName == "" {
		selectedShapeName = instance.Shape
	}

	shapes, err := o.ListEditableShapes(instance)
	if err != nil {
		return nil, err
	}

	var currentShape *OCIShapeOption
	var selectedShape *OCIShapeOption
	for i := range shapes {
		if shapes[i].Name == instance.Shape {
			currentShape = &shapes[i]
		}
		if shapes[i].Name == selectedShapeName {
			selectedShape = &shapes[i]
		}
	}
	if selectedShape == nil && selectedShapeName != "" {
		return nil, fmt.Errorf("OCI shape %s is not available for instance %s", selectedShapeName, instanceID)
	}
	if selectedShape == nil && len(shapes) > 0 {
		selectedShape = &shapes[0]
	}
	if instance.OCPUs <= 0 && currentShape != nil {
		instance.OCPUs = currentShape.defaultOCPUs()
	}
	if instance.MemoryInGBs <= 0 && currentShape != nil {
		instance.MemoryInGBs = currentShape.defaultMemoryInGBs(instance.OCPUs)
	}

	options := &OCIInstanceEditOptions{
		Instance:      instance,
		Shapes:        shapes,
		SelectedShape: selectedShape,
	}
	if selectedShape != nil {
		options.Limits = o.shapeEditLimits(*selectedShape, instance, currentShape)
		options.Warnings = o.limitWarnings(options.Limits)
	}
	return options, nil
}

func (o *OCIService) UpdateInstanceConfig(instanceID string, payload OCIInstanceEditPayload) (map[string]interface{}, error) {
	shapeName := strings.TrimSpace(payload.Shape)
	if shapeName == "" {
		return nil, fmt.Errorf("missing OCI shape")
	}

	options, err := o.InstanceEditOptions(instanceID, shapeName)
	if err != nil {
		return nil, err
	}
	if options.SelectedShape == nil {
		return nil, fmt.Errorf("OCI shape %s is not available", shapeName)
	}
	shape := *options.SelectedShape

	displayName := strings.TrimSpace(payload.DisplayName)
	if displayName == "" {
		return nil, fmt.Errorf("instance display name is required")
	}

	body := map[string]interface{}{
		"displayName": displayName,
		"shape":       shape.Name,
	}
	if payload.AllowDowntime {
		body["updateOperationConstraint"] = "ALLOW_DOWNTIME"
	} else {
		body["updateOperationConstraint"] = "AVOID_DOWNTIME"
	}

	if shape.IsFlexible {
		ocpus := payload.OCPUs
		if ocpus <= 0 {
			ocpus = options.Instance.OCPUs
		}
		if ocpus <= 0 {
			ocpus = shape.defaultOCPUs()
		}

		memory := payload.MemoryInGBs
		if memory <= 0 {
			memory = options.Instance.MemoryInGBs
		}
		if memory <= 0 {
			memory = shape.defaultMemoryInGBs(ocpus)
		}

		if err := validateOCIShapeConfig(shape, options.Limits, ocpus, memory); err != nil {
			return nil, err
		}

		shapeConfig := map[string]interface{}{
			"ocpus":       ocpus,
			"memoryInGBs": memory,
		}
		if baseline := strings.TrimSpace(payload.BaselineOCPUUtilization); baseline != "" {
			shapeConfig["baselineOcpuUtilization"] = baseline
		}
		body["shapeConfig"] = shapeConfig
	}

	if err := o.requestJSON("PUT", "/instances/"+url.PathEscape(instanceID), nil, body, nil, false); err != nil {
		return nil, err
	}
	return o.GetVM(instanceID)
}

func (o *OCIService) rawInstance(instanceID string) (map[string]interface{}, error) {
	var instance map[string]interface{}
	err := o.requestJSON("GET", "/instances/"+url.PathEscape(instanceID), nil, nil, &instance, false)
	return instance, err
}

func (o *OCIService) ListEditableShapes(instance OCIInstanceEditInstance) ([]OCIShapeOption, error) {
	query := url.Values{
		"compartmentId": {o.account.CompartmentID},
		"limit":         {"1000"},
	}
	if instance.AvailabilityDomain != "" {
		query.Set("availabilityDomain", instance.AvailabilityDomain)
	}
	if instance.ImageID != "" {
		query.Set("imageId", instance.ImageID)
	}

	var rawShapes []map[string]interface{}
	if err := o.requestJSON("GET", "/shapes", query, nil, &rawShapes, false); err != nil {
		return nil, err
	}

	shapes := make([]OCIShapeOption, 0, len(rawShapes))
	seen := make(map[string]struct{}, len(rawShapes))
	for _, rawShape := range rawShapes {
		shape := normalizeOCIShapeOption(rawShape, instance.Shape)
		if shape.Name == "" {
			continue
		}
		if _, ok := seen[shape.Name]; ok {
			continue
		}
		seen[shape.Name] = struct{}{}
		shapes = append(shapes, shape)
	}
	sort.Slice(shapes, func(i, j int) bool {
		if shapes[i].Family != shapes[j].Family {
			return shapeFamilySortKey(shapes[i].Family) < shapeFamilySortKey(shapes[j].Family)
		}
		return shapes[i].Name < shapes[j].Name
	})
	return shapes, nil
}

func normalizeOCIInstanceEdit(instance map[string]interface{}) OCIInstanceEditInstance {
	shapeConfig, _ := instance["shapeConfig"].(map[string]interface{})
	sourceDetails, _ := instance["sourceDetails"].(map[string]interface{})

	return OCIInstanceEditInstance{
		ID:                      stringValue(instance["id"]),
		DisplayName:             valueOrDefault(stringValue(instance["displayName"]), stringValue(instance["id"])),
		Shape:                   stringValue(instance["shape"]),
		AvailabilityDomain:      stringValue(instance["availabilityDomain"]),
		ImageID:                 stringValue(sourceDetails["imageId"]),
		LifecycleState:          stringValue(instance["lifecycleState"]),
		OCPUs:                   float64InterfaceValue(shapeConfig["ocpus"]),
		MemoryInGBs:             float64InterfaceValue(shapeConfig["memoryInGBs"]),
		BaselineOCPUUtilization: stringValue(shapeConfig["baselineOcpuUtilization"]),
	}
}

func normalizeOCIShapeOption(raw map[string]interface{}, currentShape string) OCIShapeOption {
	shapeConfigOptions, _ := raw["shapeConfigOptions"].(map[string]interface{})

	shape := OCIShapeOption{
		Name:                      stringValue(raw["shape"]),
		ProcessorDescription:      stringValue(raw["processorDescription"]),
		OCPUs:                     float64InterfaceValue(raw["ocpus"]),
		MemoryInGBs:               float64InterfaceValue(raw["memoryInGBs"]),
		NetworkingBandwidthInGbps: float64InterfaceValue(raw["networkingBandwidthInGbps"]),
		MaxVnicAttachments:        intFromFloat(float64InterfaceValue(raw["maxVnicAttachments"])),
		QuotaNames:                stringSliceValue(raw["quotaNames"]),
		ResizeCompatible:          true,
	}
	if shape.Name == currentShape {
		shape.ResizeCompatible = true
	}

	shape.OCPUOptions = parseOCIShapeOCPUOptions(firstMapValue(shapeConfigOptions, raw, "ocpuOptions"))
	shape.MemoryOptions = parseOCIShapeMemoryOptions(firstMapValue(shapeConfigOptions, raw, "memoryOptions"))
	shape.IsFlexible = strings.Contains(strings.ToLower(shape.Name), ".flex") ||
		(shape.OCPUOptions.Max > 0 && shape.OCPUOptions.Max != shape.OCPUOptions.Min) ||
		(shape.MemoryOptions.MaxInGBs > 0 && shape.MemoryOptions.MaxInGBs != shape.MemoryOptions.MinInGBs)
	shape.Family = ociShapeFamily(shape)

	return shape
}

func parseOCIShapeOCPUOptions(value interface{}) OCIShapeOCPUOptions {
	options, _ := value.(map[string]interface{})
	return OCIShapeOCPUOptions{
		Min:            float64InterfaceValue(options["min"]),
		Max:            float64InterfaceValue(options["max"]),
		MaxPerNumaNode: float64InterfaceValue(options["maxPerNumaNode"]),
	}
}

func parseOCIShapeMemoryOptions(value interface{}) OCIShapeMemoryOptions {
	options, _ := value.(map[string]interface{})
	return OCIShapeMemoryOptions{
		MinInGBs:            float64InterfaceValue(options["minInGBs"]),
		MaxInGBs:            float64InterfaceValue(options["maxInGBs"]),
		DefaultPerOCPUInGBs: float64InterfaceValue(options["defaultPerOcpuInGBs"]),
		MinPerOCPUInGBs:     float64InterfaceValue(options["minPerOcpuInGBs"]),
		MaxPerOCPUInGBs:     float64InterfaceValue(options["maxPerOcpuInGBs"]),
		DefaultMemoryInGBs:  float64InterfaceValue(options["defaultMemoryInGBs"]),
		DefaultOCPUs:        float64InterfaceValue(options["defaultOcpus"]),
	}
}

func firstMapValue(primary map[string]interface{}, fallback map[string]interface{}, key string) interface{} {
	if primary != nil {
		if value, ok := primary[key]; ok {
			return value
		}
	}
	return fallback[key]
}

func (o *OCIService) shapeEditLimits(shape OCIShapeOption, instance OCIInstanceEditInstance, currentShape *OCIShapeOption) OCIEditLimits {
	return OCIEditLimits{
		OCPU:   o.resourceLimitForShape("ocpu", shape, instance, currentShape),
		Memory: o.resourceLimitForShape("memory", shape, instance, currentShape),
	}
}

func (o *OCIService) resourceLimitForShape(kind string, shape OCIShapeOption, instance OCIInstanceEditInstance, currentShape *OCIShapeOption) OCIResourceLimitInfo {
	minValue, maxValue := shape.ocpuRange()
	currentValue := instance.OCPUs
	if kind == "memory" {
		minValue, maxValue = shape.memoryRange()
		currentValue = instance.MemoryInGBs
	}

	info := OCIResourceLimitInfo{
		ResourceName: quotaNameForKind(shape.QuotaNames, kind),
		ShapeMin:     minValue,
		ShapeMax:     maxValue,
		EffectiveMax: maxValue,
		Source:       "shape",
	}
	if info.ResourceName == "" {
		return info
	}

	availability, err := o.computeResourceAvailability(info.ResourceName, instance.AvailabilityDomain)
	if err != nil {
		info.Error = err.Error()
		return info
	}

	info.HasAvailability = true
	info.Source = "availability"
	info.Available = availability.Available
	info.Used = availability.Used

	if currentShape != nil && quotaNameForKind(currentShape.QuotaNames, kind) == info.ResourceName {
		info.Reusable = currentValue
	}
	quotaMax := info.Available + info.Reusable
	info.EffectiveMax = math.Min(maxValue, quotaMax)
	return info
}

func (o *OCIService) computeResourceAvailability(limitName, availabilityDomain string) (ociResourceAvailability, error) {
	if limitName == "" {
		return ociResourceAvailability{}, fmt.Errorf("missing OCI limit name")
	}
	availability, err := o.computeResourceAvailabilityScoped(limitName, availabilityDomain)
	if err == nil || availabilityDomain == "" || !strings.Contains(err.Error(), "400") {
		return availability, err
	}
	return o.computeResourceAvailabilityScoped(limitName, "")
}

func (o *OCIService) computeResourceAvailabilityScoped(limitName, availabilityDomain string) (ociResourceAvailability, error) {
	query := url.Values{
		"compartmentId": {o.account.CompartmentID},
	}
	if availabilityDomain != "" {
		query.Set("availabilityDomain", availabilityDomain)
	}

	var raw map[string]interface{}
	path := "/services/compute/limits/" + url.PathEscape(limitName) + "/resourceAvailability"
	if err := o.limitsRequestJSON("GET", path, query, nil, &raw, false); err != nil {
		return ociResourceAvailability{}, err
	}

	available, ok := float64InterfaceValueOK(raw["fractionalAvailability"])
	if !ok {
		available, ok = float64InterfaceValueOK(raw["available"])
	}
	if !ok {
		return ociResourceAvailability{}, fmt.Errorf("OCI did not return availability for limit %s", limitName)
	}
	used, _ := float64InterfaceValueOK(raw["fractionalUsage"])
	if used == 0 {
		used = float64InterfaceValue(raw["used"])
	}

	return ociResourceAvailability{
		Available:           available,
		Used:                used,
		EffectiveQuotaValue: float64InterfaceValue(raw["effectiveQuotaValue"]),
	}, nil
}

func (o *OCIService) limitsRequestJSON(method, path string, query url.Values, body interface{}, out interface{}, allowNotFound bool) error {
	endpoint := o.limitsEndpoint(path, query)
	return o.doRequest(method, endpoint, body, out, allowNotFound)
}

func (o *OCIService) limitsEndpoint(path string, query url.Values) string {
	u := url.URL{
		Scheme: "https",
		Host:   "limits." + o.account.Region + ".oci.oraclecloud.com",
		Path:   "/20181025" + path,
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func validateOCIShapeConfig(shape OCIShapeOption, limits OCIEditLimits, ocpus, memoryInGBs float64) error {
	ocpuMin := limits.OCPU.ShapeMin
	ocpuMax := limits.OCPU.EffectiveMax
	if ocpuMax <= 0 {
		ocpuMax = limits.OCPU.ShapeMax
	}
	if ocpus < ocpuMin || ocpus > ocpuMax {
		return fmt.Errorf("OCPU 数应介于 %s 和 %s 之间", formatOCIAmount(ocpuMin), formatOCIAmount(ocpuMax))
	}

	memoryMin, memoryMax := memoryRangeForOCPUs(shape, limits.Memory, ocpus)
	if memoryInGBs < memoryMin || memoryInGBs > memoryMax {
		return fmt.Errorf("内存应介于 %s GB 和 %s GB 之间", formatOCIAmount(memoryMin), formatOCIAmount(memoryMax))
	}
	return nil
}

func memoryRangeForOCPUs(shape OCIShapeOption, limit OCIResourceLimitInfo, ocpus float64) (float64, float64) {
	minValue := limit.ShapeMin
	maxValue := limit.EffectiveMax
	if maxValue <= 0 {
		maxValue = limit.ShapeMax
	}
	if shape.MemoryOptions.MinPerOCPUInGBs > 0 {
		minValue = math.Max(minValue, ocpus*shape.MemoryOptions.MinPerOCPUInGBs)
	}
	if shape.MemoryOptions.MaxPerOCPUInGBs > 0 {
		maxValue = math.Min(maxValue, ocpus*shape.MemoryOptions.MaxPerOCPUInGBs)
	}
	return minValue, maxValue
}

func (s OCIShapeOption) ocpuRange() (float64, float64) {
	minValue := s.OCPUs
	maxValue := s.OCPUs
	if s.IsFlexible {
		if s.OCPUOptions.Min > 0 {
			minValue = s.OCPUOptions.Min
		}
		if s.OCPUOptions.Max > 0 {
			maxValue = s.OCPUOptions.Max
		}
	}
	if minValue <= 0 && maxValue > 0 {
		minValue = maxValue
	}
	if maxValue <= 0 && minValue > 0 {
		maxValue = minValue
	}
	return minValue, maxValue
}

func (s OCIShapeOption) memoryRange() (float64, float64) {
	minValue := s.MemoryInGBs
	maxValue := s.MemoryInGBs
	if s.IsFlexible {
		if s.MemoryOptions.MinInGBs > 0 {
			minValue = s.MemoryOptions.MinInGBs
		}
		if s.MemoryOptions.MaxInGBs > 0 {
			maxValue = s.MemoryOptions.MaxInGBs
		}
	}
	if minValue <= 0 && maxValue > 0 {
		minValue = maxValue
	}
	if maxValue <= 0 && minValue > 0 {
		maxValue = minValue
	}
	return minValue, maxValue
}

func (s OCIShapeOption) defaultOCPUs() float64 {
	if s.MemoryOptions.DefaultOCPUs > 0 {
		return s.MemoryOptions.DefaultOCPUs
	}
	if s.OCPUs > 0 {
		return s.OCPUs
	}
	minValue, _ := s.ocpuRange()
	return minValue
}

func (s OCIShapeOption) defaultMemoryInGBs(ocpus float64) float64 {
	if s.MemoryOptions.DefaultMemoryInGBs > 0 {
		return s.MemoryOptions.DefaultMemoryInGBs
	}
	if s.MemoryInGBs > 0 {
		return s.MemoryInGBs
	}
	if s.MemoryOptions.DefaultPerOCPUInGBs > 0 && ocpus > 0 {
		return ocpus * s.MemoryOptions.DefaultPerOCPUInGBs
	}
	minValue, _ := s.memoryRange()
	return minValue
}

func (o *OCIService) limitWarnings(limits OCIEditLimits) []string {
	warnings := []string{}
	if limits.OCPU.Error != "" {
		warnings = append(warnings, "无法读取 OCPU 剩余额度："+limits.OCPU.Error)
	}
	if limits.Memory.Error != "" {
		warnings = append(warnings, "无法读取内存剩余额度："+limits.Memory.Error)
	}
	return warnings
}

func quotaNameForKind(quotaNames []string, kind string) string {
	for _, quotaName := range quotaNames {
		lower := strings.ToLower(quotaName)
		switch kind {
		case "memory":
			if strings.Contains(lower, "memory") || strings.Contains(lower, "mem") {
				return quotaName
			}
		case "ocpu":
			if strings.Contains(lower, "ocpu") || strings.Contains(lower, "core") || strings.Contains(lower, "cpu") {
				return quotaName
			}
		}
	}
	return ""
}

func ociShapeFamily(shape OCIShapeOption) string {
	text := strings.ToLower(shape.Name + " " + shape.ProcessorDescription)
	switch {
	case strings.Contains(text, "ampere") || strings.Contains(text, ".a1."):
		return "ampere"
	case strings.Contains(text, "amd") || strings.Contains(text, ".e3.") || strings.Contains(text, ".e4.") || strings.Contains(text, ".e5."):
		return "amd"
	case strings.Contains(text, "intel") || strings.Contains(text, ".standard2") || strings.Contains(text, ".optimized3"):
		return "intel"
	default:
		return "special"
	}
}

func shapeFamilySortKey(family string) int {
	switch family {
	case "amd":
		return 1
	case "intel":
		return 2
	case "ampere":
		return 3
	default:
		return 4
	}
}

func float64InterfaceValue(value interface{}) float64 {
	parsed, _ := float64InterfaceValueOK(value)
	return parsed
}

func float64InterfaceValueOK(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(v, "%f", &parsed); err == nil {
			return parsed, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func intFromFloat(value float64) int {
	if value <= 0 {
		return 0
	}
	return int(math.Round(value))
}

func formatOCIAmount(value float64) string {
	if math.Abs(value-math.Round(value)) < 0.000001 {
		return fmt.Sprintf("%.0f", value)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), ".")
}
