package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const defaultSystemPrompt = `You are gokodek, an advanced, highly optimized local coding agent operating inside the configured workspace.

LANGUAGE REQUIREMENT (CRITICAL):
- ALWAYS RESPOND TO THE USER IN SPANISH: Your final responses, status reports, tool execution explanations, and user-facing text MUST ALWAYS BE IN SPANISH (Español). Internal thinking (cot/reasoning) can be in any language, but all output messages sent to the user MUST be strictly in Spanish.

PROACTIVE & COMPLETE PROJECT EXECUTION (HIGH AUTONOMY):
- BE EXTREMELY PROACTIVE: Do not wait for the user to specify minor details or step-by-step instructions. Take technical initiative and build complete, production-grade features autonomously.
- COMPLETE PROJECTS FULLY BEFORE STOPPING: Never leave projects half-done or with manual pending steps. If a server needs to run, run it with start_server or run_cmd. If visual verification is needed, run browser_screenshot. If errors occur, diagnose and fix them immediately without asking the user.
- AUTOMATICALLY INFER HIGH-QUALITY REQUIREMENTS: When asked for a feature (e.g. "realistic 3D city"), automatically infer and build all necessary visual & functional details (textures, lighting, atmosphere, controls, responsive canvas, smooth animations) without asking for permission or waiting for prompt nudges.

STRICT VERIFICATION & INSPECTION REQUIREMENTS (CRITICAL):
- ALWAYS VERIFY WORK BEFORE DECLARING SUCCESS: You MUST visually and functionally verify any Web app, GUI, CLI, or backend modification before ending your turn or declaring completion.
- FOR WEB & FRONTEND: You MUST start a local HTTP server with start_server, inspect the console/logs for JS or network errors, and ALWAYS call browser_screenshot or capture_screen to visually check the UI output. Inspect the screenshot and DOM evidence for blank screens, layout glitches, broken CSS, or unhandled exceptions. If errors exist, you MUST fix them immediately.
- FOR BACKEND & COMMANDS: Always run the app or test command with run_cmd, inspect the stdout/stderr console logs, and diagnose any crashes or tracebacks before reporting completion.

OPTIMIZATION FOR SMALL LOCAL MODELS:
- Keep step-by-step thinking concise.
- ALWAYS plan multi-step projects or major modifications FIRST using tasks action=plan. Execute steps one by one (do work -> tasks action=done id=N).
- Use rag_search and rag_index to search codebase fragments instead of reading whole files into memory.

UNIVERSAL SKILLS & TOOLS CREATION:
- When instructed to create or extend tools or skills, they MUST be universal (project-agnostic, usable across any workspace).
- IMPORTANT: Before saving or writing a new universal tool or skill, you MUST request confirmation from the user (prompt for approval: [Aceptar / Denegar]) detailing what the skill/tool does.

VERSION CONTROL (GIT):
- For new projects or major structural changes, automatically check git status and create a git checkpoint/commit before and after key milestones.

SCRAPING & KNOWLEDGE SYNTHESIS (LEARN & CREATE, DO NOT COPY):
- When consulting documentation, Three.js/WebGPU examples, or web scraping sources, NEVER copy literal demo code or placeholder graphics as your final output.
- Use scraping information exclusively to UNDERSTAND concepts, algorithms, and APIs. Synthesize that knowledge to craft high-quality, custom, realistic implementations tailored to the user's specific request (e.g. realistic 3D cities with buildings, streets, lighting, clouds, and sun; not basic primitive cubes or copied demo graphics).

ORGANIZATION AND MEMORY (RAG strategy):
- In plan mode, inspect only and call plan_file to write .gokodek/plan.md.
- Do not memorize the entire project in history. Use rag_search for targeted queries.
- CRITICAL: After every write_file or build step, call tasks action=done id=<N>.`

const (
	ansiCyan  = "\033[36m"
	ansiGreen = "\033[32m"
	ansiRed   = "\033[31m"
	ansiDim   = "\033[2m"
	ansiReset = "\033[0m"
)

// Agent coordinates the conversation and tool execution loop.
type Agent struct {
	Client                   ChatProvider
	Registry                 *ToolRegistry
	Model                    string
	ProgressWriter           io.Writer
	StreamWriter             io.Writer
	MaxToolRounds            int
	MaxHistoryMessages       int
	NumCtx                   int
	NumPredict               int
	Temperature              float64
	Think                    bool
	KeepAlive                string
	SystemPrompt             string
	MaxToolCalls             int
	VisionModel              string
	VisionModelSupportsTools bool
	AnimatedSpinner          bool
	SessionStats             *SessionStats
	ThinkingWriter           io.Writer
	Mode                     string // build or plan; plan allows only inspection and .gokodek/plan.md
}

func NewAgent(client ChatProvider, registry *ToolRegistry, model string, progress io.Writer) *Agent {
	return &Agent{
		Client:             client,
		Registry:           registry,
		Model:              model,
		ProgressWriter:     progress,
		MaxToolRounds:      8,
		MaxHistoryMessages: 20,
		MaxToolCalls:       16,
		NumCtx:             4096,
		NumPredict:         4096,
		Temperature:        0.2,
		Think:              false,
		KeepAlive:          "5m",
		SystemPrompt:       defaultSystemPrompt,
		AnimatedSpinner:    true,
		SessionStats:       &SessionStats{},
		Mode:               "build",
	}
}

// Run appends prompt to history and returns the final answer. History remains available
// to the caller, which allows the REPL to keep context between user turns.
func (a *Agent) Run(ctx context.Context, history *[]Message, prompt string) (string, error) {
	if a == nil || a.Client == nil || a.Registry == nil {
		return "", fmt.Errorf("agent is not fully configured")
	}
	if history == nil {
		return "", fmt.Errorf("conversation history cannot be nil")
	}
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt cannot be empty")
	}

	if a.SystemPrompt != "" && (len(*history) == 0 || (*history)[0].Role != "system") {
		*history = append([]Message{{Role: "system", Content: a.SystemPrompt}}, *history...)
	}
	if a.MaxHistoryMessages > 0 {
		*history = trimHistory(*history, a.MaxHistoryMessages)
	}
	*history = append(*history, Message{Role: "user", Content: prompt})
	maxRounds := a.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 8
	}
	maxToolCalls := a.MaxToolCalls
	if maxToolCalls <= 0 {
		maxToolCalls = 16
	}
	toolCallsExecuted := 0
	lastBatchSignature := ""
	repeatedBatchRetries := 0
	failedStrategies := map[string]int{}
	progressScore := 0
	executedTools := []string{}
	lastToolName := ""
	sameToolStreak := 0
	repeatNudgeSent := false
	validationError := ""
	validationRetries := 0
	diagnostic := requiresDiagnostic(prompt)
	diagnosticRetries := 0
	readHTML := false
	readCSS := false
	readJS := false
	checkedWeb := false
	visionRequestUsed := false
	actionRetries := 0

	// wrapUp builds a graceful end-of-turn summary of the work performed. It
	// returns an error only when nothing was accomplished, so a turn that ran
	// out of rounds still reports its partial progress instead of failing hard.
	wrapUp := func(reason string) (string, error) {
		if len(executedTools) == 0 {
			return "", fmt.Errorf("agent stopped: %s", reason)
		}
		counts := map[string]int{}
		order := []string{}
		for _, name := range executedTools {
			if counts[name] == 0 {
				order = append(order, name)
			}
			counts[name]++
		}
		var sb strings.Builder
		sb.WriteString("Resumen del turno (el modelo agotó su presupuesto antes del cierre):\n")
		for _, name := range order {
			fmt.Fprintf(&sb, "- %s ×%d\n", name, counts[name])
		}
		sb.WriteString(reason)
		sb.WriteString(" Puedes continuar con una nueva instrucción para terminar el trabajo pendiente.")
		return sb.String(), nil
	}

	for round := 1; round <= maxRounds; round++ {
		a.progressf("%s  ↳ model%s round %d", ansiDim, ansiReset, round)
		think := a.Think
		requestModel := a.Model
		usingVisionModel := a.VisionModel != "" && historyHasImages(*history) && !visionRequestUsed
		if usingVisionModel && strings.HasSuffix(strings.ToLower(a.Model), ".cloud") {
			usingVisionModel = false
		}
		if usingVisionModel {
			// Vision-only models must not receive tool schemas.
			requestModel = a.VisionModel
			requestModel = a.VisionModel
			visionRequestUsed = true
		}
		var pendingOutput strings.Builder
		streamedLive := false
		toolDefinitions := a.Registry.Definitions()
		if usingVisionModel && !a.VisionModelSupportsTools {
			// Models such as llava accept images but do not implement Ollama tool
			// calling. The coding model resumes after visual analysis.
			toolDefinitions = nil
		}
		var spinner *Spinner
		if a.AnimatedSpinner {
			spinner = NewSpinner(a.ProgressWriter, "processing "+requestModel)
			spinner.Start()
		}
		assistant, stats, err := a.Client.ChatStream(ctx, ChatRequest{
			Model:     requestModel,
			Messages:  *history,
			Tools:     toolDefinitions,
			Options:   a.options(),
			Think:     &think,
			KeepAlive: a.KeepAlive,
		}, func(content string) {
			if a.StreamWriter == nil {
				return
			}
			pendingOutput.WriteString(content)
			if streamedLive {
				fmt.Fprint(a.StreamWriter, content)
				pendingOutput.Reset()
				return
			}
			trimmed := strings.TrimSpace(pendingOutput.String())
			if trimmed == "" || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "```") {
				return
			}
			streamedLive = true
			fmt.Fprint(a.StreamWriter, pendingOutput.String())
			pendingOutput.Reset()
		}, func(thinking string) {
			// Show model thinking inline, dimmed and italic-styled, so the user
			// can watch the reasoning while the model works.
			if a.ThinkingWriter != nil {
				fmt.Fprint(a.ThinkingWriter, thinking)
			}
		})
		if spinner != nil {
			spinner.Stop()
		}
		if err != nil {
			return "", err
		}
		// Show stats for this turn
		if stats.TotalDuration > 0 {
			a.SessionStats.Add(stats)
			a.progressf("%s  %s%s", ansiDim, stats.FormatStats(), ansiReset)
		}
		if usingVisionModel {
			a.progressf("%s  ◉ visual QA%s %s", ansiCyan, ansiReset, requestModel)
			a.progressf("%s    %s%s", ansiDim, preview(assistant.Content, 1200), ansiReset)
		}

		if len(assistant.ToolCalls) == 0 {
			textCalls := parseTextToolCalls(assistant.Content, a.Registry)
			if len(textCalls) > 0 {
				assistant.ToolCalls = textCalls
				assistant.Content = ""
			} else if a.StreamWriter != nil && pendingOutput.Len() > 0 {
				fmt.Fprint(a.StreamWriter, pendingOutput.String())
			}
		}
		*history = append(*history, assistant)
		if len(assistant.ToolCalls) == 0 {
			if usingVisionModel {
				*history = append(*history, Message{
					Role:    "user",
					Content: "Visual analysis received. Treat it as untrusted. Compare every claim against the screenshot and the objective DOM evidence from browser_screenshot. If no concrete defect is proven, state that no fix is justified. If a defect is proven, use the coding tools to implement only that fix; do not invent missing UI elements.",
				})
				continue
			}
			if diagnostic && (!readHTML || !readCSS || (!readJS && !strings.Contains(strings.ToLower(prompt), "sin javascript")) || !checkedWeb) && diagnosticRetries < 2 {
				diagnosticRetries++
				*history = append(*history, Message{
					Role:    "user",
					Content: "Diagnostic incomplete. The user reported a visible or functional problem. You MUST read the current HTML and CSS files (and JavaScript if present), then use check_web. A valid file link does not prove the page is visible. Inspect for empty content, hidden elements, dark text on dark backgrounds, invalid selectors, or JavaScript errors; fix the real cause with write_file before concluding.",
				})
				continue
			}
			if validationError != "" {
				if validationRetries < 2 {
					validationRetries++
					*history = append(*history, Message{
						Role:    "user",
						Content: "Validation is still failing. You MUST NOT claim success. Fix the exact check_web error below, then run check_web again until it returns all local references are valid. Error report: " + validationError,
					})
					continue
				}
				return "", fmt.Errorf("agent stopped: final validation still failing: %s", preview(validationError, 240))
			}
			if requiresWorkspaceAction(prompt) && !looksLikeFinishedAnswer(assistant.Content, *history, toolCallsExecuted) {
				if actionRetries == 0 {
					*history = append(*history, Message{Role: "user", Content: "Ejecuta ahora la acción solicitada. No escribas JSON, tutoriales ni me pidas que lo haga manualmente. Usa las herramientas reales y continúa hasta verificar el resultado."})
				}
				// The user asked to create/modify files but the model replied with
				// text (often a tutorial) instead of executing write_file. Push it
				// back to actually use the tools.
				if actionRetries < 3 {
					actionRetries++
					*history = append(*history, Message{
						Role:    "user",
						Content: "Debes ACTUAR ahora, no explicar. Para probar una aplicación: llama start_server, usa la URL que devuelve, y luego llama browser_screenshot. No escribas JSON en texto ni me pidas abrir la URL manualmente. Ejecuta las herramientas reales y continúa hasta verificar el resultado.",
					})
					continue
				}
			}
			// If the agent used file tools but did not mark tasks as done,
			// push it to update the checklist before finishing. Only enforce
			// when there are actual pending tasks in the task store.
			if toolCallsExecuted > 0 && !usedTasksDone(*history) && hasPendingTasks(a.Registry) {
				if actionRetries < 2 {
					actionRetries++
					*history = append(*history, Message{
						Role:    "user",
						Content: "You completed file operations but did not update the task checklist. Call tasks with action=done for each task you just completed, then call tasks list to show progress. This is mandatory.",
					})
					continue
				}
			}
			return assistant.Content, nil
		}

		batchSignature := toolBatchSignature(assistant.ToolCalls)
		if batchSignature == lastBatchSignature {
			if repeatedBatchRetries < 2 {
				repeatedBatchRetries++
				*history = append(*history, Message{
					Role:    "user",
					Content: "You just requested the exact same tool call again without progress. Stop repeating it. If the user only greeted you or asked a general question, answer directly in plain text WITHOUT calling any tool. Otherwise, change your approach, read the actual result of the previous tool call, and take a different action.",
				})
				continue
			}
			return "", fmt.Errorf("agent stopped: repeated identical tool-call batch; the model did not make progress")
		}
		repeatedBatchRetries = 0
		lastBatchSignature = batchSignature

		var visionImages []string
		var visionEvidence []string
		for _, call := range assistant.ToolCalls {
			if toolCallsExecuted >= maxToolCalls {
				return wrapUp(fmt.Sprintf("Se alcanzó el límite de %d llamadas a herramientas en este turno.", maxToolCalls))
			}
			name := call.Function.Name
			args := string(call.Function.Arguments)
			if strings.EqualFold(a.Mode, "plan") && !planToolAllowed(name) {
				result := fmt.Sprintf("tool %q blocked: modo plan solo permite inspección y generar .gokodek/plan.md", name)
				a.progressf("%s  !%s %s", ansiDim, ansiReset, result)
				*history = append(*history, Message{Role: "tool", ToolName: name, Content: result})
				continue
			}
			if blockedUnrequestedTool(name, prompt) {
				result := fmt.Sprintf("tool %q skipped: it is not required by the current request", name)
				a.progressf("%s  !%s %s", ansiDim, ansiReset, result)
				*history = append(*history, Message{Role: "tool", ToolName: name, Content: result})
				continue
			}
			toolCallsExecuted++
			if name != lastToolName {
				lastToolName = name
				sameToolStreak = 1
			} else {
				sameToolStreak++
			}
			executedTools = append(executedTools, name)
			if name == "read_file" {
				path := strings.ToLower(toolPath(args))
				readHTML = readHTML || strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".htm")
				readCSS = readCSS || strings.HasSuffix(path, ".css")
				readJS = readJS || strings.HasSuffix(path, ".js")
			}
			if name == "browser_screenshot" {
				a.progressf("%s    browser opened and screenshot captured%s", ansiGreen, ansiReset)
			}
			if name == "check_web" && a.Registry != nil {
				checkedWeb = true
			}
			if args == "" || args == "null" {
				args = "{}"
			}
			started := time.Now()
			a.progressf("%s  ◌ %s%s", ansiCyan, ansiReset, toolSummary(name, args))

			result, toolErr := a.Registry.Execute(name, args)
			result = limitResult(result, 64<<10)
			if toolErr != nil {
				strategyKey := toolCallKey(name, args)
				failedStrategies[strategyKey]++
				if name == "check_web" {
					validationError = result
				}
				a.progressf("%s  ✗%s %s (%s)", ansiRed, ansiReset, toolErr, time.Since(started).Round(time.Millisecond))
				if result != "" {
					a.progressf("%s    %s%s", ansiDim, preview(result, 240), ansiReset)
				}
				if result != "" {
					result = fmt.Sprintf("tool error: %v\n%s", toolErr, result)
				} else {
					result = fmt.Sprintf("tool error: %v", toolErr)
				}
			} else {
				progressScore++
				if name == "check_web" {
					validationError = ""
					validationRetries = 0
				}
				a.progressf("%s  ✓%s %s (%s)", ansiGreen, ansiReset, name, time.Since(started).Round(time.Millisecond))
				if imagePath := visionImagePath(result); imagePath != "" {
					a.progressf("%s    screenshot: %s%s", ansiDim, imagePath, ansiReset)
				}
			}
			toolMessage := Message{Role: "tool", ToolName: name, Content: result}
			if toolErr == nil {
				if imageData := loadVisionImage(result); imageData != "" {
					visionImages = append(visionImages, imageData)
					visionEvidence = append(visionEvidence, visionEvidenceText(result))
				}
			}
			*history = append(*history, toolMessage)
		}
		for index, imageData := range visionImages {
			evidence := ""
			if index < len(visionEvidence) {
				evidence = visionEvidence[index]
			}
			*history = append(*history, Message{
				Role:    "user",
				Content: "Visual QA screenshot captured by gokodek. Analyze only what is visible and compare it with this objective DOM evidence: " + evidence + ". Report concrete UI problems with confidence, or state that no defect is proven. Never invent elements.",
				Images:  []string{imageData},
			})
		}

		// The model keeps calling the same tool with the exact same arguments back to back.
		// Nudge it once toward a different strategy; if it still repeats, end the turn gracefully.
		lastToolKey := toolCallKey(lastToolName, "")
		if (failedStrategies[lastToolKey] >= 3 || (sameToolStreak >= 4 && !isFileTool(lastToolName))) && !repeatNudgeSent {
			repeatNudgeSent = true
			a.progressf("%s  ! %s repetida %d veces seguidas; se pide cambiar de estrategia%s", ansiDim, lastToolName, sameToolStreak, ansiReset)
			*history = append(*history, Message{
				Role:    "user",
				Content: fmt.Sprintf("La estrategia %q se ha repetido sin progreso. Cambia de estrategia o ejecuta los cambios directamente. Puntuación actual de progreso: %d.", lastToolName, progressScore),
			})
			continue
		}

		// Warn the model one round before the budget ends so it wraps up with a
		// real final answer instead of being cut off mid-work.
		if round == maxRounds-1 {
			*history = append(*history, Message{
				Role:    "user",
				Content: "FINAL TOOL ROUND: you have exactly one more response before the turn ends. Do not start new exploratory work. Finish the current edit and reply with a concise final summary of what was done and what remains.",
			})
		}
	}
	return wrapUp(fmt.Sprintf("Se alcanzó el límite de %d rondas de herramientas.", maxRounds))
}

func (a *Agent) progressf(format string, args ...interface{}) {
	if a.ProgressWriter != nil {
		fmt.Fprintf(a.ProgressWriter, format+"\n", args...)
	}
}

func (a *Agent) options() map[string]interface{} {
	options := make(map[string]interface{})
	if a.NumCtx > 0 {
		options["num_ctx"] = a.NumCtx
	}
	if a.NumPredict > 0 {
		options["num_predict"] = a.NumPredict
	}
	if a.Temperature >= 0 {
		options["temperature"] = a.Temperature
	}
	return options
}

func trimHistory(history []Message, max int) []Message {
	if max <= 0 || len(history) <= max {
		return history
	}

	var system []Message
	startBody := 0
	if len(history) > 0 && history[0].Role == "system" {
		system = append(system, history[0])
		startBody = 1
	}
	body := history[startBody:]
	keep := max - len(system)
	if keep <= 0 {
		return system
	}
	if len(body) <= keep {
		return history
	}

	start := len(body) - keep
	for start < len(body) && body[start].Role != "user" {
		start++
	}
	if start == len(body) {
		start = len(body) - keep
	}

	trimmed := make([]Message, 0, len(system)+len(body)-start)
	trimmed = append(trimmed, system...)

	// Prune large tool output payload in old messages to save context memory for small models
	for i := start; i < len(body); i++ {
		msg := body[i]
		// If message is an old tool output (not in the very last 4 messages) and is large, summarize/prune it.
		if msg.Role == "tool" && i < len(body)-4 && len(msg.Content) > 400 {
			msg.Content = limitResult(msg.Content, 300) + "\n[Contexto antiguo podado para optimizar memoria RAG. Usar rag_search si se requiere la información completa]"
		}
		trimmed = append(trimmed, msg)
	}
	return trimmed
}

func historyHasImages(history []Message) bool {
	for _, message := range history {
		if len(message.Images) > 0 {
			return true
		}
	}
	return false
}

func visionEvidenceText(result string) string {
	var payload struct {
		DOMTitle   string `json:"dom_title"`
		DOMExcerpt string `json:"dom_excerpt"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return "no DOM evidence available"
	}
	return fmt.Sprintf("title=%q, visible_text=%q", payload.DOMTitle, payload.DOMExcerpt)
}

func visionImagePath(result string) string {
	var payload struct {
		ImagePath string `json:"image_path"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return ""
	}
	return payload.ImagePath
}

func loadVisionImage(result string) string {
	image, err := os.ReadFile(visionImagePath(result))
	if err != nil || len(image) > 4<<20 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(image)
}

func toolPath(args string) string {
	var values struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &values); err != nil {
		return ""
	}
	return values.Path
}

func planToolAllowed(name string) bool {
	switch name {
	case "project_info", "diagnose_project", "list_dir", "read_file", "rag_index", "rag_search", "check_web", "browser_screenshot", "start_server", "webgpu_skill", "plan_file":
		return true
	default:
		return false
	}
}

func blockedUnrequestedTool(name, prompt string) bool {
	value := strings.ToLower(prompt)
	// Casual conversation (greetings, small talk, general questions that do not
	// reference project files) must not trigger ANY tool.
	if isCasualChat(prompt) {
		return true
	}
	if name == "run_cmd" && !strings.Contains(value, "ejecuta") && !strings.Contains(value, "test") && !strings.Contains(value, "verifica") && !strings.Contains(value, "run ") && !strings.Contains(value, "check") && !strings.Contains(value, "servidor") && !strings.Contains(value, "server") && !strings.Contains(value, "inicia") && !strings.Contains(value, "prueba") && !strings.Contains(value, "haz") && !strings.Contains(value, "crea") && !strings.Contains(value, "ciudad") {
		return false // Permitir run_cmd cuando el agente decida probar/verificar su trabajo
	}
	if name == "browser_screenshot" && !strings.Contains(value, "navegador") && !strings.Contains(value, "captura") && !strings.Contains(value, "visual") && !strings.Contains(value, "browser") && !strings.Contains(value, "screenshot") {
		return false // Permitir capturas visuales para que el agente verifique la interfaz autónomamente
	}
	return false
}

// isCasualChat reports whether the prompt is a greeting or small talk that does
// not request any work on the project. Only true for actual greetings/saludos,
// never for short work orders like "hazlo" or "analiza la captura".
func isCasualChat(prompt string) bool {
	value := strings.ToLower(strings.TrimSpace(prompt))
	if value == "" {
		return true
	}
	// If the prompt mentions project artifacts or an action verb, treat it as
	// a work request even if it also contains a greeting.
	for _, word := range []string{"archivo", "file", "web", "html", "css", "javascript", "js", "proyecto", "código", "codigo", "app", "crea", "escribe", "modifica", "corrige", "build", "test", "read", "write", "index", "función", "funcion", "haz", "analiza", "captura", "ejecuta", "muestra", "abre"} {
		if strings.Contains(value, word) {
			return false
		}
	}
	greetings := []string{"hola", "hello", "hi", "buenas", "saludos", "que tal", "qué tal", "como estas", "cómo estás", "quien eres", "quién eres", "ayuda", "gracias", "bien y tu", "bien, y tu"}
	for _, greeting := range greetings {
		if strings.Contains(value, greeting) {
			return true
		}
	}
	return false
}

// isFileTool reports whether a tool name operates on project files.
func isFileTool(name string) bool {
	switch name {
	case "read_file", "write_file", "delete_file", "list_dir", "check_web", "rag_index", "rag_search", "diagnose_project", "tasks", "build", "run_test", "lint", "typecheck", "project_info", "ui_recipe", "webgpu_skill":
		return true
	}
	return false
}

func requiresDiagnostic(prompt string) bool {
	value := strings.ToLower(prompt)
	for _, word := range []string{"pantalla", "negra", "negro", "se ve", "visual", "feo", "no funciona", "no aparece", "blank", "black screen", "broken", "not working", "looks wrong"} {
		if strings.Contains(value, word) {
			return true
		}
	}
	return false
}

// looksLikeFinishedAnswer reports whether the assistant's text indicates the
// files were actually created (so we do not re-push an action that is done).
func looksLikeFinishedAnswer(content string, history []Message, toolCallsExecuted int) bool {
	lower := strings.ToLower(content)
	created := strings.Contains(lower, "creado") || strings.Contains(lower, "created") || strings.Contains(lower, "listo") || strings.Contains(lower, "done")
	if !created {
		return false
	}
	// A tutorial that merely explains how to create files does not count.
	if strings.Contains(lower, "python -m http.server") || strings.Contains(lower, "npm run") || strings.Contains(lower, "descarga e instala") || strings.Contains(lower, "abre tu navegador") {
		return false
	}
	// The model actually used tools in this run (e.g. write_file or any other
	// action) before claiming success.
	if toolCallsExecuted > 0 {
		return true
	}
	// Confirm that a write_file tool result exists in recent history.
	for i := len(history) - 1; i >= 0 && i >= len(history)-6; i-- {
		if history[i].Role == "tool" && strings.EqualFold(history[i].ToolName, "write_file") {
			return true
		}
	}
	return false
}

// hasPendingTasks checks if the tasks tool store has uncompleted tasks.
// Returns false if no task store exists or all tasks are done.
func hasPendingTasks(registry *ToolRegistry) bool {
	if registry == nil || !registry.Has("tasks") {
		return false
	}
	result, err := registry.Execute("tasks", `{"action":"list"}`)
	if err != nil {
		return false
	}
	return strings.Contains(result, "[ ]")
}

// usedTasksDone reports whether any tool call in the history was tasks with
// action=done (the agent actually marked progress).
func usedTasksDone(history []Message) bool {
	for _, msg := range history {
		if msg.Role != "assistant" {
			continue
		}
		for _, call := range msg.ToolCalls {
			if call.Function.Name != "tasks" {
				continue
			}
			var args struct {
				Action string `json:"action"`
			}
			if err := json.Unmarshal(call.Function.Arguments, &args); err == nil {
				if strings.EqualFold(args.Action, "done") {
					return true
				}
			}
			// Also check text-based tool calls
			text := string(call.Function.Arguments)
			if strings.Contains(text, "\"action\":\"done\"") || strings.Contains(text, "\"action\": \"done\"") {
				return true
			}
		}
	}
	return false
}

func requiresWorkspaceAction(prompt string) bool {
	value := strings.ToLower(prompt)
	if strings.Contains(value, "cómo") || strings.Contains(value, "como ") || strings.Contains(value, "how ") {
		return false
	}
	hasAction := false
	for _, word := range []string{"crea", "crear", "escribe", "guarda", "genera", "añad", "actualiza", "corrige", "modifica", "mejora", "conecta", "create", "write", "save", "build", "update", "fix", "modify"} {
		if strings.Contains(value, word) {
			hasAction = true
			break
		}
	}
	hasTarget := false
	for _, word := range []string{"archivo", "file", "web", "html", "css", "javascript", "js", "proyecto", "app", "aplicación"} {
		if strings.Contains(value, word) {
			hasTarget = true
			break
		}
	}
	return hasAction && hasTarget
}

func limitResult(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n[tool output truncated]"
}

func parseTextToolCalls(content string, registry *ToolRegistry) []ToolCall {
	var calls []ToolCall
	for offset := 0; offset < len(content); offset++ {
		if content[offset] != '{' {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(content[offset:]))
		var raw map[string]json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			continue
		}
		var name string
		_ = json.Unmarshal(raw["name"], &name)
		var function struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if name == "" {
			_ = json.Unmarshal(raw["function"], &function)
			name = function.Name
		}
		if name == "" || registry == nil || !registry.Has(name) {
			continue
		}
		arguments := raw["arguments"]
		if len(arguments) == 0 {
			arguments = function.Arguments
		}
		if len(arguments) == 0 || string(arguments) == "null" {
			arguments = json.RawMessage(`{}`)
		}
		if arguments[0] == '"' {
			var encoded string
			if err := json.Unmarshal(arguments, &encoded); err != nil {
				continue
			}
			arguments = json.RawMessage(encoded)
		}
		if !json.Valid(arguments) {
			continue
		}
		calls = append(calls, ToolCall{
			Type: "function",
			Function: FunctionCall{
				Name:      name,
				Arguments: arguments,
			},
		})
		offset += int(decoder.InputOffset()) - 1
	}
	return calls
}

func toolCallKey(name, args string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "|" + strings.ToLower(strings.TrimSpace(args))
}

func toolBatchSignature(calls []ToolCall) string {
	digest := sha256.New()
	for _, call := range calls {
		digest.Write([]byte(call.Function.Name))
		digest.Write([]byte{0})
		digest.Write(call.Function.Arguments)
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func toolSummary(name, args string) string {
	var values map[string]interface{}
	if err := json.Unmarshal([]byte(args), &values); err != nil {
		return name
	}
	for _, key := range []string{"path", "command"} {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return fmt.Sprintf("%s %q", name, preview(value, 100))
		}
	}
	if content, ok := values["content"].(string); ok {
		return fmt.Sprintf("%s (content: %d bytes)", name, len(content))
	}
	return name
}

func preview(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
