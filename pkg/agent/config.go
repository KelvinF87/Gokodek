package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type ModelProfile struct {
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	VisionModel string `json:"vision_model,omitempty"`
	VisionTools bool   `json:"vision_tools,omitempty"`
	NumCtx      int    `json:"num_ctx,omitempty"`
	NumPredict  int    `json:"num_predict,omitempty"`
	Think       bool   `json:"think,omitempty"`
	KeepAlive   string `json:"keep_alive,omitempty"`
}

// MCPServerConfig describes a user-configured MCP server connection.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled,omitempty"`
}

type AllowedSource struct {
	Domain      string   `json:"domain"`
	Description string   `json:"description"`
	UseFor      []string `json:"use_for,omitempty"`
	URLs        []string `json:"urls,omitempty"`
}

type ScrapingConfig struct {
	AllowedDomains      []string        `json:"allowed_domains,omitempty"`
	AllowedURLs         []string        `json:"allowed_urls,omitempty"`
	Sources             []AllowedSource `json:"sources,omitempty"`
	RequireConfirmation bool            `json:"require_confirmation"`
}

type Config struct {
	Workspace    string         `json:"workspace,omitempty"`
	Active       string         `json:"active,omitempty"`
	RecentModels []string       `json:"recent_models,omitempty"`
	Profiles     []ModelProfile `json:"profiles"`
	// OllamaURL is the endpoint for the local (or remote) Ollama server.
	OllamaURL string `json:"ollama_url,omitempty"`
	// APIKeys holds provider API keys keyed by provider name
	// (openai, gemini, openrouter). Stored locally, never printed.
	APIKeys map[string]string `json:"api_keys,omitempty"`
	// EmbeddingModel is the Ollama model used for RAG vector embeddings.
	EmbeddingModel string `json:"embedding_model,omitempty"`
	// MCPServers lists user-configured MCP servers to expose as tools.
	MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`
	Scraping   ScrapingConfig    `json:"scraping,omitempty"`
}

func DefaultConfig(workspace string) Config {
	return Config{
		Workspace:      workspace,
		Active:         "local-coder",
		OllamaURL:      "http://localhost:11434",
		APIKeys:        map[string]string{},
		EmbeddingModel: "nomic-embed-text",
		Scraping: ScrapingConfig{
			AllowedDomains:      defaultScrapingDomains(),
			Sources:             defaultScrapingSources(),
			RequireConfirmation: true,
		},
		Profiles: []ModelProfile{
			{Name: "local-coder", Provider: "ollama", Model: "qwen2.5-coder:3b", VisionModel: "qwen3-vl:2b", VisionTools: true, NumCtx: 4096, NumPredict: 4096, KeepAlive: "5m"},
			{Name: "local-fast", Provider: "ollama", Model: "qwen3:1.7b", NumCtx: 3072, NumPredict: 2048, KeepAlive: "5m"},
			{Name: "openai", Provider: "openai", Model: "gpt-4o-mini", NumCtx: 8192, NumPredict: 4096},
			{Name: "gemini", Provider: "gemini", Model: "gemini-2.5-flash", NumCtx: 8192, NumPredict: 4096},
			{Name: "openrouter", Provider: "openrouter", Model: "deepseek/deepseek-chat-v3-0324", NumCtx: 8192, NumPredict: 4096},
		},
	}
}

func ConfigPath() string {
	if value := os.Getenv("GOKODEK_CONFIG"); value != "" {
		return value
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			base = os.Getenv("USERPROFILE")
		}
		return filepath.Join(base, "gokodek", "config.json")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base, _ = os.UserHomeDir()
	}
	return filepath.Join(base, "gokodek", "config.json")
}

func LoadConfig(workspace string) (Config, error) {
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		config := DefaultConfig(workspace)
		if saveErr := SaveConfig(config); saveErr != nil {
			return config, saveErr
		}
		return config, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if config.Workspace == "" {
		config.Workspace = workspace
	}
	if config.OllamaURL == "" {
		config.OllamaURL = "http://localhost:11434"
	}
	if config.APIKeys == nil {
		config.APIKeys = map[string]string{}
	}
	if config.EmbeddingModel == "" {
		config.EmbeddingModel = "nomic-embed-text"
	}
	if len(config.Profiles) == 0 {
		config = DefaultConfig(config.Workspace)
	} else {
		config.Scraping = mergeScrapingConfig(config.Scraping)
		// Persist the migration so the user can inspect and edit the complete
		// catalog from /config without changing the operating-system environment.
		if migrated, saveErr := scrapingConfigChanged(data, config); migrated && saveErr == nil {
			_ = SaveConfig(config)
		}
	}
	return config, nil
}

func defaultScrapingDomains() []string {
	return []string{
		"threejs.org", "webgpu.github.io", "developer.mozilla.org", "html.spec.whatwg.org",
		"tc39.es", "ecma-international.org", "php.net", "react.dev", "vite.dev", "nodejs.org",
		"typescriptlang.org", "web.dev", "github.com", "cdn.jsdelivr.net", "unpkg.com",
		"cdnjs.cloudflare.com", "esm.sh", "raw.githubusercontent.com",
	}
}

func defaultScrapingSources() []AllowedSource {
	return []AllowedSource{
		{Domain: "threejs.org", Description: "Documentación, manual y ejemplos oficiales de Three.js para gráficos 3D WebGL/WebGPU.", UseFor: []string{"three.js", "3d", "webgl", "webgpu", "escenas", "juegos"}, URLs: []string{"https://threejs.org/docs/", "https://threejs.org/examples/", "https://threejs.org/manual/"}},
		{Domain: "webgpu.github.io", Description: "Especificación y ejemplos de WebGPU para renderizado 3D moderno en navegador.", UseFor: []string{"webgpu", "gpu", "compute", "3d"}, URLs: []string{"https://webgpu.github.io/"}},
		{Domain: "developer.mozilla.org", Description: "Referencia web oficial para HTML, CSS, JavaScript, APIs del navegador, WebGL y WebGPU.", UseFor: []string{"html", "css", "javascript", "dom", "canvas", "webgl", "webgpu"}, URLs: []string{"https://developer.mozilla.org/en-US/docs/Web/HTML", "https://developer.mozilla.org/en-US/docs/Web/JavaScript", "https://developer.mozilla.org/en-US/docs/Web/CSS", "https://developer.mozilla.org/en-US/docs/Web/API/WebGPU"}},
		{Domain: "html.spec.whatwg.org", Description: "Especificación oficial del estándar HTML.", UseFor: []string{"html", "semántica", "accesibilidad"}, URLs: []string{"https://html.spec.whatwg.org/"}},
		{Domain: "tc39.es", Description: "Especificación ECMAScript, base del lenguaje JavaScript.", UseFor: []string{"javascript", "ecmascript"}, URLs: []string{"https://tc39.es/ecma262/"}},
		{Domain: "php.net", Description: "Manual oficial del lenguaje PHP y sus extensiones.", UseFor: []string{"php", "backend", "laravel"}, URLs: []string{"https://www.php.net/docs.php"}},
		{Domain: "react.dev", Description: "Documentación oficial de React para interfaces y componentes.", UseFor: []string{"react", "frontend", "componentes"}, URLs: []string{"https://react.dev/learn"}},
		{Domain: "vite.dev", Description: "Documentación oficial de Vite para desarrollo y servidores frontend.", UseFor: []string{"vite", "node", "frontend", "dev server"}, URLs: []string{"https://vite.dev/guide/"}},
		{Domain: "nodejs.org", Description: "Documentación oficial de Node.js y sus APIs.", UseFor: []string{"node", "javascript", "servidor"}, URLs: []string{"https://nodejs.org/docs/latest/api/"}},
		{Domain: "typescriptlang.org", Description: "Documentación oficial de TypeScript.", UseFor: []string{"typescript", "ts", "javascript"}, URLs: []string{"https://www.typescriptlang.org/docs/"}},
		{Domain: "web.dev", Description: "Guías de rendimiento, accesibilidad y compatibilidad web.", UseFor: []string{"performance", "accessibility", "web"}, URLs: []string{"https://web.dev/learn/"}},
		{Domain: "cdn.jsdelivr.net", Description: "CDN de jsDelivr para descargar paquetes npm versionados como Three.js, CSS o JavaScript.", UseFor: []string{"cdn", "npm", "three.js", "javascript", "css", "dependencias"}, URLs: []string{"https://cdn.jsdelivr.net/"}},
		{Domain: "unpkg.com", Description: "CDN de paquetes npm para vendorizar archivos JavaScript y CSS concretos.", UseFor: []string{"cdn", "npm", "javascript", "css", "dependencias"}, URLs: []string{"https://unpkg.com/"}},
		{Domain: "cdnjs.cloudflare.com", Description: "CDN público de librerías web versionadas, útil para demos estáticas.", UseFor: []string{"cdn", "javascript", "css", "dependencias"}, URLs: []string{"https://cdnjs.cloudflare.com/"}},
		{Domain: "esm.sh", Description: "CDN ESM para importar paquetes npm desde módulos JavaScript del navegador.", UseFor: []string{"cdn", "esm", "npm", "javascript"}, URLs: []string{"https://esm.sh/"}},
		{Domain: "raw.githubusercontent.com", Description: "Archivos raw de repositorios públicos; usar solo cuando no exista una fuente oficial o CDN versionado.", UseFor: []string{"github", "raw", "dependencias", "ejemplos"}, URLs: []string{"https://raw.githubusercontent.com/"}},
	}
}

func mergeScrapingConfig(current ScrapingConfig) ScrapingConfig {
	defaults := DefaultConfig("").Scraping
	current.AllowedDomains = appendUniqueStrings(current.AllowedDomains, defaults.AllowedDomains)
	current.AllowedURLs = appendUniqueStrings(current.AllowedURLs, defaults.AllowedURLs)
	byDomain := make(map[string]AllowedSource, len(current.Sources)+len(defaults.Sources))
	order := make([]string, 0, len(current.Sources)+len(defaults.Sources))
	for _, source := range append(current.Sources, defaults.Sources...) {
		domain := strings.ToLower(strings.TrimSpace(source.Domain))
		if domain == "" {
			continue
		}
		key := domain
		if existing, ok := byDomain[key]; ok {
			existing.Description = firstNonEmpty(existing.Description, source.Description)
			existing.UseFor = appendUniqueStrings(existing.UseFor, source.UseFor)
			existing.URLs = appendUniqueStrings(existing.URLs, source.URLs)
			byDomain[key] = existing
			continue
		}
		source.Domain = domain
		source.UseFor = uniqueStrings(source.UseFor)
		source.URLs = uniqueStrings(source.URLs)
		byDomain[key] = source
		order = append(order, key)
	}
	current.Sources = make([]AllowedSource, 0, len(order))
	for _, key := range order {
		current.Sources = append(current.Sources, byDomain[key])
	}
	if len(current.Sources) > 0 && !current.RequireConfirmation {
		// Keep an explicit false value; confirmation remains configurable.
	}
	return current
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func appendUniqueStrings(existing, additions []string) []string {
	result := append([]string(nil), existing...)
	seen := make(map[string]struct{}, len(result))
	for _, value := range result {
		seen[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	for _, value := range additions {
		trimmed := strings.TrimSpace(value)
		key := strings.ToLower(trimmed)
		if trimmed != "" && key != "" {
			if _, ok := seen[key]; !ok {
				result = append(result, trimmed)
				seen[key] = struct{}{}
			}
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	return appendUniqueStrings(nil, values)
}

func scrapingConfigChanged(original []byte, config Config) (bool, error) {
	updated, err := json.Marshal(config)
	if err != nil {
		return false, err
	}
	var before, after interface{}
	if err := json.Unmarshal(original, &before); err != nil {
		return false, err
	}
	if err := json.Unmarshal(updated, &after); err != nil {
		return false, err
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	return string(beforeJSON) != string(afterJSON), nil
}

func SaveConfig(config Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c Config) SortedProfiles() []ModelProfile {
	profiles := append([]ModelProfile(nil), c.Profiles...)
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles
}

func (c Config) FindProfile(name string) (ModelProfile, bool) {
	for _, profile := range c.Profiles {
		if strings.EqualFold(profile.Name, name) {
			return profile, true
		}
	}
	return ModelProfile{}, false
}
