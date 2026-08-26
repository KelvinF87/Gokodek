package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"

	"golang.org/x/term"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gokodek/pkg/agent"
	"gokodek/pkg/mcp"
	"gokodek/pkg/tools"
	"gokodek/pkg/tui"
)

var version = "dev"

func main() {
	model := flag.String("model", "qwen2.5-coder:3b", "Modelo de Ollama que se utilizará")
	endpoint := flag.String("endpoint", agent.DefaultOllamaEndpoint, "Endpoint HTTP de Ollama")
	workspace := flag.String("workspace", ".", "Workspace al que tendrán acceso las herramientas")
	numCtx := flag.Int("num-ctx", 4096, "Contexto máximo en tokens para reducir consumo de memoria")
	numPredict := flag.Int("num-predict", 4096, "Máximo de tokens generados por respuesta")
	think := flag.Bool("think", false, "Activar razonamiento explícito en modelos compatibles")
	maxRounds := flag.Int("max-rounds", 12, "Máximo de rondas de herramientas por turno")
	allowUI := flag.Bool("allow-ui", false, "Permitir captura del escritorio y control real de teclado/ratón")
	visionModel := flag.String("vision-model", "", "Modelo de visión de Ollama para analizar capturas (opcional)")
	browserVisible := flag.Bool("browser-visible", true, "Mostrar la ventana aislada del navegador durante browser_screenshot")
	visionTools := flag.Bool("vision-tools", false, "Indicar que el modelo de visión también soporta tool calling")
	configPath := flag.String("config", agent.ConfigPath(), "Ruta de configuración de perfiles")
	useTUI := flag.Bool("tui", true, "Usar la interfaz gráfica de terminal (TUI). Poner -tui=false para el modo clásico de línea")
	serveStatic := flag.Bool("serve-static", false, "Servir archivos estáticos de forma interna")
	flag.Parse()

	if *serveStatic {
		args := flag.Args()
		if len(args) >= 2 {
			dir := args[0]
			port := args[1]
			log.Printf("GoKodek Servidor Estático escuchando en http://127.0.0.1:%s (Dir: %s)", port, dir)
			http.Handle("/", http.FileServer(http.Dir(dir)))
			if err := http.ListenAndServe("127.0.0.1:"+port, nil); err != nil {
				log.Fatalf("Error en servidor estático: %v", err)
			}
			return
		}
	}

	absoluteWorkspace, err := filepath.Abs(*workspace)
	if flag.CommandLine.Lookup("config") != nil && *configPath != agent.ConfigPath() {
		_ = os.Setenv("GOKODEK_CONFIG", *configPath)
	}
	if err != nil {
		log.Fatalf("workspace inválido: %v", err)
	}
	if info, statErr := os.Stat(absoluteWorkspace); statErr != nil || !info.IsDir() {
		if statErr != nil {
			log.Fatalf("workspace inválido: %v", statErr)
		}
		log.Fatalf("workspace no es un directorio: %s", absoluteWorkspace)
	}

	// Load config first so the Ollama URL from config.json can override the flag.
	config, configErr := agent.LoadConfig(absoluteWorkspace)
	secrets, secretsErr := agent.LoadSecrets()
	if secretsErr != nil {
		log.Printf("credenciales locales no disponibles: %v", secretsErr)
	}
	if configErr == nil && config.APIKeys != nil {
		for provider, key := range config.APIKeys {
			if key != "" && strings.TrimSpace(secrets[strings.ToUpper(provider)+"_API_KEY"]) == "" {
				secrets[strings.ToUpper(provider)+"_API_KEY"] = key
			}
		}
	}
	if configErr == nil && config.OllamaURL != "" {
		secrets["OLLAMA_URL"] = config.OllamaURL
	}
	_ = agent.SaveSecrets(secrets)
	if configErr == nil && *configPath != agent.ConfigPath() {
		_ = agent.SaveConfig(config)
	}
	if configErr != nil {
		log.Printf("configuración no disponible: %v", configErr)
	}

	clientEndpoint := *endpoint
	if configErr == nil && strings.TrimSpace(config.OllamaURL) != "" {
		// Use the configured Ollama URL (supports remote servers). Accept both
		// a bare host (http://host:11434) and a full endpoint
		// (http://host:11434/api/chat) without duplicating the path.
		clientEndpoint = normalizeOllamaEndpoint(config.OllamaURL)
	}

	registry := agent.NewToolRegistry()
	registerAll(registry,
		tools.NewReadFileTool(absoluteWorkspace),
		tools.NewWriteFileTool(absoluteWorkspace),
		tools.NewDeleteFileTool(absoluteWorkspace),
		tools.NewListDirTool(absoluteWorkspace),
		tools.NewRunCmdTool(absoluteWorkspace),
		tools.NewCheckWebTool(absoluteWorkspace),
		func() agent.Tool {
			tool := tools.NewFetchURLTool(absoluteWorkspace)
			tool.Configure(config.Scraping.AllowedDomains, config.Scraping.AllowedURLs, config.Scraping.RequireConfirmation)
			descriptions := make(map[string]string, len(config.Scraping.Sources))
			for _, source := range config.Scraping.Sources {
				descriptions[source.Domain] = source.Description + " Usar para: " + strings.Join(source.UseFor, ", ")
			}
			tool.ConfigureSources(descriptions)
			return tool
		}(),
		tools.NewBrowserScreenshotToolWithMode(absoluteWorkspace, *browserVisible),
		tools.NewCaptureScreenTool(absoluteWorkspace, *allowUI),
		tools.NewMouseClickTool(*allowUI),
		tools.NewKeyboardTypeTool(*allowUI),
		tools.NewProjectInfoTool(absoluteWorkspace),
		tools.NewDiagnoseProjectTool(absoluteWorkspace),
		tools.NewUIRecipeTool(absoluteWorkspace),
		tools.NewWebGPUSkillTool(absoluteWorkspace),
		tools.NewHyperFramesSkillTool(absoluteWorkspace, clientEndpoint, config.EmbeddingModel),
		tools.NewBrowserControlTool(absoluteWorkspace),
		tools.CurrentDateTool{},
		tools.PlanTool{},
		tools.NewPlanFileTool(absoluteWorkspace),
		tools.WebSearchTool{},
		tools.NewBuildTool(absoluteWorkspace),
		tools.NewRunTestTool(absoluteWorkspace),
		tools.NewLintTool(absoluteWorkspace),
		tools.NewTypeCheckTool(absoluteWorkspace),
		tools.NewTasksTool(absoluteWorkspace),
		tools.NewRAGIndexTool(absoluteWorkspace, clientEndpoint, config.EmbeddingModel),
		tools.NewRAGSearchTool(absoluteWorkspace, clientEndpoint, config.EmbeddingModel),
		tools.NewCreateGlobalSkillTool(absoluteWorkspace, clientEndpoint, config.EmbeddingModel),
		tools.NewCreateDocsTool(absoluteWorkspace),
		tools.NewServerTool(absoluteWorkspace),
		tools.NewGitTool(absoluteWorkspace),
	)

	// Ejemplo de extensión: definir la herramienta y registrarla son solo estas dos líneas.
	sysInfo := &getSysInfoTool{}
	mustRegister(registry, sysInfo)

	// Connect configured MCP servers and expose their tools.
	connectMCPServers(registry, config.MCPServers)

	client := agent.NewOllamaClient(clientEndpoint)
	agentLoop := agent.NewAgent(client, registry, *model, os.Stderr)
	mustRegister(registry, tools.NewInvokeDebateTool(absoluteWorkspace, agentLoop))
	agentLoop.NumCtx = *numCtx
	agentLoop.NumPredict = *numPredict
	agentLoop.Think = *think
	agentLoop.Mode = "build"
	agentLoop.MaxToolRounds = *maxRounds
	agentLoop.MaxToolCalls = *maxRounds * 2
	agentLoop.VisionModel = *visionModel
	agentLoop.VisionModelSupportsTools = *visionTools
	agentLoop.AnimatedSpinner = true

	// Auto-apply the active profile from config on startup. This ensures
	// VisionModel, VisionTools, NumCtx, NumPredict etc. are loaded from
	// the saved config without requiring the user to run /modelo first.
	if configErr == nil && config.Active != "" {
		if profile, ok := config.FindProfile(config.Active); ok {
			applyProfile(agentLoop, profile)
			if strings.EqualFold(profile.Provider, "openai") || strings.EqualFold(profile.Provider, "gemini") || strings.EqualFold(profile.Provider, "openrouter") {
				key := agent.Secret(secrets, strings.ToUpper(profile.Provider)+"_API_KEY")
				agentLoop.Client = agent.NewRemoteClient(profile.Provider, key)
			}
			// Command-line flags override config values
			if *visionModel != "" {
				agentLoop.VisionModel = *visionModel
				agentLoop.VisionModelSupportsTools = *visionTools
			}
		}
	}
	// Auto-detect a vision model from Ollama if none was configured.
	if agentLoop.VisionModel == "" {
		if detected := detectVisionModel(clientEndpoint); detected != "" {
			agentLoop.VisionModel = detected
			agentLoop.VisionModelSupportsTools = true
			fmt.Fprintf(os.Stderr, "vision auto-detected: %s\n", detected)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if *useTUI && isTerminal() {
		runTUI(ctx, agentLoop, config, *model, absoluteWorkspace, *configPath, absoluteWorkspace)
		return
	}
	if *useTUI && !isTerminal() {
		fmt.Fprintln(os.Stderr, "gokodek: no se detectó una terminal interactiva; usando modo clásico.")
	}

	runClassicCLI(ctx, agentLoop, config, *model, *configPath, absoluteWorkspace)
}

// detectVisionModel queries Ollama for installed models and returns the
// first one that appears to support vision (contains "vl" or "vision" or
// is known like llava). Returns empty string if none found.
func detectVisionModel(endpoint string) string {
	baseURL := strings.TrimSuffix(strings.TrimSuffix(endpoint, "/api/chat"), "/api")
	resp, err := http.Get(baseURL + "/api/tags")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return ""
	}
	// Priority order: qwen3-vl > qwen2-vl > llava > any with "vl" or "vision"
	var fallback string
	for _, m := range tags.Models {
		name := strings.ToLower(m.Name)
		if strings.Contains(name, "qwen3-vl") {
			return m.Name
		}
		if strings.Contains(name, "qwen2-vl") {
			return m.Name
		}
		if strings.Contains(name, "llava") {
			if fallback == "" {
				fallback = m.Name
			}
		}
		if strings.Contains(name, "vl") || strings.Contains(name, "vision") {
			if fallback == "" {
				fallback = m.Name
			}
		}
	}
	return fallback
}

// normalizeOllamaEndpoint converts a user-provided Ollama URL into a full
// /api/chat endpoint, tolerating both "http://host:11434" and
// "http://host:11434/api/chat" (and ".../api") forms.
func normalizeOllamaEndpoint(url string) string {
	value := strings.TrimSpace(url)
	value = strings.TrimRight(value, "/")
	if value == "" {
		return agent.DefaultOllamaEndpoint
	}
	switch {
	case strings.HasSuffix(value, "/api/chat"):
		return value
	case strings.HasSuffix(value, "/api"):
		return value + "/chat"
	default:
		return value + "/api/chat"
	}
}

// connectMCPServers starts each enabled MCP server and registers its tools.
func connectMCPServers(registry *agent.ToolRegistry, servers []agent.MCPServerConfig) {
	for _, serverConfig := range servers {
		if !serverConfig.Enabled {
			continue
		}
		adapters, _, err := mcp.ConnectServer(mcp.ServerConfig{
			Name:    serverConfig.Name,
			Command: serverConfig.Command,
			Args:    serverConfig.Args,
			Env:     serverConfig.Env,
			Enabled: serverConfig.Enabled,
		})
		if err != nil {
			log.Printf("MCP %q no conectado: %v", serverConfig.Name, err)
			continue
		}
		for _, adapter := range adapters {
			if err := registry.Register(adapter); err != nil {
				log.Printf("MCP %q: registrar %q: %v", serverConfig.Name, adapter.Name(), err)
				continue
			}
		}
		log.Printf("MCP %q conectado: %d herramientas disponibles", serverConfig.Name, len(adapters))
	}
}

// ---------------------------------------------------------------------------
// TUI mode
// ---------------------------------------------------------------------------

func runTUI(ctx context.Context, loop *agent.Agent, config agent.Config, model, workspace, configPath, absoluteWorkspace string) {
	app := tui.NewApp(loop.Model, workspace)
	app.SetMode(loop.Mode)
	app.SetModelName(activeModelLabel(loop))

	// Wire agent output into the TUI. The agent's own terminal spinner is
	// disabled because the TUI footer already renders a busy indicator; otherwise
	// every spinner frame would be pushed as a separate tool message.
	loop.ProgressWriter = app.ProgressWriter
	loop.StreamWriter = app.StreamWriter
	loop.ThinkingWriter = app.ThinkingWriter
	loop.AnimatedSpinner = false

	// Welcome banner oficial de Gokodek por Kelvin Familia (4K Services)
	welcomeMsg := fmt.Sprintf("✦ GoKodek %s by Kelvin Jose Familia Adames\n✦ Powered by 4K Services (https://4kservices.es/)\n✦ Agente Inteligente Local Optimizado | F2: Rotar modelos | Escribe /help para comandos", version)
	app.AddSystemMessage(welcomeMsg)

	// Triple-Esc cancels the running agent execution.
	var cancelMu sync.Mutex
	var cancelCurrent context.CancelFunc
	app.SetOnCancel(func() {
		cancelMu.Lock()
		defer cancelMu.Unlock()
		if cancelCurrent != nil {
			cancelCurrent()
		}
	})

	// Live command help while typing: /modelo and /config open interactive menus
	app.SetOnCommand(func(input string) string {
		value := strings.TrimSpace(input)
		switch {
		case strings.EqualFold(value, "/modelo"):
			if app.IsPickerOpen() {
				return ""
			}
			app.ShowPicker("Selecciona un modelo", profileOptions(config), func(index int) {
				profiles := config.SortedProfiles()
				if index >= 0 && index < len(profiles) {
					applyProfileTUI(app, loop, &config, profiles[index])
				}
			})
			return ""
		case strings.EqualFold(value, "/config"):
			openConfigMenu(app, &config, loop)
			return ""
		case value == "/help":
			return helpText()
		case value == "/talk":
			return "Uso: /talk <tema a debatir>\nEjemplo: /talk ¿Deberíamos usar Bootstrap o CSS nativo?"
		}
		return ""
	})

	var history []agent.Message
	var mu sync.Mutex

	app.SetOnSubmit(func(prompt string) {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			return
		}
		switch prompt {
		case "/quit", "/exit":
			app.Quit()
			return
		case "/help":
			app.AddSystemMessage(helpText())
			app.SetBusy(false, "")
			return
		case "/clear":
			mu.Lock()
			history = nil
			mu.Unlock()
			app.AddSystemMessage("Contexto borrado.")
			app.SetBusy(false, "")
			return
		case "/new", "/nueva":
			mu.Lock()
			history = nil
			mu.Unlock()
			app.AddSystemMessage("Nueva conversación iniciada.")
			app.SetBusy(false, "")
			return
		}
		if strings.EqualFold(prompt, "/config") || strings.HasPrefix(strings.ToLower(prompt), "/config ") {
			openConfigMenu(app, &config, loop)
			app.SetBusy(false, "")
			return
		}
		if strings.EqualFold(prompt, "/modelo") || strings.HasPrefix(strings.ToLower(prompt), "/modelo ") {
			selectModelTUI(app, &config, loop, prompt)
			app.SetBusy(false, "")
			return
		}
		if strings.EqualFold(prompt, "/talk") || strings.HasPrefix(strings.ToLower(prompt), "/talk ") {
			topic := agent.ParseDebateTopic(prompt)
			if topic == "" {
				app.AddSystemMessage("Uso: /talk <tema a debatir>")
				app.SetBusy(false, "")
				return
			}
			go func() {
				turnCtx, turnCancel := context.WithCancel(ctx)
				cancelMu.Lock()
				cancelCurrent = turnCancel
				cancelMu.Unlock()

				app.SetBusy(true, "Debatiendo...")
				// Ground the debate in the real project so agents discuss
				// concrete files instead of generic advice.
				contextText := buildDebateContext(loop.Registry, absoluteWorkspace)
				if client, ok := loop.Client.(*agent.OllamaClient); ok {
					consensusPlan := runDebate(turnCtx, client, loop.Model, topic, app.SystemWriter, contextText)
					
					// Invocar inmediatamente al Agente Ejecutor (Builder Agent) para aplicar la resolución aprobada
					if consensusPlan != "" && turnCtx.Err() == nil {
						app.AddSystemMessage("⚡ AGENTE EJECUTOR INICIANDO: Aplicando el plan resolutivo aprobado por el Jefe 👑...")
						loop.Mode = "build"
						execPrompt := fmt.Sprintf("Ejecuta completamente el siguiente plan resolutivo aprobado por el Jefe Técnico sin detenerte hasta dejar el proyecto perfecto:\n\n%s", consensusPlan)
						mu.Lock()
						_, _ = loop.Run(turnCtx, &history, execPrompt)
						mu.Unlock()
						app.AddSystemMessage("✅ EJECUCIÓN COMPLETADA: Todos los cambios aprobados en el debate han sido aplicados y verificados.")
					}
				} else {
					app.AddSystemMessage("/talk requiere temporalmente un perfil Ollama.")
				}

				cancelMu.Lock()
				cancelCurrent = nil
				cancelMu.Unlock()
				turnCancel()

				app.SetBusy(false, "")
			}()
			return
		}

		go func() {
			// Keep the TUI mode authoritative for this turn. Plan mode is enforced
			// inside Agent.Run, not only by the model prompt.
			if strings.EqualFold(app.CurrentMode(), "plan") {
				loop.Mode = "plan"
			} else {
				loop.Mode = "build"
			}
			// Per-execution context so triple-Esc can cancel this run only.
			turnCtx, turnCancel := context.WithCancel(ctx)
			cancelMu.Lock()
			cancelCurrent = turnCancel
			cancelMu.Unlock()

			app.SetBusy(true, "Pensando...")
			mu.Lock()
			_, err := loop.Run(turnCtx, &history, prompt)
			mu.Unlock()

			cancelMu.Lock()
			cancelCurrent = nil
			cancelMu.Unlock()
			turnCancel()

			app.SetBusy(false, "")
			if err != nil {
				if ctx.Err() == context.Canceled || errors.Is(err, context.Canceled) {
					app.AddSystemMessage("Ejecución cancelada (Esc ×3).")
				} else if agent.IsToolsUnsupportedError(err) {
					app.AddSystemMessage(fmt.Sprintf("El modelo %q no soporta herramientas (tool calling). \nUsa /modelo para elegir un modelo que sí las soporte, como qwen2.5-coder, qwen3 o qwen3-vl.", loop.Model))
				} else {
					app.ProgressWriter.Write([]byte(fmt.Sprintf("error: %v", err)))
				}
			}
			if loop.SessionStats != nil {
				app.UpdateStats(
					loop.SessionStats.Turns,
					loop.SessionStats.TotalPromptTokens,
					loop.SessionStats.TotalGeneratedTokens,
					loop.SessionStats.TotalDuration.Milliseconds(),
				)
			}
		}()
	})

	if err := app.Start(); err != nil {
		log.Fatalf("TUI error: %v", err)
	}
}

// openConfigMenu shows the interactive configuration menu: API keys, Ollama
// server URL, and detection of locally installed Ollama models.
func openConfigMenu(app *tui.App, config *agent.Config, loop *agent.Agent) {
	options := []string{
		"Detectar modelos de Ollama (ollama list)",
		"Configurar URL de Ollama",
		"Configurar modelo de embeddings (RAG)",
		"Configurar API Key de OpenAI",
		"Configurar API Key de Gemini",
		"Configurar API Key de OpenRouter",
		"Gestionar servidores MCP",
		"Gestionar URLs permitidas para scraping",
		"Ver configuración actual",
		"Guardar y salir",
	}
	app.ShowPicker("Configuración", options, func(index int) {
		switch index {
		case 0:
			detectOllamaModelsTUI(app, config, loop)
		case 1:
			setOllamaURLTUI(app, config, loop)
		case 2:
			setEmbeddingModelTUI(app, config)
		case 3:
			setAPIKeyTUI(app, config, "openai", "OpenAI", loop)
		case 4:
			setAPIKeyTUI(app, config, "gemini", "Gemini", loop)
		case 5:
			setAPIKeyTUI(app, config, "openrouter", "OpenRouter", loop)
		case 6:
			manageMCPServersTUI(app, config)
		case 7:
			manageScrapingURLsTUI(app, config)
		case 8:
			showConfigTUI(app, config)
		case 9:
			saveConfigTUI(app, config)
		}
	})
}

func manageScrapingURLsTUI(app *tui.App, config *agent.Config) {
	options := []string{"Añadir dominio permitido", "Añadir URL exacta permitida", "Ver lista actual"}
	app.ShowPicker("URLs permitidas para scraping", options, func(index int) {
		switch index {
		case 0:
			app.AskText("Dominio permitido", "Ej: threejs.org", false, func(value string) {
				value = strings.TrimSpace(strings.ToLower(value))
				if value == "" {
					return
				}
				config.Scraping.AllowedDomains = append(config.Scraping.AllowedDomains, value)
				_ = agent.SaveConfig(*config)
				app.AddSystemMessage("Dominio permitido guardado: " + value)
			})
		case 1:
			app.AskText("URL permitida", "Ej: https://threejs.org/docs/", false, func(value string) {
				value = strings.TrimSpace(value)
				if value == "" {
					return
				}
				config.Scraping.AllowedURLs = append(config.Scraping.AllowedURLs, value)
				_ = agent.SaveConfig(*config)
				app.AddSystemMessage("URL permitida guardada: " + value)
			})
		case 2:
			var sb strings.Builder
			fmt.Fprintf(&sb, "Dominios: %s\\nURLs: %s\\nConfirmación requerida: %t\\n\\nFuentes:\\n", strings.Join(config.Scraping.AllowedDomains, ", "), strings.Join(config.Scraping.AllowedURLs, ", "), config.Scraping.RequireConfirmation)
			for _, source := range config.Scraping.Sources {
				fmt.Fprintf(&sb, "- %s: %s\\n  uso: %s\\n", source.Domain, source.Description, strings.Join(source.UseFor, ", "))
			}
			app.AddSystemMessage(sb.String())
		}
	})
}

// detectOllamaModelsTUI queries the configured Ollama server and offers detected
// models as new profiles or as replacements for the active profile.
func detectOllamaModelsTUI(app *tui.App, config *agent.Config, loop *agent.Agent) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := agent.DetectOllamaModels(ctx, config.OllamaURL)
	if err != nil {
		app.AddSystemMessage(fmt.Sprintf("No se pudieron detectar modelos de Ollama: %v", err))
		return
	}
	if len(models) == 0 {
		app.AddSystemMessage("No hay modelos instalados en Ollama. Ejecuta: ollama pull <modelo>")
		return
	}
	names := make([]string, 0, len(models))
	for _, model := range models {
		names = append(names, model.Name)
	}
	app.AddSystemMessage(fmt.Sprintf("Detectados %d modelos en Ollama:", len(models)))
	app.ShowPicker("Modelos de Ollama detectados", names, func(index int) {
		if index < 0 || index >= len(models) {
			return
		}
		name := models[index].Name
		// Check if there is an existing ollama profile with this model.
		for _, profile := range config.Profiles {
			if profile.Provider == "ollama" && strings.EqualFold(profile.Model, name) {
				applyProfileTUI(app, loop, config, profile)
				return
			}
		}
		// Create a new profile for the detected model.
		profileName := sanitizeProfileName(name)
		newProfile := agent.ModelProfile{
			Name:       profileName,
			Provider:   "ollama",
			Model:      name,
			NumCtx:     4096,
			NumPredict: 4096,
			KeepAlive:  "5m",
		}
		config.Profiles = append(config.Profiles, newProfile)
		if err := agent.SaveConfig(*config); err != nil {
			app.AddSystemMessage(fmt.Sprintf("No se pudo guardar: %v", err))
			return
		}
		applyProfileTUI(app, loop, config, newProfile)
		app.AddSystemMessage(fmt.Sprintf("Perfil %q creado y activado para %s", profileName, name))
	})
}

// setOllamaURLTUI prompts for the Ollama server URL and updates the client.
func setOllamaURLTUI(app *tui.App, config *agent.Config, loop *agent.Agent) {
	app.AskText("URL de Ollama", fmt.Sprintf("Actual: %s. Ej: http://192.168.1.10:11434", config.OllamaURL), false, func(value string) {
		if value == "" {
			app.AddSystemMessage("URL vacía, no se cambió nada.")
			return
		}
		config.OllamaURL = value
		if client, ok := loop.Client.(*agent.OllamaClient); ok {
			client.Endpoint = normalizeOllamaEndpoint(value)
		}
		if err := agent.SaveConfig(*config); err != nil {
			app.AddSystemMessage(fmt.Sprintf("No se pudo guardar: %v", err))
			return
		}
		app.AddSystemMessage(fmt.Sprintf("URL de Ollama actualizada a %s", normalizeOllamaEndpoint(value)))
	})
}

// setEmbeddingModelTUI lets the user set the Ollama embedding model used by RAG.
func setEmbeddingModelTUI(app *tui.App, config *agent.Config) {
	app.AskText("Modelo de embeddings (RAG)", fmt.Sprintf("Actual: %s. Ej: nomic-embed-text", config.EmbeddingModel), false, func(value string) {
		if value == "" {
			app.AddSystemMessage("Modelo vacío, no se cambió nada.")
			return
		}
		config.EmbeddingModel = value
		if err := agent.SaveConfig(*config); err != nil {
			app.AddSystemMessage(fmt.Sprintf("No se pudo guardar: %v", err))
			return
		}
		app.AddSystemMessage(fmt.Sprintf("Modelo de embeddings actualizado a %s", value))
	})
}

// manageMCPServersTUI shows configured MCP servers and allows adding a new one.
func manageMCPServersTUI(app *tui.App, config *agent.Config) {
	servers := config.MCPServers
	options := make([]string, 0, len(servers)+1)
	for _, server := range servers {
		status := "off"
		if server.Enabled {
			status = "on"
		}
		options = append(options, fmt.Sprintf("%s (%s) — %s", server.Name, status, server.Command))
	}
	options = append(options, "➕ Añadir servidor MCP")

	app.ShowPicker("Servidores MCP", options, func(index int) {
		if index == len(servers) {
			addMCPServerTUI(app, config)
			return
		}
		if index < 0 || index >= len(servers) {
			return
		}
		// Toggle enabled state.
		config.MCPServers[index].Enabled = !config.MCPServers[index].Enabled
		if err := agent.SaveConfig(*config); err != nil {
			app.AddSystemMessage(fmt.Sprintf("No se pudo guardar: %v", err))
			return
		}
		state := "desactivado"
		if config.MCPServers[index].Enabled {
			state = "activado (reinicia gokodek para conectar)"
		}
		app.AddSystemMessage(fmt.Sprintf("Servidor MCP %q %s", config.MCPServers[index].Name, state))
	})
}

// addMCPServerTUI asks for the MCP server command and args, then saves it.
func addMCPServerTUI(app *tui.App, config *agent.Config) {
	app.AskText("Comando MCP", "Ej: npx -y @modelcontextprotocol/server-filesystem D:\\proyecto", false, func(command string) {
		if strings.TrimSpace(command) == "" {
			app.AddSystemMessage("Comando vacío, cancelado.")
			return
		}
		fields := strings.Fields(command)
		name := filepath.Base(fields[0])
		name = strings.TrimSuffix(name, ".exe")
		name = strings.TrimSuffix(name, ".cmd")
		config.MCPServers = append(config.MCPServers, agent.MCPServerConfig{
			Name:    name,
			Command: fields[0],
			Args:    fields[1:],
			Enabled: true,
		})
		if err := agent.SaveConfig(*config); err != nil {
			app.AddSystemMessage(fmt.Sprintf("No se pudo guardar: %v", err))
			return
		}
		app.AddSystemMessage(fmt.Sprintf("Servidor MCP %q añadido y activado. Reinicia gokodek para conectar sus herramientas.", name))
	})
}

// setAPIKeyTUI prompts for a provider API key (masked) and stores it.
func setAPIKeyTUI(app *tui.App, config *agent.Config, provider, label string, loop *agent.Agent) {
	current := "no configurada"
	if key := config.APIKeys[provider]; key != "" {
		current = maskKey(key)
	}
	app.AskText("API Key de "+label, fmt.Sprintf("Actual: %s. Pega tu API key (se guarda en config.json)", current), true, func(value string) {
		if value == "" {
			app.AddSystemMessage("Key vacía, no se cambió nada.")
			return
		}
		if config.APIKeys == nil {
			config.APIKeys = map[string]string{}
		}
		config.APIKeys[provider] = value
		secrets, _ := agent.LoadSecrets()
		secrets[strings.ToUpper(provider)+"_API_KEY"] = value
		if err := agent.SaveSecrets(secrets); err != nil {
			app.AddSystemMessage(fmt.Sprintf("No se pudo guardar la credencial local: %v", err))
			return
		}
		if strings.EqualFold(provider, "gemini") || strings.EqualFold(provider, "openai") || strings.EqualFold(provider, "openrouter") {
			loop.Client = agent.NewRemoteClient(provider, value)
		}
		if err := agent.SaveConfig(*config); err != nil {
			app.AddSystemMessage(fmt.Sprintf("No se pudo guardar: %v", err))
			return
		}
		app.AddSystemMessage(fmt.Sprintf("API Key de %s guardada (%s)", label, maskKey(value)))
	})
}

// showConfigTUI prints the current configuration (keys masked).
func showConfigTUI(app *tui.App, config *agent.Config) {
	var sb strings.Builder
	sb.WriteString("Configuración actual:\n")
	sb.WriteString(fmt.Sprintf("  URL Ollama: %s\n", config.OllamaURL))
	sb.WriteString(fmt.Sprintf("  Embeddings: %s\n", config.EmbeddingModel))
	sb.WriteString(fmt.Sprintf("  Scraping domains: %s\n", strings.Join(config.Scraping.AllowedDomains, ", ")))
	sb.WriteString(fmt.Sprintf("  Scraping URLs: %s\n", strings.Join(config.Scraping.AllowedURLs, ", ")))
	for _, provider := range []string{"openai", "gemini", "openrouter"} {
		status := "no configurada"
		if key := config.APIKeys[provider]; key != "" {
			status = maskKey(key)
		}
		sb.WriteString(fmt.Sprintf("  %-10s %s\n", provider, status))
	}
	sb.WriteString("  Servidores MCP:\n")
	if len(config.MCPServers) == 0 {
		sb.WriteString("    (ninguno configurado)\n")
	}
	for _, server := range config.MCPServers {
		state := "off"
		if server.Enabled {
			state = "on"
		}
		sb.WriteString(fmt.Sprintf("    - %s [%s] %s\n", server.Name, state, server.Command))
	}
	sb.WriteString("  Perfiles:\n")
	for _, profile := range config.Profiles {
		sb.WriteString(fmt.Sprintf("    - %s (%s/%s)\n", profile.Name, profile.Provider, profile.Model))
	}
	app.AddSystemMessage(sb.String())
}

// saveConfigTUI persists the current configuration.
func saveConfigTUI(app *tui.App, config *agent.Config) {
	if err := agent.SaveConfig(*config); err != nil {
		app.AddSystemMessage(fmt.Sprintf("No se pudo guardar: %v", err))
		return
	}
	app.AddSystemMessage("Configuración guardada en " + agent.ConfigPath())
}

// sanitizeProfileName converts a model name into a safe profile identifier.
func sanitizeProfileName(modelName string) string {
	value := strings.ToLower(modelName)
	value = strings.ReplaceAll(value, ":", "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, ".", "-")
	return value
}

// maskKey hides all but the last four characters of a secret.
func maskKey(key string) string {
	if len(key) <= 4 {
		return "••••"
	}
	return "••••" + key[len(key)-4:]
}

// buildDebateContext gathers real information about the workspace so the
// debate agents discuss actual files instead of generic advice.
func buildDebateContext(registry *agent.ToolRegistry, workspace string) string {
	if registry == nil {
		return ""
	}
	// Use the registered project_info tool if available.
	if registry.Has("project_info") {
		if result, err := registry.Execute("project_info", "{}"); err == nil && strings.TrimSpace(result) != "" {
			return result
		}
	}
	// Fallback: list the workspace directly.
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return ""
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return "workspace=" + workspace + "\nfiles=" + strings.Join(names, ", ")
}

// profileOptions formats each configured profile as a menu option.
func profileOptions(config agent.Config) []string {
	profiles := config.SortedProfiles()
	options := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		options = append(options, fmt.Sprintf("%s  (%s)", profile.Model, profile.Provider))
	}
	return options
}

// listModelsText builds the profile list text shown in the TUI.
func listModelsText(config agent.Config) string {
	profiles := config.SortedProfiles()
	var sb strings.Builder
	sb.WriteString("Modelos configurados:\n")
	if len(profiles) == 0 {
		sb.WriteString("  (sin perfiles configurados)\n")
	}
	for i, profile := range profiles {
		sb.WriteString(fmt.Sprintf("  %d) %-14s %-10s %s\n", i+1, profile.Name, profile.Provider, profile.Model))
	}
	sb.WriteString("Escribe /modelo <nombre> para seleccionar.")
	return sb.String()
}

// selectModelTUI shows the profile list inside the TUI and applies the chosen profile.
func selectModelTUI(app *tui.App, config *agent.Config, loop *agent.Agent, prompt string) {
	profiles := config.SortedProfiles()
	query := strings.TrimSpace(strings.TrimPrefix(prompt, "/modelo"))
	if query != "" {
		for _, profile := range profiles {
			if strings.EqualFold(profile.Name, query) || strings.EqualFold(profile.Model, query) {
				applyProfileTUI(app, loop, config, profile)
				return
			}
		}
	}
	app.AddSystemMessage(listModelsText(*config))
}

func activeModelLabel(loop *agent.Agent) string {
	if client, ok := loop.Client.(*agent.RemoteClient); ok {
		return client.Provider + "/" + loop.Model
	}
	return "ollama/" + loop.Model
}

func configKeyForProvider(provider string) string {
	secrets, _ := agent.LoadSecrets()
	key := strings.TrimSpace(secrets[strings.ToUpper(provider)+"_API_KEY"])
	if key == "" {
		key = strings.TrimSpace(os.Getenv(strings.ToUpper(provider) + "_API_KEY"))
	}
	return key
}

func applyProfileTUI(app *tui.App, loop *agent.Agent, config *agent.Config, profile agent.ModelProfile) {
	applyProfile(loop, profile)
	if strings.EqualFold(profile.Provider, "ollama") {
		loop.Client = agent.NewOllamaClient(agent.DefaultOllamaEndpoint)
	} else {
		loop.Client = agent.NewRemoteClient(profile.Provider, configKeyForProvider(profile.Provider))
	}
	config.Active = profile.Name
	_ = agent.SaveConfig(*config)

	// Update recent models list for quick F2 access
	recent := []string{profile.Model}
	for _, m := range config.RecentModels {
		if !strings.EqualFold(m, profile.Model) && len(recent) < 4 {
			recent = append(recent, m)
		}
	}
	config.RecentModels = recent
	_ = agent.SaveConfig(*config)
	app.SetRecentModels(recent)

	app.SetModelName(profile.Provider + "/" + profile.Model)
	app.AddSystemMessage(fmt.Sprintf("Modelo activo guardado por defecto: %s/%s", profile.Provider, profile.Model))
}

// ---------------------------------------------------------------------------
// Classic CLI mode (line-based REPL)
// ---------------------------------------------------------------------------

func runClassicCLI(ctx context.Context, loop *agent.Agent, config agent.Config, model, configPath, absoluteWorkspace string) {
	loop.StreamWriter = os.Stdout
	var history []agent.Message
	scanner := newScanner()
	for {
		if ctx.Err() != nil {
			fmt.Println("\nSaliendo.")
			return
		}
		fmt.Printf("\n%s›%s ", ansiCyan, ansiReset)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "error leyendo entrada: %v\n", err)
			}
			fmt.Println("\nSaliendo.")
			return
		}
		prompt := strings.TrimSpace(scanner.Text())
		switch prompt {
		case "":
			continue
		case "/quit", "/exit":
			fmt.Println("Saliendo.")
			return
		case "/help":
			fmt.Print(helpText())
			continue
		case "/clear":
			history = nil
			fmt.Println("Contexto borrado.")
			continue
		case "/new", "/nueva":
			history = nil
			fmt.Println("Nueva conversación iniciada.")
			continue
		}
		if strings.EqualFold(prompt, "/modelo") || strings.HasPrefix(strings.ToLower(prompt), "/modelo ") {
			selected, ok := selectModel(config, prompt, scanner)
			if ok {
				applyProfile(loop, selected)
				if strings.EqualFold(selected.Provider, "ollama") {
					loop.Client = agent.NewOllamaClient(normalizeOllamaEndpoint(config.OllamaURL))
				} else {
					key := configKeyForProvider(selected.Provider)
					if key == "" {
						key = config.APIKeys[strings.ToLower(selected.Provider)]
					}
					loop.Client = agent.NewRemoteClient(selected.Provider, key)
				}
				model = selected.Model
				fmt.Printf("Modelo activo: %s/%s\n", selected.Provider, selected.Model)
			}
			continue
		}
		if strings.EqualFold(prompt, "/talk") || strings.HasPrefix(strings.ToLower(prompt), "/talk ") {
			topic := agent.ParseDebateTopic(prompt)
			if topic == "" {
				fmt.Println("Uso: /talk <tema a debatir>")
				continue
			}
			if client, ok := loop.Client.(*agent.OllamaClient); ok {
				runDebate(ctx, client, loop.Model, topic, os.Stdout, buildDebateContext(loop.Registry, absoluteWorkspace))
			} else {
				fmt.Println("/talk requiere temporalmente un perfil Ollama.")
			}
			continue
		}

		fmt.Printf("\n%s╭─ %sagent%s %s╮%s\n%s│%s ", ansiDim, ansiGreen, ansiDim, loop.Model, ansiReset, ansiDim, ansiReset)
		answer, err := loop.Run(ctx, &history, prompt)
		fmt.Println()
		if err != nil {
			if agent.IsToolsUnsupportedError(err) {
				fmt.Fprintf(os.Stderr, "%s✗%s El modelo %q no soporta herramientas. Usa /modelo para elegir qwen2.5-coder, qwen3 o qwen3-vl.\n", ansiRed, ansiReset, loop.Model)
			} else {
				fmt.Fprintf(os.Stderr, "%s✗%s %v\n", ansiRed, ansiReset, err)
			}
			continue
		}
		if loop.SessionStats != nil {
			fmt.Printf("%s  %s%s\n", ansiDim, loop.SessionStats.FormatSession(), ansiReset)
		}
		if loop.StreamWriter == nil {
			fmt.Print(answer)
		}
	}
}

const (
	ansiCyan    = "\033[36m"
	ansiGreen   = "\033[32m"
	ansiRed     = "\033[31m"
	ansiDim     = "\033[2m"
	ansiYellow  = "\033[33m"
	ansiMagenta = "\033[35m"
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
)

func runDebate(ctx context.Context, client *agent.OllamaClient, model, topic string, writer io.Writer, projectContext string) string {
	config := agent.DefaultDebateConfig(topic)
	config.Context = projectContext
	engine := agent.NewDebateEngine(client, model, config)

	fmt.Fprintf(writer, "\n%s╔══════════════════════════════════════════════════════════════╗%s\n", ansiYellow, ansiReset)
	fmt.Fprintf(writer, "%s║  %s🎭 DEBATE TÉCNICO RESOLUTIVO - AGENTES CRÍTICOS & JEFE 👑%s    ║%s\n", ansiYellow, ansiBold, ansiYellow, ansiReset)
	fmt.Fprintf(writer, "%s╚══════════════════════════════════════════════════════════════╝%s\n\n", ansiYellow, ansiReset)
	fmt.Fprintf(writer, "%s  Tema:%s %s\n\n", ansiDim, ansiReset, topic)

	consensus, err := engine.Run(ctx,
		func(msg agent.DebateMessage) {
			fmt.Fprintf(writer, "\r%s  %s %s analizando...%s", ansiDim, msg.Icon, msg.Agent, ansiReset)
		},
		func(msg agent.DebateMessage) {
			fmt.Fprintf(writer, "\r\033[K")
			fmt.Fprintf(writer, "%s┌─ %s %s (Ronda %d) ──────────────────────────┐%s\n", ansiYellow, msg.Icon, msg.Agent, msg.Round, ansiReset)
			lines := strings.Split(msg.Content, "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					fmt.Fprintf(writer, "%s│%s %s\n", ansiDim, ansiReset, line)
				}
			}
			fmt.Fprintf(writer, "%s└─────────────────────────────────────────────────┘%s\n\n", ansiYellow, ansiReset)
		},
	)

	if err != nil {
		fmt.Fprintf(writer, "%s✗ Fallo en el debate: %v%s\n", ansiRed, err, ansiReset)
		return ""
	}

	fmt.Fprintf(writer, "%s╔══════════════════════════════════════════════════════════════╗%s\n", ansiGreen, ansiReset)
	fmt.Fprintf(writer, "%s║  %s👑 RESOLUCIÓN FINAL CON VISTO BUENO DEL JEFE TÉCNICO%s         ║%s\n", ansiGreen, ansiBold, ansiGreen, ansiReset)
	fmt.Fprintf(writer, "%s╚══════════════════════════════════════════════════════════════╝%s\n\n", ansiGreen, ansiReset)
	fmt.Fprintln(writer, consensus)
	fmt.Fprintln(writer)
	return consensus
}

func newScanner() *bufio.Scanner {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	return scanner
}

// isTerminal reports whether stdin and stdout are attached to a real terminal.
// The TUI requires an interactive console; otherwise we fall back to the line REPL.
func isTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func registerAll(registry *agent.ToolRegistry, toolsToRegister ...agent.Tool) {
	for _, tool := range toolsToRegister {
		mustRegister(registry, tool)
	}
}

func mustRegister(registry *agent.ToolRegistry, tool agent.Tool) {
	if err := registry.Register(tool); err != nil {
		log.Fatalf("registrar herramienta %q: %v", tool.Name(), err)
	}
}

func selectModel(config agent.Config, prompt string, scanner *bufio.Scanner) (agent.ModelProfile, bool) {
	profiles := config.SortedProfiles()
	query := strings.TrimSpace(strings.TrimPrefix(prompt, "/modelo"))
	if query != "" {
		for _, profile := range profiles {
			if strings.EqualFold(profile.Name, query) || strings.EqualFold(profile.Model, query) {
				return profile, true
			}
		}
		fmt.Printf("Perfil no encontrado: %s\n", query)
	}
	fmt.Println("\nModelos configurados:")
	for i, profile := range profiles {
		fmt.Printf("  %d) %-14s %-10s %s\n", i+1, profile.Name, profile.Provider, profile.Model)
	}
	fmt.Print("Selecciona un modelo (número o nombre, Enter cancela): ")
	if scanner == nil || !scanner.Scan() {
		return agent.ModelProfile{}, false
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return agent.ModelProfile{}, false
	}
	if number, err := strconv.Atoi(input); err == nil && number >= 1 && number <= len(profiles) {
		return profiles[number-1], true
	}
	for _, profile := range profiles {
		if strings.EqualFold(profile.Name, input) {
			return profile, true
		}
	}
	return agent.ModelProfile{}, false
}

func applyProfile(loop *agent.Agent, profile agent.ModelProfile) {
	loop.Model = profile.Model
	if strings.EqualFold(profile.Provider, "openai") || strings.EqualFold(profile.Provider, "gemini") || strings.EqualFold(profile.Provider, "openrouter") {
		// Provider client is replaced by the caller when config keys are available.
	}
	loop.VisionModel = profile.VisionModel
	loop.VisionModelSupportsTools = profile.VisionTools
	if profile.NumCtx > 0 {
		loop.NumCtx = profile.NumCtx
	}
	if profile.NumPredict > 0 {
		loop.NumPredict = profile.NumPredict
	}
	loop.Think = profile.Think
	if profile.KeepAlive != "" {
		loop.KeepAlive = profile.KeepAlive
	}
}

func helpText() string {
	return `gokodek — CLI agéntico local

Comandos:
  /help       Muestra esta ayuda
  /modelo     Selecciona un perfil de modelo (ej: /modelo local-fast)
  /config     Configura API keys, URL de Ollama y detecta modelos
  /talk       Inicia un debate multi-agente sobre un tema
  /clear      Borra el historial de la conversación
  /new        Inicia una conversación nueva (borra el contexto)
  /quit       Termina gokodek

Herramientas:
  Archivos:   read_file, write_file, delete_file, list_dir, run_cmd
  Web:        check_web, fetch_url, web_search, browser_screenshot
  Testing:    run_test, lint, typecheck
  Análisis:   project_info, ui_recipe, plan, build
  Sistema:    current_date, get_sys_info
  UI (con -allow-ui=true): capture_screen, mouse_click, keyboard_type

Debate multi-agente:
  /talk ¿Deberíamos usar Bootstrap o CSS nativo para esta web?
  Los agentes (architect, reviewer, implementer, tester) debaten
  y producen un consenso accionable.

Atajos TUI:
  Enter        Enviar mensaje
  ↑            Historial de comandos
  PgUp/PgDn    Scroll de la conversación (End vuelve al final)
  Ctrl+C       Salir

Para copiar contenido antiguo, sube con PgUp y selecciona el texto con el
ratón (en Windows Terminal, mantén Shift si el ratón queda capturado).
`
}

// getSysInfoTool is a complete custom tool example implemented in this file.
type getSysInfoTool struct{}

func (t *getSysInfoTool) Name() string { return "get_sys_info" }
func (t *getSysInfoTool) Description() string {
	return "Return basic operating-system and runtime information."
}
func (t *getSysInfoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t *getSysInfoTool) Execute(argsJSON string) (string, error) {
	var ignored map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &ignored); err != nil {
		return "", fmt.Errorf("get_sys_info arguments: %w", err)
	}

	hostname, _ := os.Hostname()
	currentUser := "unknown"
	if value, err := user.Current(); err == nil {
		currentUser = value.Username
	}
	workingDirectory, _ := os.Getwd()
	return fmt.Sprintf(
		"os=%s\narch=%s\ngo=%s\nhostname=%s\nuser=%s\ncwd=%s",
		runtime.GOOS, runtime.GOARCH, runtime.Version(), hostname, currentUser, workingDirectory,
	), nil
}
