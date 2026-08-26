package tools

import (
	"testing"
)

func TestBrowserControlToolRequiresActiveBrowser(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewBrowserControlTool(tempDir)

	if tool.Name() != "browser_control" {
		t.Errorf("expected name browser_control, got %s", tool.Name())
	}

	// Running a DOM action when browser is not running should fail
	_, err := tool.Execute(`{"action":"click","selector":"#btn"}`)
	if err == nil {
		t.Fatal("expected click to fail when browser is not running")
	}
}
