package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"gokodek/pkg/agent"
	"os"
	"path/filepath"
	"strings"
)

type InvokeDebateTool struct {
	workspace string
	agentRef  *agent.Agent
}

func NewInvokeDebateTool(workspace string, agentRef *agent.Agent) *InvokeDebateTool {
	return &InvokeDebateTool{
		workspace: workspace,
		agentRef:  agentRef,
	}
}

func (t *InvokeDebateTool) Name() string { return "invoke_expert_debate" }
func (t *InvokeDebateTool) Description() string {
	return "Auto-invokes the Multi-Agent Expert Committee (Architect, Backend/Security, Frontend/UX, QA, and Boss Lead) to debate a complex problem, evaluate architectural trade-offs, and produce an approved master plan."
}

func (t *InvokeDebateTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"topic": map[string]interface{}{
			"type":        "string",
			"description": "Specific problem or architecture decision to debate (e.g. 'How to optimize database queries for 4K Services' or 'Redesign Three.js lighting and city street grid').",
		},
	}, "topic")
}

func (t *InvokeDebateTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invoke_expert_debate arguments: %w", err)
	}

	topic := strings.TrimSpace(args.Topic)
	if topic == "" {
		return "", fmt.Errorf("topic cannot be empty")
	}

	client, ok := t.agentRef.Client.(*agent.OllamaClient)
	if !ok || client == nil {
		return "El debate multi-agente requiere un cliente Ollama activo.", nil
	}

	// Build workspace context text
	var sb strings.Builder
	_ = filepath.Walk(t.workspace, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(path, ".git") || strings.Contains(path, "node_modules") || strings.Contains(path, "dist") {
			return nil
		}
		rel, _ := filepath.Rel(t.workspace, path)
		if len(rel) < 80 {
			sb.WriteString("- " + rel + "\n")
		}
		return nil
	})

	config := agent.DefaultDebateConfig(topic)
	config.Context = sb.String()
	engine := agent.NewDebateEngine(client, t.agentRef.Model, config)

	consensus, err := engine.Run(context.Background(), nil, nil)
	if err != nil {
		return "", fmt.Errorf("error ejecutando debate de expertos: %w", err)
	}

	return fmt.Sprintf("=== DEBATE DE EXPERTOS Y RESOLUCIÓN DEL JEFE 👑 ===\n\n%s\n\n[INSTRUCCIÓN PARA EL AGENTE]: Procede a aplicar y verificar inmediatamente el plan resolutivo anterior.", consensus), nil
}
