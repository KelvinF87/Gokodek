package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// ServerConfig describes a user-configured MCP server.
type ServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled,omitempty"`
}

// Tool mirrors the MCP tools/list entry.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolCallResult is the outcome of an MCP tools/call.
type ToolCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// Server is a live connection to one MCP server process.
type Server struct {
	config  ServerConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	reader  *bufio.Reader
	mu      sync.Mutex
	nextID  int
	pending map[int]chan json.RawMessage
	done    chan struct{}
}

// NewServer starts an MCP server process and performs the initialize handshake.
func NewServer(ctx context.Context, config ServerConfig) (*Server, error) {
	if strings.TrimSpace(config.Command) == "" {
		return nil, fmt.Errorf("mcp server %q: command is empty", config.Name)
	}
	cmd := exec.CommandContext(ctx, config.Command, config.Args...)
	cmd.Env = environWith(config.Env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard // MCP servers log to stderr; we ignore it.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp start %s: %w", config.Name, err)
	}
	s := &Server{
		config:  config,
		cmd:     cmd,
		stdin:   stdin,
		reader:  bufio.NewReader(stdout),
		pending: map[int]chan json.RawMessage{},
		done:    make(chan struct{}),
	}
	go s.readLoop()

	// Initialize handshake.
	initResp, err := s.call(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "gokodek", "version": "dev"},
	})
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("mcp initialize %s: %w", config.Name, err)
	}
	_ = initResp
	// Send the initialized notification (no id).
	s.notify("notifications/initialized", map[string]interface{}{})
	return s, nil
}

// ListTools returns the tools advertised by the server.
func (s *Server) ListTools(ctx context.Context) ([]Tool, error) {
	resp, err := s.call(ctx, "tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp, &payload); err != nil {
		return nil, err
	}
	return payload.Tools, nil
}

// CallTool invokes a server tool and returns its text output.
func (s *Server) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (string, error) {
	params := map[string]interface{}{"name": name}
	if arguments != nil {
		params["arguments"] = arguments
	}
	resp, err := s.call(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}
	var result ToolCallResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, piece := range result.Content {
		if piece.Type == "text" || piece.Text != "" {
			sb.WriteString(piece.Text)
		}
	}
	if result.IsError {
		return sb.String(), fmt.Errorf("mcp tool %s returned an error", name)
	}
	return sb.String(), nil
}

// Close terminates the server process.
func (s *Server) Close() error {
	select {
	case <-s.done:
		return nil
	default:
		close(s.done)
	}
	_ = s.stdin.Close()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

func (s *Server) notify(method string, params map[string]interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	data, _ := json.Marshal(msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.stdin.Write(append(data, '\n'))
}

func (s *Server) call(ctx context.Context, method string, params map[string]interface{}) (json.RawMessage, error) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	ch := make(chan json.RawMessage, 1)
	s.pending[id] = ch
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	_, err = s.stdin.Write(append(data, '\n'))
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("mcp server closed")
	case raw := <-ch:
		// JSON-RPC allows an "error" member.
		var envelope struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, err
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("mcp error: %s", envelope.Error.Message)
		}
		return envelope.Result, nil
	}
}

func (s *Server) readLoop() {
	defer close(s.done)
	decoder := json.NewDecoder(s.reader)
	for {
		var envelope struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Params json.RawMessage `json:"params"`
		}
		if err := decoder.Decode(&envelope); err != nil {
			return
		}
		if envelope.ID != nil {
			s.mu.Lock()
			ch := s.pending[*envelope.ID]
			delete(s.pending, *envelope.ID)
			s.mu.Unlock()
			if ch != nil {
				// Re-marshal the whole envelope to preserve "error"/"result".
				raw, _ := json.Marshal(envelope)
				ch <- raw
			}
			continue
		}
		// Server-initiated notifications (e.g. tools/list_changed) are ignored.
		_ = envelope.Method
		_ = envelope.Params
	}
}

func environWith(extra map[string]string) []string {
	env := append([]string(nil), environ()...)
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func environ() []string {
	// exec.Cmd already inherits the parent environment when Env is nil, but we
	// build a copy so we can add keys portably.
	type envEntry struct{ key, value string }
	// Fallback: read os.Environ directly.
	return readEnviron()
}
