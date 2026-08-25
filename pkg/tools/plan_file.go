package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type PlanFileTool struct{ workspace string }

func NewPlanFileTool(workspace string) *PlanFileTool { return &PlanFileTool{workspace: workspace} }
func (t *PlanFileTool) Name() string                 { return "plan_file" }
func (t *PlanFileTool) Description() string {
	return "Write the structured implementation plan only to .gokodek/plan.md. This is the only write allowed in plan mode."
}
func (t *PlanFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"content": map[string]interface{}{"type": "string", "description": "Complete Markdown plan with analysis, ordered tasks, files, risks, tests, and acceptance criteria."}}, "required": []string{"content"}}
}
func (t *PlanFileTool) Execute(raw string) (string, error) {
	var a struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("plan_file arguments: %w", err)
	}
	if strings.TrimSpace(a.Content) == "" {
		return "", fmt.Errorf("plan_file content cannot be empty")
	}
	path := filepath.Join(t.workspace, ".gokodek", "plan.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	header := fmt.Sprintf("# gokodek implementation plan\n\n> Generated: %s\n> Mode: plan (no application files modified)\n\n", time.Now().Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(header+a.Content+"\n"), 0600); err != nil {
		return "", fmt.Errorf("write plan: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Plan guardado únicamente en .gokodek/plan.md (%d bytes)", info.Size()), nil
}
