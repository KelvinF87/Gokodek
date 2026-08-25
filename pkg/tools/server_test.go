package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasExtensionSkipsGeneratedFolders(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "x", "bad.php"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if hasExtension(root, ".php") {
		t.Fatal("generated dependency folder must be ignored")
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if hasExtension(root, ".php") {
		t.Fatal("html project must not be classified as php")
	}
}

func TestHasPackageScript(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "package.json")
	if err := os.WriteFile(path, []byte(`{"scripts":{"dev":"vite"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !hasPackageScript(path, "dev") {
		t.Fatal("expected dev script")
	}
	if hasPackageScript(path, "start") {
		t.Fatal("unexpected start script")
	}
}
