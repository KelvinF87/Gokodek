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

type CreateGlobalSkillTool struct {
	workspace      string
	ollamaEndpoint string
	embeddingModel string
}

func NewCreateGlobalSkillTool(workspace, ollamaEndpoint, embeddingModel string) *CreateGlobalSkillTool {
	return &CreateGlobalSkillTool{
		workspace:      workspace,
		ollamaEndpoint: ollamaEndpoint,
		embeddingModel: embeddingModel,
	}
}

func (t *CreateGlobalSkillTool) Name() string { return "create_global_skill" }
func (t *CreateGlobalSkillTool) Description() string {
	return "Create and save a universal, project-agnostic skill/knowledge pattern into the global Gokodek store (~/.gokodek/skills/), and automatically index it into the global vector RAG database for use across any project."
}

func (t *CreateGlobalSkillTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"name":         map[string]interface{}{"type": "string", "description": "Short identifier for the skill (e.g. realistic_3d_city, webgpu_engine, jwt_auth_handler)."},
		"description":  map[string]interface{}{"type": "string", "description": "Brief description of what this universal skill teaches/accomplishes."},
		"instructions": map[string]interface{}{"type": "string", "description": "Detailed step-by-step universal guidelines, best practices, concepts, and code patterns for this skill."},
	})
}

func (t *CreateGlobalSkillTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("create_global_skill arguments: %w", err)
	}

	name := strings.TrimSpace(strings.ToLower(args.Name))
	if name == "" || strings.TrimSpace(args.Instructions) == "" {
		return "", fmt.Errorf("name and instructions are required for create_global_skill")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}

	skillsDir := filepath.Join(homeDir, ".gokodek", "skills", name)
	if err := os.MkdirAll(skillsDir, 0700); err != nil {
		return "", fmt.Errorf("cannot create skills directory: %w", err)
	}

	skillFilePath := filepath.Join(skillsDir, "SKILL.md")
	content := fmt.Sprintf("# Universal Skill: %s\n\n## Description\n%s\n\n## Instructions & Best Practices\n%s\n", name, args.Description, args.Instructions)

	if err := os.WriteFile(skillFilePath, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("failed writing skill file: %w", err)
	}

	// Auto-index into global RAG vector store
	globalIndexPath := filepath.Join(homeDir, ".gokodek", "global_rag_index.json")
	embedder := rag.NewEmbedder(normalizeRAGEndpoint(t.ollamaEndpoint), t.embeddingModel)
	index := rag.NewIndex(globalIndexPath, embedder)
	_ = index.Load()

	ctx := context.Background()
	n, err := index.AddFile(ctx, skillFilePath, 800)
	if err == nil {
		_ = index.Save()
	}

	return fmt.Sprintf("Habilidad global universal %q guardada exitosamente en %s e indexada vectorialmente (%d fragmentos). Estará disponible automáticamente para cualquier proyecto vía rag_search.", name, skillFilePath, n), nil
}
