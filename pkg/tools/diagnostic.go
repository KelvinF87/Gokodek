package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DiagnoseProjectTool struct{ workspaceTool }

func NewDiagnoseProjectTool(workspace string) *DiagnoseProjectTool {
	return &DiagnoseProjectTool{newWorkspaceTool(workspace)}
}
func (t *DiagnoseProjectTool) Name() string { return "diagnose_project" }
func (t *DiagnoseProjectTool) Description() string {
	return "Find likely relevant project files for a reported bug using filenames, extensions, and text matches; does not modify files."
}
func (t *DiagnoseProjectTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{"query": map[string]interface{}{"type": "string", "description": "Bug or feature to investigate."}, "max_files": map[string]interface{}{"type": "integer", "description": "Maximum files to return; default 12."}}, "query")
}
func (t *DiagnoseProjectTool) Execute(raw string) (string, error) {
	var a struct {
		Query string `json:"query"`
		Max   int    `json:"max_files"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("diagnose_project arguments: %w", err)
	}
	if strings.TrimSpace(a.Query) == "" {
		return "", fmt.Errorf("diagnose_project requires query")
	}
	if a.Max <= 0 || a.Max > 30 {
		a.Max = 12
	}
	terms := strings.Fields(strings.ToLower(a.Query))
	type candidate struct {
		path   string
		score  int
		reason string
	}
	var cs []candidate
	err := filepath.Walk(t.workspace, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", ".gokodek", "node_modules", "vendor", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if info.Size() > 512<<10 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		allowed := map[string]bool{".php": true, ".html": true, ".htm": true, ".css": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true, ".json": true, ".sql": true, ".go": true, ".py": true, ".md": true}
		if !allowed[ext] {
			return nil
		}
		rel, _ := filepath.Rel(t.workspace, path)
		base := strings.ToLower(rel)
		score := 0
		reasons := []string{}
		for _, term := range terms {
			if strings.Contains(base, term) {
				score += 4
				reasons = append(reasons, "nombre")
			}
		}
		data, _ := os.ReadFile(path)
		text := strings.ToLower(string(data))
		for _, term := range terms {
			if len(term) >= 3 && strings.Contains(text, term) {
				score++
				reasons = append(reasons, "contenido")
			}
		}
		if strings.Contains(base, "docx") || strings.Contains(text, "docx") {
			if strings.Contains(strings.ToLower(a.Query), "docx") {
				score += 5
				reasons = append(reasons, "docx")
			}
		}
		if score > 0 {
			cs = append(cs, candidate{rel, score, strings.Join(reasons, ",")})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].score > cs[j].score })
	if len(cs) > a.Max {
		cs = cs[:a.Max]
	}
	if len(cs) == 0 {
		return "No se encontraron archivos relevantes. Inspecciona primero project_info y list_dir; no se modificó nada.", nil
	}
	var b strings.Builder
	b.WriteString("Archivos candidatos (ordenados, no modificados):\n")
	for _, c := range cs {
		fmt.Fprintf(&b, "- %s score=%d motivo=%s\n", c.path, c.score, c.reason)
	}
	b.WriteString("Lee solo estos candidatos y sus dependencias directas antes de editar.")
	return b.String(), nil
}
