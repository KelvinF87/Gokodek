package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultCommandTimeout = 120 * time.Second
	maxCommandTimeout     = 120 * time.Second
	maxCommandOutput      = 512 << 10
)

type RunCmdTool struct {
	workspaceTool
}

func NewRunCmdTool(workspace string) *RunCmdTool {
	return &RunCmdTool{workspaceTool: newWorkspaceTool(workspace)}
}

func (t *RunCmdTool) Name() string { return "run_cmd" }
func (t *RunCmdTool) Description() string {
	return "Run a development command in the workspace using PowerShell on Windows or Bash/sh on Unix. Destructive system commands are rejected."
}
func (t *RunCmdTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"command":         map[string]interface{}{"type": "string", "description": "Command to run, such as go test ./... or npm test."},
		"timeout_seconds": map[string]interface{}{"type": "integer", "description": "Optional timeout from 1 to 120 seconds; defaults to 120."},
	}, "command")
}
func (t *RunCmdTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("run_cmd arguments: %w", err)
	}
	command := strings.TrimSpace(args.Command)
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}
	if strings.HasPrefix(strings.ToLower(command), "open ") {
		return "", fmt.Errorf("open is not a file inspection command; use read_file or list_dir instead")
	}
	if isDangerous(command) {
		return "", fmt.Errorf("command rejected by safety policy")
	}
	if err := validateCommandForWorkspace(command, t.workspace); err != nil {
		return "", err
	}

	timeout := defaultCommandTimeout
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	if timeout > maxCommandTimeout {
		timeout = maxCommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		shell := "powershell.exe"
		if _, err := exec.LookPath(shell); err != nil {
			shell = "pwsh.exe"
		}
		cmd = exec.CommandContext(ctx, shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-lc", command)
	}
	cmd.Dir = t.workspace
	output, err := cmd.CombinedOutput()
	result := string(output)
	if len(result) > maxCommandOutput {
		result = result[:maxCommandOutput] + "\n[output truncated]"
	}
	if ctx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("command timed out after %s", timeout)
	}
	if err != nil {
		return result, fmt.Errorf("command failed: %w", err)
	}
	if strings.TrimSpace(result) == "" {
		return "(command completed with no output)", nil
	}
	return result, nil
}

func validateCommandForWorkspace(command, workspace string) error {
	value := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if value == "npm test" || value == "npm run test" || value == "yarn test" || value == "pnpm test" {
		if _, err := os.Stat(filepath.Join(workspace, "package.json")); err != nil {
			return fmt.Errorf("no package.json in workspace; do not run %q", command)
		}
	}
	return nil
}

func isDangerous(command string) bool {
	value := strings.ToLower(strings.Join(strings.Fields(command), " "))
	blocked := []string{
		"rm -rf /", "rm -rf .", "rm -r /", "remove-item -recurse", "remove-item -force",
		"format ", "shutdown", "restart-computer", "diskpart", "mkfs", "dd if=",
		"git reset --hard", "git clean -fd", "del /s", "rmdir /s", "rd /s",
	}
	for _, pattern := range blocked {
		if strings.Contains(value, pattern) {
			return true
		}
	}
	return false
}
