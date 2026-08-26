package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gokodek/pkg/rag"
)

type HyperFramesSkillTool struct {
	workspace      string
	ollamaEndpoint string
	embeddingModel string
}

func NewHyperFramesSkillTool(workspace, ollamaEndpoint, embeddingModel string) *HyperFramesSkillTool {
	return &HyperFramesSkillTool{
		workspace:      workspace,
		ollamaEndpoint: ollamaEndpoint,
		embeddingModel: embeddingModel,
	}
}

func (t *HyperFramesSkillTool) Name() string { return "hyperframes_skill" }
func (t *HyperFramesSkillTool) Description() string {
	return "Generate and configure a comprehensive HyperFrames instruction skill for deterministic HTML-native video rendering, seekable timelines, audio mixing, and browser automation testing. It registers the skill locally and globally."
}

func (t *HyperFramesSkillTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"goal": map[string]interface{}{"type": "string", "description": "Optional description of the video or graphic project."},
	})
}

func (t *HyperFramesSkillTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Goal string `json:"goal"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("hyperframes_skill arguments: %w", err)
	}

	// 1. Build Skill MD content
	skillMD := `# Universal Skill: HyperFrames Video & Motion Graphics Production

## License
Apache 2.0 (Open Source) - Feel free to distribute and modify this skill framework.

## Introduction
HyperFrames is an open-source framework for turning HTML, CSS, media, and seekable animations into deterministic MP4 videos. Compositions are standard HTML files with data attributes for timing and tracks. Headless Chrome captures frames sequentially, and FFmpeg encodes the final MP4.

## Core Composition Rules

### 1. HTML Layout & Timing Attributes
Every composition starts with a stage wrapper specifying resolution, start time, and composition ID:
` + "```html" + `
<div id="stage" data-composition-id="main-comp" data-start="0" data-width="1920" data-height="1080">
  <!-- Clips go here -->
</div>
` + "```" + `

- **Clips (` + "`class=\"clip\"`" + `)**: Elements rendered over time must use the ` + "`clip`" + ` class and timing attributes:
  - ` + "`data-start`" + `: Start timestamp in seconds.
  - ` + "`data-duration`" + `: Total active duration in seconds.
  - ` + "`data-track-index`" + `: Layer stacking index (higher values overlay lower values).
- **Muted Media**: Always include ` + "`muted playsinline`" + ` in ` + "`<video>`" + ` tags to prevent Chrome playback locks.

### 2. Seekable Animations (The Timeline Adapter Contract)
Animations MUST be deterministic and seekable. Do not use random numbers, ` + "`Date.now()`" + `, or wall-clock clocks. Use frame-based triggers or timeline registration.
Register GSAP timelines under ` + "`window.__timelines`" + ` so the capture engine can control playback:
` + "```javascript" + `
const tl = gsap.timeline({ paused: true });
tl.from("#title", { opacity: 0, y: 50, duration: 1.2, ease: "power2.out" }, 0.5);

window.__timelines = window.__timelines || {};
window.__timelines["main-comp"] = tl;
` + "```" + `

### 3. Audio Mixing & Effect Chains
- Use the '<audio>' tag for soundtracks and voiceovers, specifying timing:
  ` + "```html" + `
  <audio data-start="0" data-duration="10" data-track-index="1" data-volume="0.5" src="bgm.mp3"></audio>
  ` + "```" + `
- Differentiate music tracks and narrative voiceovers. Apply voiceover carving to dynamically lower background music volume when speech runs.

---

## Browser-Based Integration Testing (Testing Extension Integration)

To verify the visual output and scripting accuracy, utilize the local ` + "`browser_control`" + ` extension tool. This allows you to bypass sandboxing, dialog prompt locks, and perform automated QA.

### Rules for Testing
1. **Serve the App**: Start the dev server using ` + "`start_server`" + ` to resolve to ` + "`http://127.0.0.1:PORT`" + `.
2. **Escaping Dialogs**: The browser extension automatically intercepts and escapes alert/confirm blocking popups to keep tests non-blocking.
3. **Command Loop**: Use ` + "`browser_control`" + ` actions:
   - ` + "`open`" + `: Navigate to the local server URL.
   - ` + "`click`" + `: Target selectors like buttons, menu items, or canvas elements.
   - ` + "`type`" + `: Fill input fields.
   - ` + "`eval`" + `: Execute custom JavaScript to assert timeline progress (` + "`window.__timelines['main-comp'].progress()`" + `).
   - ` + "`screenshot`" + `: Capture a screenshot to feed to the vision model to inspect animations visually.
   - ` + "`state`" + `: Verify page title, text excerpts, and read consolidated browser console errors.
4. **Zero-Error Guarantee**: Never mark a production composition as functional if console logs contain JavaScript errors, CORS violations, or media asset load failures.

---

## Workspace CLI Commands
- **Initialization**: ` + "`npx hyperframes init [project-name]`" + `
- **Live Preview**: ` + "`npx hyperframes preview`" + ` (hosts a live dev server)
- **Render Output**: ` + "`npx hyperframes render --output video.mp4`" + `

`
	if strings.TrimSpace(args.Goal) != "" {
		skillMD += "\n## Project-Specific Goal\n" + args.Goal + "\n"
	}

	// 2. Write locally
	localDir := filepath.Join(t.workspace, ".gokodek", "skills", "hyperframes")
	if err := os.MkdirAll(localDir, 0700); err != nil {
		return "", fmt.Errorf("creating local skills dir: %w", err)
	}
	localPath := filepath.Join(localDir, "SKILL.md")
	if err := os.WriteFile(localPath, []byte(skillMD), 0600); err != nil {
		return "", fmt.Errorf("writing local skill file: %w", err)
	}

	// 3. Write globally
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}
	globalDir := filepath.Join(homeDir, ".gokodek", "skills", "hyperframes")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		return "", fmt.Errorf("creating global skills dir: %w", err)
	}
	globalPath := filepath.Join(globalDir, "SKILL.md")
	if err := os.WriteFile(globalPath, []byte(skillMD), 0600); err != nil {
		return "", fmt.Errorf("writing global skill file: %w", err)
	}

	// 4. Index globally via RAG if embedding model and client are configured
	var fragments int
	if t.ollamaEndpoint != "" && t.embeddingModel != "" {
		globalIndexPath := filepath.Join(homeDir, ".gokodek", "global_rag_index.json")
		embedder := rag.NewEmbedder(t.ollamaEndpoint, t.embeddingModel)
		index := rag.NewIndex(globalIndexPath, embedder)
		_ = index.Load()
		ctx := context.Background()
		n, err := index.AddFile(ctx, globalPath, 800)
		if err == nil {
			_ = index.Save()
			fragments = n
		}
	}

	return fmt.Sprintf("HyperFrames skill configured successfully under local path %s and global path %s. Globally indexed %d fragments.", localPath, globalPath, fragments), nil
}
