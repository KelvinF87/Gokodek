package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DeleteFileTool struct{ workspaceTool }

func NewDeleteFileTool(workspace string) *DeleteFileTool {
	return &DeleteFileTool{workspaceTool: newWorkspaceTool(workspace)}
}
func (t *DeleteFileTool) Name() string { return "delete_file" }
func (t *DeleteFileTool) Description() string {
	return "Delete an obsolete file or empty directory inside the workspace. Requires confirm=true and creates a Git checkpoint before deletion. Never deletes the workspace root, .git, or .gokodek."
}
func (t *DeleteFileTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"path":    map[string]interface{}{"type": "string", "description": "File or empty directory relative to the workspace."},
		"confirm": map[string]interface{}{"type": "boolean", "description": "Must be true to authorize deletion."},
		"reason":  map[string]interface{}{"type": "string", "description": "Why this item is obsolete."},
	}, "path", "confirm", "reason")
}
func (t *DeleteFileTool) Execute(raw string) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Confirm bool   `json:"confirm"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", fmt.Errorf("delete_file arguments: %w", err)
	}
	if !args.Confirm {
		return "", fmt.Errorf("delete_file requiere confirm=true; primero identifica y muestra el archivo obsoleto")
	}
	if strings.TrimSpace(args.Reason) == "" {
		return "", fmt.Errorf("delete_file requiere reason")
	}
	path, err := t.resolve(args.Path)
	if err != nil {
		return "", err
	}
	rel, _ := filepath.Rel(t.workspace, path)
	if rel == "." || rel == "" || rel == ".git" || rel == ".gokodek" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) || strings.HasPrefix(rel, ".gokodek"+string(filepath.Separator)) {
		return "", fmt.Errorf("no se permite borrar el workspace, .git ni .gokodek")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", args.Path, err)
	}
	if info.IsDir() {
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return "", readErr
		}
		if len(entries) != 0 {
			return "", fmt.Errorf("la carpeta %q no está vacía; borra archivos explícitamente", args.Path)
		}
	}
	checkpoint, err := runGitCheckpoint(t.workspace, "proteger borrado de "+args.Path)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("delete %q: %w", args.Path, err)
	}
	return fmt.Sprintf("eliminado %s (%s). %s", args.Path, args.Reason, checkpoint), nil
}
