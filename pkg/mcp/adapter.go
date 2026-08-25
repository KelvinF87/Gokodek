package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolAdapter bridges a live MCP server's tools into gokodek's Tool interface.
type ToolAdapter struct {
	ServerName string
	Tool       Tool
	server     *Server
}

// NewToolAdapter builds an adapter for one MCP tool.
func NewToolAdapter(serverName string, tool Tool, server *Server) *ToolAdapter {
	return &ToolAdapter{ServerName: serverName, Tool: tool, server: server}
}

// FullName returns a unique tool name in the registry, prefixed by server.
func (a *ToolAdapter) FullName() string {
	return sanitizeName(a.ServerName) + "_" + sanitizeName(a.Tool.Name)
}

func (a *ToolAdapter) Name() string { return a.FullName() }

func (a *ToolAdapter) Description() string {
	description := a.Tool.Description
	if description == "" {
		description = "Tool provided by MCP server " + a.ServerName
	}
	return description
}

func (a *ToolAdapter) Parameters() map[string]interface{} {
	if a.Tool.InputSchema != nil {
		return a.Tool.InputSchema
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (a *ToolAdapter) Execute(argsJSON string) (string, error) {
	var arguments map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" && argsJSON != "{}" {
		if err := json.Unmarshal([]byte(argsJSON), &arguments); err != nil {
			return "", fmt.Errorf("mcp %s arguments: %w", a.FullName(), err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return a.server.CallTool(ctx, a.Tool.Name, arguments)
}

// ConnectServer starts an MCP server and returns its tool adapters plus the
// server handle for later cleanup.
func ConnectServer(config ServerConfig) ([]*ToolAdapter, *Server, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server, err := NewServer(ctx, config)
	if err != nil {
		return nil, nil, err
	}
	tools, err := server.ListTools(ctx)
	if err != nil {
		server.Close()
		return nil, nil, err
	}
	adapters := make([]*ToolAdapter, 0, len(tools))
	for _, tool := range tools {
		adapters = append(adapters, NewToolAdapter(config.Name, tool, server))
	}
	return adapters, server, nil
}

// sanitizeName makes a tool/server name registry-safe.
func sanitizeName(value string) string {
	var sb strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '_' || r == '-' || r == '.':
			sb.WriteRune('_')
		default:
			sb.WriteRune('_')
		}
	}
	name := sb.String()
	if name == "" {
		return "mcp_tool"
	}
	return name
}
