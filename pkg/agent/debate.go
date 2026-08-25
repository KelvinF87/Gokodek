package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AgentRole represents the specialized role of a debate agent.
type AgentRole struct {
	Name        string
	Icon        string
	SystemVoice string
}

// Predefined agent roles for the debate system.
var (
	RoleBoss = AgentRole{
		Name:        "JEFE_LEAD",
		Icon:        "👑",
		SystemVoice: "Eres el Jefe Técnico y Arquitecto Principal. Tu objetivo es revisar las propuestas de los agentes críticos especialistas, exigir máxima calidad, tomar la decisión final y dar el VISTO BUENO oficial cuando la solución sea 100% impecable y ejecutable.",
	}
	RoleArchitect = AgentRole{
		Name:        "Arquitecto_Estructura",
		Icon:        "🏗️",
		SystemVoice: "Eres el Arquitecto de Software Crítico. Te enfocas en patrones de diseño, mantenibilidad a largo plazo y acoplamiento limpio. Rechazas parches superficiales o código que no sea escalable.",
	}
	RoleBackendSecurity = AgentRole{
		Name:        "Experto_Backend_Seguridad",
		Icon:        "🛡️",
		SystemVoice: "Eres el Especialista Crítico en Backend y Seguridad. Buscas vulnerabilidades, fallos en APIs, concurrencia, fugas de memoria y rendimiento en base de datos o lógica de servidor.",
	}
	RoleFrontendUX = AgentRole{
		Name:        "Experto_Frontend_UX",
		Icon:        "🎨",
		SystemVoice: "Eres el Especialista Crítico en Frontend y Experiencia de Usuario. Exiges una estética visual deslumbrante, rendimiento a 60fps, responsividad impecable y animación fluida.",
	}
	RoleTester = AgentRole{
		Name:        "Experto_QA_Pruebas",
		Icon:        "🧪",
		SystemVoice: "Eres el Ingeniero de QA y Pruebas Automáticas. Verificas casos de borde, manejo de excepciones y pruebas concretas de verificación antes de aprobar cualquier cambio.",
	}
)

// DebateMessage is a single turn in the multi-agent discussion.
type DebateMessage struct {
	Agent   string    `json:"agent"`
	Icon    string    `json:"icon"`
	Content string    `json:"content"`
	Round   int       `json:"round"`
	Time    time.Time `json:"time"`
}

// DebateConfig controls the multi-agent discussion behavior.
type DebateConfig struct {
	MaxRounds     int
	Agents        []AgentRole
	Topic         string
	MaxTokensEach int
	// Context is real information about the workspace (files, stack, existing
	// code) that grounds the discussion so agents stop speaking in generic advice.
	Context string
}

// DefaultDebateConfig returns a sensible default debate setup with critical experts and the Boss.
func DefaultDebateConfig(topic string) DebateConfig {
	return DebateConfig{
		MaxRounds:     2,
		Agents:        []AgentRole{RoleArchitect, RoleBackendSecurity, RoleFrontendUX, RoleTester, RoleBoss},
		Topic:         topic,
		MaxTokensEach: 600,
	}
}

// DebateEngine orchestrates a multi-agent discussion.
type DebateEngine struct {
	Client   *OllamaClient
	Model    string
	Config   DebateConfig
	Messages []DebateMessage
}

// NewDebateEngine creates a new debate engine with the given configuration.
func NewDebateEngine(client *OllamaClient, model string, config DebateConfig) *DebateEngine {
	return &DebateEngine{
		Client: client,
		Model:  model,
		Config: config,
	}
}

// Run executes the multi-agent debate and returns the final consensus.
func (d *DebateEngine) Run(ctx context.Context, onStream func(DebateMessage), onComplete func(DebateMessage)) (string, error) {
	if len(d.Config.Agents) == 0 {
		return "", fmt.Errorf("no hay agentes configurados para el debate")
	}

	var discussion []string
	discussion = append(discussion, fmt.Sprintf("TEMA DE DISCUSIÓN: %s", d.Config.Topic))
	if d.Config.Context != "" {
		discussion = append(discussion, fmt.Sprintf("CONTEXTO DEL PROYECTO:\n%s", d.Config.Context))
	}

	for round := 1; round <= d.Config.MaxRounds; round++ {
		for _, agent := range d.Config.Agents {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}

			systemPrompt := fmt.Sprintf(`%s

Estás participando en un DEBATE TÉCNICO PROFESIONAL de resolución de problemas.

TEMA: %s

CONTEXTO REAL DEL PROYECTO (archivos en el workspace):
%s

Transcripción del debate previo:
%s

REGLAS DE INTERVENCIÓN (SIEMPRE EN ESPAÑOL):
- Si eres un AGENTE CRÍTICO ESPECIALISTA: Analiza desde tu área de especialidad. Identifica errores técnicos reales, problemas de seguridad o fallos visuales. Propón soluciones concretas especificando rutas de archivos exactas.
- Si eres el JEFE_LEAD (👑): Revisa los argumentos de los críticos especialistas. Aprueba o corrige su enfoque. Si la solución es óptima y resuelve el problema al 100%%, otorga explícitamente el "VISTO BUENO OFICIAL 👑".
- Sé directo, conciso y técnico. Máximo 2 párrafos cortos y termina con una lista de "ACCIONES CONCRETAS:".`, agent.SystemVoice, d.Config.Topic, d.Config.Context, strings.Join(discussion, "\n"))

			var currentMessage strings.Builder
			msg := DebateMessage{
				Agent: agent.Name,
				Icon:  agent.Icon,
				Round: round,
				Time:  time.Now(),
			}

			options := map[string]interface{}{
				"num_predict": d.Config.MaxTokensEach,
			}

			final, _, err := d.Client.ChatStream(ctx, ChatRequest{
				Model:    d.Model,
				Messages: []Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: "Presenta tu análisis técnico y propuesta resolutiva ahora."}},
				Options:  options,
			}, func(content string) {
				currentMessage.WriteString(content)
				msg.Content = currentMessage.String()
				if onStream != nil {
					onStream(msg)
				}
			}, nil)

			if err != nil {
				return "", fmt.Errorf("agente %s falló: %w", agent.Name, err)
			}

			content := final.Content
			if content == "" {
				content = currentMessage.String()
			}
			msg.Content = content
			msg.Round = round

			d.Messages = append(d.Messages, msg)
			discussion = append(discussion, fmt.Sprintf("[%s %s]: %s", agent.Icon, agent.Name, content))

			if onComplete != nil {
				onComplete(msg)
			}
		}
	}

	// Synthesize final consensus led by the Boss
	consensus, err := d.synthesize(ctx, discussion)
	if err != nil {
		return d.buildSummary(), nil
	}
	return consensus, nil
}

// synthesize produce el plan resolutivo final firmado por el Jefe.
func (d *DebateEngine) synthesize(ctx context.Context, discussion []string) (string, error) {
	systemPrompt := fmt.Sprintf(`Eres el JEFE TÉCNICO PRINCIPAL (👑 LEAD ARCHITECT BOSS). Has moderado el debate de los agentes críticos especialistas.

TEMA EVALUADO: %s

CONTEXTO DEL PROYECTO:
%s

Transcripción completa del debate:
%s

Produce la RESOLUCIÓN FINAL EN ESPAÑOL con la siguiente estructura exacta:

👑 VISTO BUENO DEL JEFE TÉCNICO:
[Párrafo resolutivo otorgando el visto bueno y confirmando la solución integral aprobada].

📋 PLAN DE EJECUCIÓN RESOLUTIVO:
1. <Paso técnico concreto> (Archivo: <ruta_de_archivo>)
2. <Paso técnico concreto> (Archivo: <ruta_de_archivo>)
3. <Paso técnico concreto> (Archivo: <ruta_de_archivo>)

🧪 PRUEBAS Y VERIFICACIÓN EXIGIDA:
[Comando o verificación concreta obligatoria: run_cmd, go test, check_web, browser_screenshot, etc.]`, d.Config.Topic, d.Config.Context, strings.Join(discussion, "\n"))

	response, err := d.Client.Chat(ctx, ChatRequest{
		Model:    d.Model,
		Messages: []Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: "Genera la resolución y plan de ejecución final ahora."}},
		Options: map[string]interface{}{
			"num_predict": 750,
		},
	})
	if err != nil {
		return "", err
	}
	return response.Message.Content, nil
}

// buildSummary creates a basic summary from the debate messages.
func (d *DebateEngine) buildSummary() string {
	var sb strings.Builder
	sb.WriteString("## Debate Summary\n\n")
	for _, msg := range d.Messages {
		sb.WriteString(fmt.Sprintf("**%s %s** (Round %d):\n%s\n\n", msg.Icon, msg.Agent, msg.Round, msg.Content))
	}
	return sb.String()
}

// FormatDebateMessage formats a single debate message for terminal display.
func FormatDebateMessage(msg DebateMessage) string {
	return fmt.Sprintf("\n%s┌─ %s %s (round %d) ─┐%s\n%s│%s %s\n%s└─────────────────────────────────────┘%s",
		ansiDim, msg.Icon, msg.Agent, msg.Round, ansiReset,
		ansiDim, ansiReset, msg.Content,
		ansiDim, ansiReset,
	)
}

// ParseDebateTopic extracts a debate topic from a /talk command.
func ParseDebateTopic(input string) string {
	topic := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(input), "/talk"))
	topic = strings.TrimSpace(strings.TrimPrefix(topic, "debate"))
	topic = strings.TrimSpace(strings.TrimPrefix(topic, " "))
	if topic == "" {
		return ""
	}
	return topic
}

// DebateRequest represents a structured debate request parsed from user input.
type DebateRequest struct {
	Topic     string
	MaxRounds int
	Agents    []string
}

// ParseDebateRequest parses a /talk command with optional parameters.
func ParseDebateRequest(input string) DebateRequest {
	req := DebateRequest{
		MaxRounds: 2,
		Agents:    []string{"architect", "backend", "frontend", "tester", "boss"},
	}

	body := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(input), "/talk"))
	body = strings.TrimSpace(strings.TrimPrefix(body, "debate"))
	body = strings.TrimSpace(strings.TrimPrefix(body, " "))

	if body == "" {
		return req
	}

	// Try to parse JSON parameters
	if strings.HasPrefix(body, "{") {
		var parsed struct {
			Topic     string   `json:"topic"`
			MaxRounds int      `json:"max_rounds"`
			Agents    []string `json:"agents"`
		}
		if err := json.Unmarshal([]byte(body), &parsed); err == nil {
			if parsed.Topic != "" {
				req.Topic = parsed.Topic
			}
			if parsed.MaxRounds > 0 && parsed.MaxRounds <= 10 {
				req.MaxRounds = parsed.MaxRounds
			}
			if len(parsed.Agents) > 0 {
				req.Agents = parsed.Agents
			}
			return req
		}
	}

	// Plain text topic
	req.Topic = body
	return req
}

// ResolveAgentRoles converts agent name strings to AgentRole structs.
func ResolveAgentRoles(names []string) []AgentRole {
	roleMap := map[string]AgentRole{
		"architect": RoleArchitect,
		"backend":   RoleBackendSecurity,
		"frontend":  RoleFrontendUX,
		"tester":    RoleTester,
		"boss":      RoleBoss,
		"jefe":      RoleBoss,
	}

	var roles []AgentRole
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if role, ok := roleMap[name]; ok && !seen[name] {
			roles = append(roles, role)
			seen[name] = true
		}
	}
	if len(roles) == 0 {
		roles = []AgentRole{RoleArchitect, RoleBackendSecurity, RoleFrontendUX, RoleTester, RoleBoss}
	}
	return roles
}
