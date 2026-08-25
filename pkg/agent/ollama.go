package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultOllamaEndpoint = "http://localhost:11434/api/chat"

type Message struct {
	Role      string     `json:"role"`
	ToolName  string     `json:"tool_name,omitempty"`
	Content   string     `json:"content,omitempty"`
	Thinking  string     `json:"thinking,omitempty"`
	Images    []string   `json:"images,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Index     int             `json:"index,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ChatRequest struct {
	Model     string                 `json:"model"`
	Messages  []Message              `json:"messages"`
	Tools     []ToolDefinition       `json:"tools,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
	Stream    bool                   `json:"stream"`
	Think     *bool                  `json:"think,omitempty"`
	KeepAlive string                 `json:"keep_alive,omitempty"`
}

type ChatResponse struct {
	Model              string  `json:"model,omitempty"`
	Message            Message `json:"message"`
	Done               bool    `json:"done"`
	PromptEvalCount    int     `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64   `json:"prompt_eval_duration,omitempty"`
	EvalCount          int     `json:"eval_count,omitempty"`
	EvalDuration       int64   `json:"eval_duration,omitempty"`
}

// Stats holds token and timing statistics for a single response or accumulated session.
type Stats struct {
	PromptTokens    int           `json:"prompt_tokens"`
	GeneratedTokens int           `json:"generated_tokens"`
	PromptDuration  time.Duration `json:"prompt_duration"`
	EvalDuration    time.Duration `json:"eval_duration"`
	TotalDuration   time.Duration `json:"total_duration"`
}

// TokensPerSecond returns generated tokens per second.
func (s Stats) TokensPerSecond() float64 {
	if s.EvalDuration.Seconds() == 0 {
		return 0
	}
	return float64(s.GeneratedTokens) / s.EvalDuration.Seconds()
}

// FormatStats returns a compact stats line for display.
func (s Stats) FormatStats() string {
	tps := s.TokensPerSecond()
	if s.TotalDuration == 0 {
		return ""
	}
	return fmt.Sprintf("tokens %d prompt + %d gen · %.1f tok/s · %s",
		s.PromptTokens, s.GeneratedTokens, tps, s.TotalDuration.Round(time.Millisecond))
}

// SessionStats accumulates stats across multiple turns.
type SessionStats struct {
	TotalPromptTokens    int
	TotalGeneratedTokens int
	TotalDuration        time.Duration
	Turns                int
}

// Add incorporates stats from a single response.
func (s *SessionStats) Add(stats Stats) {
	s.TotalPromptTokens += stats.PromptTokens
	s.TotalGeneratedTokens += stats.GeneratedTokens
	s.TotalDuration += stats.TotalDuration
	s.Turns++
}

// FormatSession returns a summary line for the session.
func (s SessionStats) FormatSession() string {
	return fmt.Sprintf("session: %d turns · %d prompt + %d gen tokens · %s total",
		s.Turns, s.TotalPromptTokens, s.TotalGeneratedTokens, s.TotalDuration.Round(time.Millisecond))
}

// OllamaClient talks directly to Ollama without an SDK or external dependency.
type OllamaClient struct {
	Endpoint   string
	HTTPClient *http.Client
}

func NewOllamaClient(endpoint string) *OllamaClient {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = DefaultOllamaEndpoint
	}
	return &OllamaClient{
		Endpoint: strings.TrimRight(endpoint, "/"),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

func (c *OllamaClient) Chat(ctx context.Context, request ChatRequest) (ChatResponse, error) {
	request.Stream = false
	res, err := c.doRequest(ctx, request)
	if err != nil {
		return ChatResponse{}, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("read Ollama response: %w", err)
	}
	var response ChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return response, fmt.Errorf("decode Ollama response: %w", err)
	}
	return response, nil
}

// ChatStream decodes Ollama's newline-delimited response and emits generated text
// as it arrives. Tool calls are returned only after the stream has completed.
// onThinking, when non-nil, receives thinking tokens for models that support it.
func (c *OllamaClient) ChatStream(ctx context.Context, request ChatRequest, onContent func(string), onThinking func(string)) (Message, Stats, error) {
	request.Stream = true
	startTime := time.Now()
	res, err := c.doRequest(ctx, request)
	if err != nil {
		return Message{}, Stats{}, err
	}
	defer res.Body.Close()

	var final Message
	var stats Stats
	decoder := json.NewDecoder(res.Body)
	for {
		var chunk ChatResponse
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return final, stats, fmt.Errorf("decode Ollama stream: %w", err)
		}
		if chunk.Message.Role != "" {
			final.Role = chunk.Message.Role
		}
		if chunk.Message.Content != "" {
			final.Content += chunk.Message.Content
			if onContent != nil {
				onContent(chunk.Message.Content)
			}
		}
		if chunk.Message.Thinking != "" {
			final.Thinking += chunk.Message.Thinking
			if onThinking != nil {
				onThinking(chunk.Message.Thinking)
			}
		}
		if len(chunk.Message.ToolCalls) > 0 {
			final.ToolCalls = append(final.ToolCalls, chunk.Message.ToolCalls...)
		}
		// Capture final stats from last chunk with Done=true
		if chunk.Done {
			stats.PromptTokens = chunk.PromptEvalCount
			stats.GeneratedTokens = chunk.EvalCount
			if chunk.PromptEvalDuration > 0 {
				stats.PromptDuration = time.Duration(chunk.PromptEvalDuration)
			}
			if chunk.EvalDuration > 0 {
				stats.EvalDuration = time.Duration(chunk.EvalDuration)
			}
		}
	}
	stats.TotalDuration = time.Since(startTime)
	return final, stats, nil
}

// IsToolsUnsupportedError reports whether the error is Ollama rejecting the
// request because the selected model does not implement tool calling.
func IsToolsUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "does not support tools") || strings.Contains(message, "does not support the tools")
}

func (c *OllamaClient) doRequest(ctx context.Context, request ChatRequest) (*http.Response, error) {
	if strings.TrimSpace(request.Model) == "" {
		return nil, fmt.Errorf("Ollama model cannot be empty")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode Ollama request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create Ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call Ollama: %w", err)
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("Ollama returned HTTP %d (read error: %v)", res.StatusCode, readErr)
		}
		return nil, fmt.Errorf("Ollama returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return res, nil
}
