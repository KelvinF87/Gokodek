package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CreateDocsTool struct {
	workspace string
}

func NewCreateDocsTool(workspace string) *CreateDocsTool {
	return &CreateDocsTool{workspace: workspace}
}

func (t *CreateDocsTool) Name() string { return "create_docs" }
func (t *CreateDocsTool) Description() string {
	return "Generate professional technical documentation, tutorials (subagents, skills, custom tools), or project reports in Markdown (.md) or HTML (.html formatted for PDF/Word export)."
}

func (t *CreateDocsTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"doc_type": map[string]interface{}{
			"type":        "string",
			"description": "Type of document: 'subagent_tutorial', 'skill_tutorial', 'tool_tutorial', 'project_report', or 'custom'.",
		},
		"title": map[string]interface{}{
			"type":        "string",
			"description": "Title of the document.",
		},
		"format": map[string]interface{}{
			"type":        "string",
			"description": "Output format: 'md' (Markdown) or 'html' (HTML printable to PDF/Word). Default: 'md'.",
		},
		"content": map[string]interface{}{
			"type":        "string",
			"description": "Custom content/sections to include (optional if using predefined tutorial types).",
		},
		"filename": map[string]interface{}{
			"type":        "string",
			"description": "Output filename (e.g. TUTORIAL_SUBAGENTS.md, DOCUMENTACION.html).",
		},
	})
}

func (t *CreateDocsTool) Execute(argsJSON string) (string, error) {
	var args struct {
		DocType  string `json:"doc_type"`
		Title    string `json:"title"`
		Format   string `json:"format"`
		Content  string `json:"content"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("create_docs arguments: %w", err)
	}

	docType := strings.ToLower(strings.TrimSpace(args.DocType))
	format := strings.ToLower(strings.TrimSpace(args.Format))
	if format == "" {
		format = "md"
	}

	filename := strings.TrimSpace(args.Filename)
	if filename == "" {
		if docType != "" {
			filename = docType + "." + format
		} else {
			filename = "DOCUMENTACION." + format
		}
	}

	var title string
	var body string

	switch docType {
	case "subagent_tutorial":
		title = "Tutorial: Creación y Uso de Subagentes en Gokodek"
		body = `# Tutorial: Creación y Uso de Subagentes

Los **Subagentes** permiten delegar tareas complejas o multitarea a instancias secundarias especializadas para mantener limpia la memoria de contexto del agente principal.

## 1. Concepto de Subagente
Un subagente es una ejecución autónoma del agente con un rol y prompt de sistema especializado (por ejemplo: *Refactoring Subagent*, *Test Subagent*, *Documentation Subagent*).

## 2. Pasos para Implementar un Subagente

1. **Definir el Rol y Prompt**:
   Crea un prompt de sistema conciso con las herramientas restringidas que necesita el subagente.

2. **Lanzar la Tarea**:
   El agente principal delega la sub-tarea especificando el objetivo claro.

3. **Recibir el Resultado**:
   El subagente ejecuta el bucle de herramientas e informa los resultados al agente principal antes de terminar.

## 3. Ejemplo de Uso
` + "```go" + `
subAgent := agent.NewAgent(client, registry, "qwen2.5-coder:3b", os.Stderr)
subAgent.SystemPrompt = "Eres un subagente especializado en refactorización de CSS."
result, err := subAgent.Run(ctx, &subHistory, "Refactoriza styles.css")
` + "```" + `
`
	case "skill_tutorial":
		title = "Tutorial: Creación de Habilidades Globales Universales (Skills)"
		body = `# Tutorial: Habilidades Globales Universales en Gokodek

Las **Skills Universales** son patrones de conocimiento, algoritmos o arquitecturas agnósticas de proyecto que se almacenan en la memoria global de Gokodek (` + "`~/.gokodek/skills/`" + `) y se indexan en el sistema RAG vectorial.

## 1. Crear una Habilidad Universal

Usa la herramienta ` + "`create_global_skill`" + `:

` + "```json" + `
{
  "name": "realistic_3d_city",
  "description": "Construcción de ciudades 3D realistas en Three.js con carreteras, parques y nubes",
  "instructions": "Pasos técnicos: 1. Definir rejilla con calles. 2. Instanciar edificios variados. 3. Añadir luz solar y nubes..."
}
` + "```" + `

## 2. Cómo Gokodek Recupera las Skills
Gokodek realiza búsquedas semánticas con ` + "`rag_search`" + ` e inyecta la skill universal automáticamente cuando el usuario pide una tarea relacionada, evitando saturar la ventana de contexto.
`

	case "tool_tutorial":
		title = "Tutorial: Creación de Herramientas Personalizadas en Go"
		body = `# Tutorial: Creación de Herramientas Personalizadas

Cada herramienta expuesta al agente implementa la interfaz ` + "`agent.Tool`" + ` en Go:

` + "```go" + `
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]interface{}
    Execute(argsJSON string) (string, error)
}
` + "```" + `

## 1. Pasos de Implementación
1. Crear el archivo en ` + "`pkg/tools/mi_herramienta.go`" + `.
2. Definir el nombre, descripción y esquema JSON de parámetros.
3. Implementar la función ` + "`Execute`" + `.
4. Registrar la herramienta en ` + "`main.go`" + ` con ` + "`registry.Register(...)`" + `.
`

	default:
		title = args.Title
		if title == "" {
			title = "Documentación Técnica de Proyecto"
		}
		body = args.Content
	}

	var output string
	if format == "html" {
		output = fmt.Sprintf(`<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <title>%s</title>
    <style>
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; line-height: 1.6; color: #1e293b; padding: 40px; max-width: 900px; margin: auto; }
        h1 { color: #4f46e5; border-bottom: 2px solid #e2e8f0; padding-bottom: 10px; }
        h2 { color: #0284c7; margin-top: 30px; }
        code { background: #f1f5f9; padding: 2px 6px; border-radius: 4px; font-family: monospace; }
        pre { background: #0f172a; color: #f8fafc; padding: 15px; border-radius: 8px; overflow-x: auto; }
        blockquote { border-left: 4px solid #6366f1; margin: 0; padding-left: 15px; color: #475569; }
    </style>
</head>
<body>
    <h1>%s</h1>
    <div>%s</div>
</body>
</html>`, title, title, strings.ReplaceAll(body, "\n", "<br>"))
	} else {
		output = fmt.Sprintf("# %s\n\n%s", title, body)
	}

	targetPath := filepath.Join(t.workspace, filename)
	if err := os.WriteFile(targetPath, []byte(output), 0600); err != nil {
		return "", fmt.Errorf("error guardando documentación: %w", err)
	}

	return fmt.Sprintf("Documento generado exitosamente en %s (Formato: %s).", targetPath, strings.ToUpper(format)), nil
}
