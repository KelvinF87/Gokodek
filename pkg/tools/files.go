package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxReadBytes = 128 << 10

type workspaceTool struct {
	workspace string
}

func newWorkspaceTool(workspace string) workspaceTool {
	if strings.TrimSpace(workspace) == "" {
		workspace, _ = os.Getwd()
	}
	absolute, err := filepath.Abs(workspace)
	if err == nil {
		workspace = absolute
	}
	return workspaceTool{workspace: filepath.Clean(workspace)}
}

func (t workspaceTool) resolve(rawPath string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	candidate := rawPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(t.workspace, candidate)
	}
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	candidate = filepath.Clean(candidate)

	relative, err := filepath.Rel(t.workspace, candidate)
	if err != nil {
		return "", fmt.Errorf("check workspace path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside workspace %q", rawPath, t.workspace)
	}
	return candidate, nil
}

type ReadFileTool struct{ workspaceTool }

func NewReadFileTool(workspace string) *ReadFileTool {
	return &ReadFileTool{workspaceTool: newWorkspaceTool(workspace)}
}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string {
	return "Read a UTF-8 text file inside the current workspace."
}
func (t *ReadFileTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"path": map[string]interface{}{"type": "string", "description": "File path relative to the workspace."},
	}, "path")
}
func (t *ReadFileTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("read_file arguments: %w", err)
	}
	path, err := t.resolve(args.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", args.Path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%q is a directory, not a file", args.Path)
	}
	if info.Size() > maxReadBytes {
		return "", fmt.Errorf("file %q is too large (%d bytes; limit is %d)", args.Path, info.Size(), maxReadBytes)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", args.Path, err)
	}
	return string(content), nil
}

type WriteFileTool struct{ workspaceTool }

func NewWriteFileTool(workspace string) *WriteFileTool {
	return &WriteFileTool{workspaceTool: newWorkspaceTool(workspace)}
}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Create or update a UTF-8 text file inside the workspace. Reuses an existing singular/plural equivalent instead of creating duplicate files and creates a Git checkpoint before overwriting an existing file."
}
func (t *WriteFileTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"path":    map[string]interface{}{"type": "string", "description": "File path relative to the workspace."},
		"content": map[string]interface{}{"type": "string", "description": "Complete UTF-8 content to write."},
	}, "path", "content")
}
func (t *WriteFileTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("write_file arguments: %w", err)
	}
	path, err := t.resolve(args.Path)
	if err != nil {
		return "", err
	}
	requestedPath := args.Path
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if equivalent := findEquivalentFile(path); equivalent != "" {
			path = equivalent
			if relative, relErr := filepath.Rel(t.workspace, equivalent); relErr == nil {
				args.Path = relative
			}
		}
	}
	if err := validateWriteContent(args.Path, args.Content); err != nil {
		return "", err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		if _, checkpointErr := runGitCheckpoint(t.workspace, "proteger sobrescritura de "+args.Path); checkpointErr != nil {
			return "", checkpointErr
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("create parent directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("write %q: %w", args.Path, err)
	}
	writtenInfo, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("verify written file %q: %w", args.Path, err)
	}
	message := fmt.Sprintf("wrote %d bytes to %s; verified file exists with %d bytes", len(args.Content), args.Path, writtenInfo.Size())
	if requestedPath != args.Path {
		message += fmt.Sprintf("; reused existing equivalent instead of creating duplicate %s", requestedPath)
	}
	return message, nil
}

func findEquivalentFile(path string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	candidates := []string{base + "s" + ext}
	if strings.HasSuffix(base, "s") && len(base) > 1 {
		candidates = append(candidates, strings.TrimSuffix(base, "s")+ext)
	}
	for _, candidate := range candidates {
		candidatePath := filepath.Join(dir, candidate)
		if info, err := os.Stat(candidatePath); err == nil && !info.IsDir() {
			return candidatePath
		}
	}
	return ""
}

type ListDirTool struct{ workspaceTool }

func NewListDirTool(workspace string) *ListDirTool {
	return &ListDirTool{workspaceTool: newWorkspaceTool(workspace)}
}

func (t *ListDirTool) Name() string { return "list_dir" }
func (t *ListDirTool) Description() string {
	return "List files and directories inside the workspace, including type and size."
}
func (t *ListDirTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"path": map[string]interface{}{"type": "string", "description": "Directory path relative to the workspace; defaults to the workspace root."},
	})
}
func (t *ListDirTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("list_dir arguments: %w", err)
	}
	if strings.TrimSpace(args.Path) == "" {
		args.Path = "."
	}
	path, err := t.resolve(args.Path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("list %q: %w", args.Path, err)
	}
	if len(entries) == 0 {
		return "(empty directory)", nil
	}

	var builder strings.Builder
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			return "", fmt.Errorf("inspect %q: %w", entry.Name(), infoErr)
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		fmt.Fprintf(&builder, "%s\t%s\t%d bytes\n", kind, entry.Name(), info.Size())
	}
	return strings.TrimRight(builder.String(), "\n"), nil
}

func validateWriteContent(path, content string) error {
	name := strings.ToLower(filepath.Base(path))
	trimmed := strings.TrimSpace(content)
	if name == "styles.css" && trimmed == "" {
		return fmt.Errorf("refusing to overwrite styles.css with empty content")
	}
	if name != "index.html" && !strings.HasSuffix(name, ".html") && !strings.HasSuffix(name, ".htm") {
		return nil
	}

	// Prevent a small model from replacing an existing complete page with a
	// fragment such as only <link rel="stylesheet" ...>. New files may still
	// be minimal, but an existing substantially larger page must be preserved.
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 512 || int64(len(trimmed))*4 >= info.Size() {
		return nil
	}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "<html") || !strings.Contains(lower, "<body") {
		return fmt.Errorf("refusing to overwrite %s with incomplete HTML; read the file and preserve its full document", path)
	}
	return nil
}

func objectSchema(properties map[string]interface{}, required ...string) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
