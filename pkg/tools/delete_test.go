package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteFileRequiresConfirmation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "old.css")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	tool := NewDeleteFileTool(root)
	if _, err := tool.Execute(`{"path":"old.css","confirm":false,"reason":"obsolete"}`); err == nil {
		t.Fatal("expected confirmation requirement")
	}
	_, err := tool.Execute(`{"path":"old.css","confirm":true,"reason":"obsolete"}`)
	if err == nil {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected file to be deleted after a successful checkpoint, stat error=%v", statErr)
		}
	} else if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("file was deleted despite checkpoint failure: %v", err)
	}
}

func TestDeleteFileRejectsNonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "old")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "x.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := NewDeleteFileTool(root).Execute(`{"path":"old","confirm":true,"reason":"obsolete"}`)
	if err == nil {
		t.Fatal("expected non-empty directory rejection")
	}
}
