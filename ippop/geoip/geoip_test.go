package geoip

import (
	"testing"
)

func TestGeoIPDisabledByDefault(t *testing.T) {
	// Initialize with empty DB path (disabled state)
	err := Init("")
	if err != nil {
		t.Fatalf("Init with empty path should not fail: %v", err)
	}
	defer Close()

	// Ensure lookups on empty/disabled DB gracefully return empty string
	if country := LookupCountry("8.8.8.8"); country != "" {
		t.Errorf("expected empty string when disabled, got %q", country)
	}

	if country := LookupCountry("invalid-ip"); country != "" {
		t.Errorf("expected empty string for invalid IP, got %q", country)
	}
}

func TestGeoIPInvalidDBPath(t *testing.T) {
	// Initialize with non-existent path
	err := Init("non_existent_file.mmdb")
	if err != nil {
		t.Fatalf("Init with non-existent file should gracefully handle error and return nil: %v", err)
	}
	defer Close()

	// Lookups should still gracefully return empty string
	if country := LookupCountry("8.8.8.8"); country != "" {
		t.Errorf("expected empty string when DB fails to load, got %q", country)
	}
}
