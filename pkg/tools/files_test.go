package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileToolsUseWorkspaceAndRejectTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}

	writer := NewWriteFileTool(root)
	args, _ := json.Marshal(map[string]string{"path": "src/hello.txt", "content": "hola"})
	if _, err := writer.Execute(string(args)); err != nil {
		t.Fatal(err)
	}

	reader := NewReadFileTool(root)
	content, err := reader.Execute(`{"path":"src/hello.txt"}`)
	if err != nil || content != "hola" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	if _, err := reader.Execute(`{"path":"../outside.txt"}`); err == nil {
		t.Fatal("expected traversal to be rejected")
	}

	listing, err := NewListDirTool(root).Execute(`{"path":"src"}`)
	if err != nil || !strings.Contains(listing, "hello.txt") {
		t.Fatalf("listing=%q err=%v", listing, err)
	}
}

func TestCheckWebToolValidatesLocalReferences(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(`<!doctype html><html><body><main>Hola</main><link rel="stylesheet" href="styles.css"><script src="script.js"></script></body></html>`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles.css"), []byte("body { color: red; }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "script.js"), []byte("console.log('ok');"), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := NewCheckWebTool(root).Execute(`{"path":"index.html"}`)
	if err != nil || !strings.Contains(result, "all local references are valid") {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if err := os.Remove(filepath.Join(root, "styles.css")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCheckWebTool(root).Execute(`{"path":"index.html"}`); err == nil {
		t.Fatal("expected missing stylesheet to fail")
	}
}

func TestRunCmdToolRejectsDangerousCommand(t *testing.T) {
	tool := NewRunCmdTool(t.TempDir())
	if _, err := tool.Execute(`{"command":"git reset --hard"}`); err == nil {
		t.Fatal("expected dangerous command to be rejected")
	}
}
