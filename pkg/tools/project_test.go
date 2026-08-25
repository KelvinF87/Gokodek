package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectInfoAndUIRecipePreferNativeCSS(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := NewProjectInfoTool(root).Execute(`{}`)
	if err != nil || !strings.Contains(info, "HTML") {
		t.Fatalf("info=%q err=%v", info, err)
	}
	recipe, err := NewUIRecipeTool(root).Execute(`{"goal":"portfolio","preference":"auto"}`)
	if err != nil || !strings.Contains(recipe, "choice=css") {
		t.Fatalf("recipe=%q err=%v", recipe, err)
	}
}
