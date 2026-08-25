package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ProjectInfoTool struct{ workspaceTool }

func NewProjectInfoTool(workspace string) *ProjectInfoTool {
	return &ProjectInfoTool{workspaceTool: newWorkspaceTool(workspace)}
}

func (t *ProjectInfoTool) Name() string { return "project_info" }
func (t *ProjectInfoTool) Description() string {
	return "Inspect the workspace and return a compact summary of detected project files, package managers, and UI libraries without reading the whole project."
}
func (t *ProjectInfoTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{})
}
func (t *ProjectInfoTool) Execute(argsJSON string) (string, error) {
	var ignored map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &ignored); err != nil {
		return "", fmt.Errorf("project_info arguments: %w", err)
	}
	entries, err := os.ReadDir(t.workspace)
	if err != nil {
		return "", fmt.Errorf("inspect workspace: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	return compactProjectSummary(t.workspace, files), nil
}

func compactProjectSummary(root string, files []string) string {
	checks := []struct {
		name  string
		paths []string
	}{
		{"Go", []string{"go.mod", "go.sum"}},
		{"Node", []string{"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock"}},
		{"HTML", []string{"index.html"}},
		{"CSS", []string{"styles.css", "style.css", "tailwind.css"}},
		{"Bootstrap", []string{"bootstrap.css", "bootstrap.min.css"}},
		{"Tailwind", []string{"tailwind.config.js", "tailwind.config.ts"}},
	}
	var detected []string
	for _, check := range checks {
		for _, path := range check.paths {
			if _, err := os.Stat(filepath.Join(root, path)); err == nil {
				detected = append(detected, check.name)
				break
			}
		}
	}
	return fmt.Sprintf("workspace=%s\nfiles=%s\ndetected=%s", root, strings.Join(files, ", "), strings.Join(detected, ", "))
}

type UIRecipeTool struct{ workspaceTool }

func NewUIRecipeTool(workspace string) *UIRecipeTool {
	return &UIRecipeTool{workspaceTool: newWorkspaceTool(workspace)}
}

func (t *UIRecipeTool) Name() string { return "ui_recipe" }
func (t *UIRecipeTool) Description() string {
	return "Recommend a lightweight UI approach and return a compact implementation recipe; does not install packages or change files."
}
func (t *UIRecipeTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"goal":       map[string]interface{}{"type": "string", "description": "UI goal, such as a modern portfolio or retro profile."},
		"preference": map[string]interface{}{"type": "string", "description": "Optional preference: css, bootstrap, tailwind, or auto."},
	})
}
func (t *UIRecipeTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Goal       string `json:"goal"`
		Preference string `json:"preference"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("ui_recipe arguments: %w", err)
	}
	preference := strings.ToLower(strings.TrimSpace(args.Preference))
	if preference == "" || preference == "auto" {
		preference = "css"
		if _, err := os.Stat(filepath.Join(t.workspace, "package.json")); err == nil {
			preference = "bootstrap"
		}
	}
	switch preference {
	case "css", "native":
		return "choice=css\nreason=zero dependencies and lowest memory\nrecipe=Use CSS variables, responsive grid/flex, clamp(), prefers-reduced-motion, and component classes. Keep one styles.css file.", nil
	case "bootstrap":
		return "choice=bootstrap\nreason=fast polished components with one CDN stylesheet for static pages\nrecipe=Use Bootstrap 5 CDN only when internet is available; use container, row, col, navbar, card, btn, and utilities. Add small custom overrides after Bootstrap.", nil
	case "tailwind":
		return "choice=tailwind\nreason=utility-first styling, best with an existing Node build\nrecipe=Use Tailwind only when package.json and a build pipeline already exist. Do not install it for a simple static page; CDN mode is convenient but adds network dependency.", nil
	default:
		return "", fmt.Errorf("unsupported UI preference %q; use css, bootstrap, tailwind, or auto", preference)
	}
}
