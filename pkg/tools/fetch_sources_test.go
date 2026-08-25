package tools

import (
	"strings"
	"testing"
)

func TestFetchURLToolDescriptionSeparatesRemoteURLAndLocalPath(t *testing.T) {
	tool := NewFetchURLTool(t.TempDir())
	tool.Configure([]string{"cdn.jsdelivr.net"}, nil, true)
	tool.ConfigureSources(map[string]string{
		"cdn.jsdelivr.net": "versioned npm CDN for Three.js",
	})
	description := tool.Description()
	for _, expected := range []string{
		"url=https://cdn.jsdelivr.net/npm/three@0.152.2/build/three.min.js",
		"path=libs/three.min.js",
		"cdn.jsdelivr.net: versioned npm CDN for Three.js",
		"Never pass a local path as url",
	} {
		if !strings.Contains(description, expected) {
			t.Fatalf("description missing %q: %s", expected, description)
		}
	}
}
