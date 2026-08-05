package main

import "testing"

func TestQuotaNameForKind(t *testing.T) {
	quotaNames := []string{"standard-a1-core-count", "standard-a1-memory-count"}

	if got := quotaNameForKind(quotaNames, "ocpu"); got != "standard-a1-core-count" {
		t.Fatalf("unexpected ocpu quota name: %q", got)
	}
	if got := quotaNameForKind(quotaNames, "memory"); got != "standard-a1-memory-count" {
		t.Fatalf("unexpected memory quota name: %q", got)
	}
}

func TestMemoryRangeForOCPUsHonorsPerOCPUAndAvailability(t *testing.T) {
	shape := OCIShapeOption{
		IsFlexible: true,
		MemoryOptions: OCIShapeMemoryOptions{
			MinPerOCPUInGBs: 1,
			MaxPerOCPUInGBs: 6,
		},
	}
	limit := OCIResourceLimitInfo{
		ShapeMin:     1,
		ShapeMax:     64,
		EffectiveMax: 20,
	}

	minValue, maxValue := memoryRangeForOCPUs(shape, limit, 4)
	if minValue != 4 {
		t.Fatalf("expected min memory to follow per-OCPU rule, got %v", minValue)
	}
	if maxValue != 20 {
		t.Fatalf("expected max memory to be capped by availability, got %v", maxValue)
	}
}

func TestValidateOCIShapeConfigRejectsAvailabilityMax(t *testing.T) {
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
			ShapeMax:     4,
			EffectiveMax: 2,
		},
		Memory: OCIResourceLimitInfo{
			ShapeMin:     1,
			ShapeMax:     24,
			EffectiveMax: 12,
		},
	}

	if err := validateOCIShapeConfig(shape, limits, 4, 12); err == nil {
		t.Fatal("expected OCPU availability max to reject oversized payload")
	}
	if err := validateOCIShapeConfig(shape, limits, 2, 24); err == nil {
		t.Fatal("expected memory availability max to reject oversized payload")
	}
	if err := validateOCIShapeConfig(shape, limits, 2, 12); err != nil {
		t.Fatalf("expected valid shape config, got %v", err)
	}
}
