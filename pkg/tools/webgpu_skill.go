package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type WebGPUSkillTool struct{ workspace string }

func NewWebGPUSkillTool(workspace string) *WebGPUSkillTool {
	return &WebGPUSkillTool{workspace: workspace}
}
func (t *WebGPUSkillTool) Name() string { return "webgpu_skill" }
func (t *WebGPUSkillTool) Description() string {
	return "Return a compact, local WebGPU implementation skill for 3D demos and small browser games, including WebGL2 fallback and low-memory rules. It does not modify application files."
}
func (t *WebGPUSkillTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"goal": map[string]interface{}{"type": "string", "description": "Requested 3D demo or small game."}}, "required": []string{"goal"}}
}
func (t *WebGPUSkillTool) Execute(raw string) (string, error) {
	var a struct {
		Goal string `json:"goal"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.Goal) == "" {
		return "", fmt.Errorf("webgpu_skill requires goal")
	}
	dir := filepath.Join(t.workspace, ".gokodek", "skills", "webgpu")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "SKILL.md")
	content := `# WebGPU / WebGL2 skill

## Official sources
- https://webgpu.github.io/
- https://gpuweb.github.io/gpuweb/

## Implementation rules
- Detect navigator.gpu before using WebGPU.
- Request an adapter and device with explicit error handling.
- Keep one render loop and release resources when the page closes.
- Prefer low-poly geometry, small textures, instancing, and capped pixel ratio.
- If WebGPU is unavailable, render the same basic scene through WebGL2.
- Show a visible compatibility/status message instead of a blank canvas.
- For games, separate update(), input handling, and render(); cap delta time.
- Verify with start_server, check_web, browser_screenshot, and browser console errors.
- Never claim visual success without an actual HTTP page and screenshot.

## Recommended files
index.html, styles.css, and script.js for a dependency-free demo; use an existing package.json only when present.

## User goal
` + a.Goal + "\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", err
	}
	return fmt.Sprintf("WebGPU skill preparada en .gokodek/skills/webgpu/SKILL.md. Usa WebGPU con fallback WebGL2 para: %s", a.Goal), nil
}
