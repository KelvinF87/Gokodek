package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ChatProvider is the extension point for Ollama and remote providers.
type ChatProvider interface {
	Chat(ctx context.Context, request ChatRequest) (ChatResponse, error)
	ChatStream(ctx context.Context, request ChatRequest, onContent func(string), onThinking func(string)) (Message, Stats, error)
}

type ProviderInfo struct {
	Name        string
	Configured  bool
	Endpoint    string
	APIKeyEnv   string
	Description string
}

func ProviderInfos() []ProviderInfo {
	return []ProviderInfo{
		{Name: "ollama", Configured: true, Endpoint: DefaultOllamaEndpoint, Description: "Modelos locales sin API key."},
		{Name: "openai", Configured: os.Getenv("OPENAI_API_KEY") != "", Endpoint: "https://api.openai.com/v1/chat/completions", APIKeyEnv: "OPENAI_API_KEY", Description: "API OpenAI."},
		{Name: "gemini", Configured: os.Getenv("GEMINI_API_KEY") != "", Endpoint: "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent", APIKeyEnv: "GEMINI_API_KEY", Description: "API Gemini."},
		{Name: "openrouter", Configured: os.Getenv("OPENROUTER_API_KEY") != "", Endpoint: "https://openrouter.ai/api/v1/chat/completions", APIKeyEnv: "OPENROUTER_API_KEY", Description: "API compatible con OpenAI."},
	}
}

type OpenAICompatibleConfig struct {
	Name      string
	BaseURL   string
	APIKeyEnv string
}

var OpenAICompatibleProviders = []OpenAICompatibleConfig{
	{Name: "openai", BaseURL: "https://api.openai.com/v1/chat/completions", APIKeyEnv: "OPENAI_API_KEY"},
	{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1/chat/completions", APIKeyEnv: "OPENROUTER_API_KEY"},
}

func ValidateProvider(profile ModelProfile) error {
	provider := strings.ToLower(strings.TrimSpace(profile.Provider))
	switch provider {
	case "ollama":
		return nil
	case "openai":
		if os.Getenv("OPENAI_API_KEY") == "" {
			return fmt.Errorf("provider openai requiere OPENAI_API_KEY o api_keys.openai")
		}
	case "gemini":
		if os.Getenv("GEMINI_API_KEY") == "" {
			return fmt.Errorf("provider gemini requiere GEMINI_API_KEY o api_keys.gemini")
		}
	case "openrouter":
		if os.Getenv("OPENROUTER_API_KEY") == "" {
			return fmt.Errorf("provider openrouter requiere OPENROUTER_API_KEY o api_keys.openrouter")
		}
	default:
		return fmt.Errorf("proveedor no soportado: %s", profile.Provider)
	}
	return nil
}

// RemoteClient implements OpenAI-compatible APIs and Gemini's native API.
type RemoteClient struct {
	Provider   string
	APIKey     string
	HTTPClient *http.Client
}

func NewRemoteClient(provider, apiKey string) *RemoteClient {
	return &RemoteClient{Provider: strings.ToLower(strings.TrimSpace(provider)), APIKey: strings.TrimSpace(apiKey), HTTPClient: &http.Client{Timeout: 10 * time.Minute}}
}

func (c *RemoteClient) Chat(ctx context.Context, request ChatRequest) (ChatResponse, error) {
	message, _, err := c.chat(ctx, request, nil)
	return ChatResponse{Model: request.Model, Message: message, Done: true}, err
}

func (c *RemoteClient) ChatStream(ctx context.Context, request ChatRequest, onContent func(string), onThinking func(string)) (Message, Stats, error) {
	return c.chat(ctx, request, onContent)
}

func (c *RemoteClient) chat(ctx context.Context, request ChatRequest, onContent func(string)) (Message, Stats, error) {
	if c.APIKey == "" {
		return Message{}, Stats{}, fmt.Errorf("API key no configurada para %s", c.Provider)
	}
	started := time.Now()
	var message Message
	var err error
	switch c.Provider {
	case "openai", "openrouter":
		message, err = c.chatOpenAI(ctx, request, onContent)
	case "gemini":
		message, err = c.chatGemini(ctx, request, onContent)
	default:
		err = fmt.Errorf("proveedor remoto no soportado: %s", c.Provider)
	}
	return message, Stats{TotalDuration: time.Since(started)}, err
}

type openAIRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Stream      bool             `json:"stream"`
	Temperature float64          `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
}

type openAIResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *RemoteClient) chatOpenAI(ctx context.Context, request ChatRequest, onContent func(string)) (Message, error) {
	endpoint := "https://api.openai.com/v1/chat/completions"
	if c.Provider == "openrouter" {
		endpoint = "https://openrouter.ai/api/v1/chat/completions"
	}
	payload, err := json.Marshal(openAIRequest{Model: request.Model, Messages: request.Messages, Tools: request.Tools, Stream: false, MaxTokens: optionInt(request.Options, "num_predict"), Temperature: optionFloat(request.Options, "temperature")})
	if err != nil {
		return Message{}, fmt.Errorf("encode %s request: %w", c.Provider, err)
	}
	body, err := c.post(ctx, endpoint, payload, map[string]string{"Authorization": "Bearer " + c.APIKey})
	if err != nil {
		return Message{}, err
	}
	var response openAIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return Message{}, fmt.Errorf("decode %s response: %w", c.Provider, err)
	}
	if len(response.Choices) == 0 {
		return Message{}, fmt.Errorf("%s no devolvió choices", c.Provider)
	}
	message := response.Choices[0].Message
	if onContent != nil && message.Content != "" {
		onContent(message.Content)
	}
	return message, nil
}

type geminiRequest struct {
	Contents          []geminiContent        `json:"contents"`
	Tools             []geminiToolWrapper    `json:"tools,omitempty"`
	SystemInstruction *geminiContent         `json:"systemInstruction,omitempty"`
	GenerationConfig  map[string]interface{} `json:"generationConfig,omitempty"`
}
type geminiToolWrapper struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}
type geminiFunctionDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}
type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}
type geminiPart struct {
	Text             string                 `json:"text,omitempty"`
	FunctionCall     map[string]interface{} `json:"functionCall,omitempty"`
	FunctionResponse map[string]interface{} `json:"functionResponse,omitempty"`
	ThoughtSignature string                 `json:"thoughtSignature,omitempty"`
}
type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
}

func (c *RemoteClient) chatGemini(ctx context.Context, request ChatRequest, onContent func(string)) (Message, error) {
	contents := make([]geminiContent, 0, len(request.Messages))
	for _, msg := range request.Messages {
		if msg.Role == "system" {
			continue
		}
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		parts := make([]geminiPart, 0, 1+len(msg.ToolCalls))
		if strings.TrimSpace(msg.Content) != "" {
			parts = append(parts, geminiPart{Text: msg.Content})
		}
		for _, call := range msg.ToolCalls {
			var args map[string]interface{}
			_ = json.Unmarshal(call.Function.Arguments, &args)
			functionCall := map[string]interface{}{"name": call.Function.Name, "args": args}
			part := geminiPart{FunctionCall: functionCall}
			if strings.HasPrefix(call.Type, "function:") {
				part.ThoughtSignature = strings.TrimPrefix(call.Type, "function:")
			}
			parts = append(parts, part)
		}
		if msg.Role == "tool" {
			parts = []geminiPart{{FunctionResponse: map[string]interface{}{"name": msg.ToolName, "response": map[string]interface{}{"result": msg.Content}}}}
		}
		if len(parts) > 0 {
			contents = append(contents, geminiContent{Role: role, Parts: parts})
		}
	}
	var system *geminiContent
	for _, msg := range request.Messages {
		if msg.Role == "system" {
			system = &geminiContent{Parts: []geminiPart{{Text: msg.Content}}}
			break
		}
	}
	endpoint := "https://generativelanguage.googleapis.com/v1beta/models/" + request.Model + ":generateContent?key=" + c.APIKey
	declarations := make([]geminiFunctionDeclaration, 0, len(request.Tools))
	for _, tool := range request.Tools {
		declarations = append(declarations, geminiFunctionDeclaration{Name: tool.Function.Name, Description: tool.Function.Description, Parameters: tool.Function.Parameters})
	}
	payload, err := json.Marshal(geminiRequest{Contents: contents, Tools: []geminiToolWrapper{{FunctionDeclarations: declarations}}, SystemInstruction: system, GenerationConfig: map[string]interface{}{"maxOutputTokens": optionInt(request.Options, "num_predict"), "temperature": optionFloat(request.Options, "temperature")}})
	if err != nil {
		return Message{}, fmt.Errorf("encode Gemini request: %w", err)
	}
	body, err := c.post(ctx, endpoint, payload, nil)
	if err != nil {
		return Message{}, err
	}
	var response geminiResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return Message{}, fmt.Errorf("decode Gemini response: %w", err)
	}
	if len(response.Candidates) == 0 || len(response.Candidates[0].Content.Parts) == 0 {
		return Message{}, fmt.Errorf("Gemini no devolvió contenido")
	}
	var text string
	var calls []ToolCall
	for _, part := range response.Candidates[0].Content.Parts {
		if part.Text != "" {
			text += part.Text
		}
		if part.FunctionCall != nil {
			name, _ := part.FunctionCall["name"].(string)
			args, _ := json.Marshal(part.FunctionCall["args"])
			call := ToolCall{Type: "function", Function: FunctionCall{Name: name, Arguments: args}}
			if part.ThoughtSignature != "" {
				call.Type = "function:" + part.ThoughtSignature
			}
			calls = append(calls, call)
		}
	}
	if text == "" && len(calls) == 0 {
		return Message{}, fmt.Errorf("Gemini no devolvió contenido")
	}
	message := Message{Role: "assistant", Content: text, ToolCalls: calls}
	if onContent != nil {
		onContent(text)
	}
	return message, nil
}

func (c *RemoteClient) post(ctx context.Context, endpoint string, payload []byte, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", c.Provider, err)
	}
	defer res.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if readErr != nil {
		return nil, readErr
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned HTTP %d: %s", c.Provider, res.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func optionInt(options map[string]interface{}, key string) int {
	if value, ok := options[key].(int); ok {
		return value
	}
	return 0
}
func optionFloat(options map[string]interface{}, key string) float64 {
	if value, ok := options[key].(float64); ok {
		return value
	}
	if value, ok := options[key].(int); ok {
		return float64(value)
	}
	return 0.2
}
