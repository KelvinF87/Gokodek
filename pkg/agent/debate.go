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
	RoleArchitect = AgentRole{
		Name:        "architect",
		Icon:        "🏗️",
		SystemVoice: "You are the Architect. You focus on structure, design patterns, separation of concerns, and long-term maintainability. Challenge solutions that are hacky or will not scale.",
	}
	RoleReviewer = AgentRole{
		Name:        "reviewer",
		Icon:        "🔍",
		SystemVoice: "You are the Code Reviewer. You look for bugs, edge cases, security issues, and performance problems. Be critical but constructive.",
	}
	RoleImplementer = AgentRole{
		Name:        "implementer",
		Icon:        "⚡",
		SystemVoice: "You are the Implementer. You focus on practical, working solutions. Push for code that compiles, runs, and solves the actual problem. Counter over-engineering.",
	}
	RoleTester = AgentRole{
		Name:        "tester",
		Icon:        "🧪",
		SystemVoice: "You are the QA Engineer. You think about test coverage, edge cases, error handling, and verification strategies. Ensure the solution is reliable.",
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

// DefaultDebateConfig returns a sensible default debate setup.
func DefaultDebateConfig(topic string) DebateConfig {
	return DebateConfig{
		MaxRounds:     3,
		Agents:        []AgentRole{RoleArchitect, RoleReviewer, RoleImplementer},
		Topic:         topic,
		MaxTokensEach: 512,
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
// onStream is called incrementally as each agent writes, and onComplete once per
// finished agent contribution.
func (d *DebateEngine) Run(ctx context.Context, onStream func(DebateMessage), onComplete func(DebateMessage)) (string, error) {
	if len(d.Config.Agents) == 0 {
		return "", fmt.Errorf("no agents configured for debate")
	}

	var discussion []string
	discussion = append(discussion, fmt.Sprintf("TOPIC: %s", d.Config.Topic))
	if d.Config.Context != "" {
		discussion = append(discussion, fmt.Sprintf("PROJECT CONTEXT:\n%s", d.Config.Context))
	}

	for round := 1; round <= d.Config.MaxRounds; round++ {
		for _, agent := range d.Config.Agents {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}

			// Build context for this agent. The prompt demands concrete proposals
			// grounded in the real project, not generic best-practice advice.
			systemPrompt := fmt.Sprintf(`%s

You are participating in a multi-agent discussion about a real project.

TOPIC: %s

PROJECT CONTEXT (real files in the workspace):
%s

Previous discussion:
%s

Your rules:
- Ground every claim in the PROJECT CONTEXT above. Never give generic advice like "use git" or "write unit tests" without tying it to a specific file or behavior in this project.
- Propose CONCRETE actions: exact file names, exact changes, exact functions to add or modify.
- Keep it to 2-4 short paragraphs.
- If you agree with a previous agent, say so in one line and add one new concrete detail. If you disagree, explain with specific reasoning about the code.
- End with a numbered list "ACCIONES:" of the 2-4 concrete steps you recommend, each referencing a real file.`, agent.SystemVoice, d.Config.Topic, d.Config.Context, strings.Join(discussion, "\n"))

			var currentMessage strings.Builder
			msg := DebateMessage{
				Agent: agent.Name,
				Icon:  agent.Icon,
				Round: round,
				Time:  time.Now(),
			}

			// Use options to limit tokens
			options := map[string]interface{}{
				"num_predict": d.Config.MaxTokensEach,
			}

			final, _, err := d.Client.ChatStream(ctx, ChatRequest{
				Model:    d.Model,
				Messages: []Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: "Provide your analysis now."}},
				Options:  options,
			}, func(content string) {
				currentMessage.WriteString(content)
				msg.Content = currentMessage.String()
				if onStream != nil {
					onStream(msg)
				}
			}, nil)

			if err != nil {
				return "", fmt.Errorf("agent %s failed: %w", agent.Name, err)
			}

			content := final.Content
			if content == "" {
				content = currentMessage.String()
			}
			msg.Content = content
			msg.Round = round

			d.Messages = append(d.Messages, msg)
			discussion = append(discussion, fmt.Sprintf("[%s]: %s", agent.Name, content))

			if onComplete != nil {
				onComplete(msg)
			}
		}
	}

	// Synthesize final consensus
	consensus, err := d.synthesize(ctx, discussion)
	if err != nil {
		return d.buildSummary(), nil
	}
	return consensus, nil
}

// synthesize asks one final round for a consensus summary that is a concrete
// implementation plan, not generic advice.
func (d *DebateEngine) synthesize(ctx context.Context, discussion []string) (string, error) {
	systemPrompt := fmt.Sprintf(`You are a neutral moderator turning a multi-agent discussion into an executable implementation plan.

The discussion was about: %s

PROJECT CONTEXT:
%s

Discussion transcript:
%s

Produce a final plan with EXACTLY this structure, in plain text:

DECISIÓN: one paragraph stating the agreed approach.

PLAN DE IMPLEMENTACIÓN:
1. <concrete step> (file: <real file name>)
2. <concrete step> (file: <real file name>)
3. <concrete step> (file: <real file name>)

VERIFICACIÓN: the exact command or check (e.g. run_test, check_web, go build) that proves the work is done.

Rules: every step must reference a real file from PROJECT CONTEXT. No generic advice. If the discussion was too vague, say so and list the files the agents should have inspected first.`, d.Config.Topic, d.Config.Context, strings.Join(discussion, "\n"))

	response, err := d.Client.Chat(ctx, ChatRequest{
		Model:    d.Model,
		Messages: []Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: "Produce the implementation plan now."}},
		Options: map[string]interface{}{
			"num_predict": 700,
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
		MaxRounds: 3,
		Agents:    []string{"architect", "reviewer", "implementer"},
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
		"architect":   RoleArchitect,
		"reviewer":    RoleReviewer,
		"implementer": RoleImplementer,
		"tester":      RoleTester,
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
		roles = []AgentRole{RoleArchitect, RoleReviewer, RoleImplementer}
	}
	return roles
}
