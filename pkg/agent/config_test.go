package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultScrapingIncludesOfficialSourcesAndCDNs(t *testing.T) {
	config := DefaultConfig("")
	for _, domain := range []string{"threejs.org", "webgpu.github.io", "cdn.jsdelivr.net", "unpkg.com", "cdnjs.cloudflare.com", "esm.sh"} {
		if !containsFold(config.Scraping.AllowedDomains, domain) {
			t.Fatalf("default scraping domains missing %q", domain)
		}
	}
	for _, domain := range []string{"threejs.org", "cdn.jsdelivr.net", "unpkg.com"} {
		if source, ok := sourceByDomain(config.Scraping.Sources, domain); !ok || strings.TrimSpace(source.Description) == "" {
			t.Fatalf("default scraping source %q has no description", domain)
		}
	}
}

func TestMergeScrapingConfigMigratesWithoutDuplicates(t *testing.T) {
	custom := ScrapingConfig{
		AllowedDomains:      []string{"threejs.org", "my-docs.example"},
		Sources:             []AllowedSource{{Domain: "my-docs.example", Description: "Private project docs", UseFor: []string{"project"}}},
		RequireConfirmation: true,
	}
	merged := mergeScrapingConfig(custom)
	if countFold(merged.AllowedDomains, "threejs.org") != 1 {
		t.Fatalf("expected threejs.org exactly once, got %v", merged.AllowedDomains)
	}
	if !containsFold(merged.AllowedDomains, "cdn.jsdelivr.net") {
		t.Fatalf("migration did not add cdn.jsdelivr.net")
	}
	if source, ok := sourceByDomain(merged.Sources, "my-docs.example"); !ok || source.Description != "Private project docs" {
		t.Fatalf("custom source was not preserved: %#v", merged.Sources)
	}
	encoded, err := json.Marshal(merged)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("merged config is not serializable: %v", err)
	}
}

func containsFold(values []string, target string) bool {
	return countFold(values, target) > 0
}

func countFold(values []string, target string) int {
	count := 0
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			count++
		}
	}
	return count
}

func sourceByDomain(values []AllowedSource, target string) (AllowedSource, bool) {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value.Domain), target) {
			return value, true
		}
	}
	return AllowedSource{}, false
}
