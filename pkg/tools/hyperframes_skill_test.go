package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHyperFramesSkillTool(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewHyperFramesSkillTool(tempDir, "", "")

	if tool.Name() != "hyperframes_skill" {
		t.Errorf("expected name to be hyperframes_skill, got %s", tool.Name())
	}

	res, err := tool.Execute(`{"goal":"Create a WebGL transition test"}`)
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	if res == "" {
		t.Fatal("expected non-empty output")
	}

	// Check if local file was written
	localFile := filepath.Join(tempDir, ".gokodek", "skills", "hyperframes", "SKILL.md")
	if _, err := os.Stat(localFile); os.IsNotExist(err) {
		t.Errorf("expected local skill file to exist at %s", localFile)
	}
}
