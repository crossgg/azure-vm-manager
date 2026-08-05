package main

import "testing"

func TestShapeEditLimitsUseShapeRangesOnly(t *testing.T) {
	limits := shapeEditLimits(OCIShapeOption{
		IsFlexible: true,
		OCPUOptions: OCIShapeOCPUOptions{
			Min: 1,
			Max: 4,
		},
		MemoryOptions: OCIShapeMemoryOptions{
			MinInGBs: 1,
			MaxInGBs: 24,
		},
	})

	if limits.OCPU.ShapeMin != 1 || limits.OCPU.ShapeMax != 4 || limits.OCPU.EffectiveMax != 4 {
		t.Fatalf("unexpected OCPU limits: %+v", limits.OCPU)
	}
	if limits.Memory.ShapeMin != 1 || limits.Memory.ShapeMax != 24 || limits.Memory.EffectiveMax != 24 {
		t.Fatalf("unexpected memory limits: %+v", limits.Memory)
	}
	if limits.OCPU.HasAvailability || limits.Memory.HasAvailability {
		t.Fatalf("did not expect resource availability in shape-only limits: %+v", limits)
	}
}

func TestMemoryRangeForOCPUsHonorsPerOCPUAndShapeMax(t *testing.T) {
	shape := OCIShapeOption{
		IsFlexible: true,
		MemoryOptions: OCIShapeMemoryOptions{
			MinPerOCPUInGBs: 1,
			MaxPerOCPUInGBs: 6,
		},
	}
	limit := OCIResourceLimitInfo{
		ShapeMin:     1,
		ShapeMax:     20,
		EffectiveMax: 20,
	}

	minValue, maxValue := memoryRangeForOCPUs(shape, limit, 4)
	if minValue != 4 {
		t.Fatalf("expected min memory to follow per-OCPU rule, got %v", minValue)
	}
	if maxValue != 20 {
		t.Fatalf("expected max memory to be capped by shape max, got %v", maxValue)
	}
}

func TestValidateOCIShapeConfigRejectsShapeMax(t *testing.T) {
	shape := OCIShapeOption{
		IsFlexible: true,
		MemoryOptions: OCIShapeMemoryOptions{
			MinPerOCPUInGBs: 1,
			MaxPerOCPUInGBs: 6,
		},
	}
	limits := OCIEditLimits{
		OCPU: OCIResourceLimitInfo{
			ShapeMin:     1,
			ShapeMax:     2,
			EffectiveMax: 2,
		},
		Memory: OCIResourceLimitInfo{
			ShapeMin:     1,
			ShapeMax:     12,
			EffectiveMax: 12,
		},
	}

	if err := validateOCIShapeConfig(shape, limits, 4, 12); err == nil {
		t.Fatal("expected OCPU shape max to reject oversized payload")
	}
	if err := validateOCIShapeConfig(shape, limits, 2, 24); err == nil {
		t.Fatal("expected memory shape max to reject oversized payload")
	}
	if err := validateOCIShapeConfig(shape, limits, 2, 12); err != nil {
		t.Fatalf("expected valid shape config, got %v", err)
	}
}
