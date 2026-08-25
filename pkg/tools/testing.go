package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RunTestTool executes project tests (go test, npm test, etc.)
type RunTestTool struct {
	Workspace string
}

func NewRunTestTool(workspace string) *RunTestTool {
	return &RunTestTool{Workspace: workspace}
}

func (t *RunTestTool) Name() string { return "run_test" }
func (t *RunTestTool) Description() string {
	return "Run project tests (go test, npm test, pytest, etc.) and return results."
}
func (t *RunTestTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Optional test pattern or file to run specific tests",
			},
		},
		"required": []string{},
	}
}

func (t *RunTestTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// Detect project type and run appropriate test command
	if t.fileExists("go.mod") {
		return t.runGoTest(args.Pattern)
	}
	if t.fileExists("package.json") {
		return t.runNpmTest(args.Pattern)
	}
	if t.fileExists("requirements.txt") || t.fileExists("pyproject.toml") {
		return t.runPytest(args.Pattern)
	}

	return "", fmt.Errorf("no test framework detected (no go.mod, package.json, or requirements.txt found)")
}

func (t *RunTestTool) runGoTest(pattern string) (string, error) {
	args := []string{"test", "-v", "-timeout", "60s"}
	if pattern != "" {
		args = append(args, pattern)
	} else {
		args = append(args, "./...")
	}
	return t.execCommand("go", args...)
}

func (t *RunTestTool) runNpmTest(pattern string) (string, error) {
	args := []string{"test"}
	if pattern != "" {
		args = append(args, "--", pattern)
	}
	return t.execCommand("npm", args...)
}

func (t *RunTestTool) runPytest(pattern string) (string, error) {
	args := []string{"-v"}
	if pattern != "" {
		args = append(args, pattern)
	} else {
		args = append(args, ".")
	}
	return t.execCommand("pytest", args...)
}

func (t *RunTestTool) execCommand(name string, args ...string) (string, error) {
	ctx, cancel := contextWithTimeout(120 * time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = t.Workspace

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n--- STDERR ---\n" + stderr.String()
	}

	if err != nil {
		return output, fmt.Errorf("test failed: %w", err)
	}
	return output, nil
}

func (t *RunTestTool) fileExists(name string) bool {
	path := filepath.Join(t.Workspace, name)
	_, err := exec.Command("test", "-f", path).CombinedOutput()
	if runtime.GOOS == "windows" {
		_, err = exec.Command("cmd", "/c", "if", "exist", path, "echo yes").CombinedOutput()
	}
	return err == nil
}

// LintTool runs linter on the project
type LintTool struct {
	Workspace string
}

func NewLintTool(workspace string) *LintTool {
	return &LintTool{Workspace: workspace}
}

func (t *LintTool) Name() string { return "lint" }
func (t *LintTool) Description() string {
	return "Run linter on the project (golangci-lint, eslint, etc.)"
}
func (t *LintTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}
}

func (t *LintTool) Execute(argsJSON string) (string, error) {
	ctx, cancel := contextWithTimeout(60 * time.Second)
	defer cancel()

	// Try Go linter first
	if t.fileExists("go.mod") {
		cmd := exec.CommandContext(ctx, "go", "vet", "./...")
		cmd.Dir = t.Workspace
		var output strings.Builder
		cmd.Stdout = &output
		cmd.Stderr = &output
		err := cmd.Run()
		result := output.String()
		if err != nil {
			return result, fmt.Errorf("lint issues found: %w", err)
		}
		return "go vet: no issues found\n" + result, nil
	}

	// Try eslint
	if t.fileExists("package.json") {
		cmd := exec.CommandContext(ctx, "npx", "eslint", ".", "--max-warnings=0")
		cmd.Dir = t.Workspace
		var output strings.Builder
		cmd.Stdout = &output
		cmd.Stderr = &output
		err := cmd.Run()
		result := output.String()
		if err != nil {
			return result, fmt.Errorf("lint issues found: %w", err)
		}
		return "eslint: no issues found\n" + result, nil
	}

	return "", fmt.Errorf("no linter configured for this project type")
}

func (t *LintTool) fileExists(name string) bool {
	path := filepath.Join(t.Workspace, name)
	_, err := exec.Command("test", "-f", path).CombinedOutput()
	if runtime.GOOS == "windows" {
		_, err = exec.Command("cmd", "/c", "if", "exist", path, "echo yes").CombinedOutput()
	}
	return err == nil
}

// TypeCheckTool runs type checking on the project
type TypeCheckTool struct {
	Workspace string
}

func NewTypeCheckTool(workspace string) *TypeCheckTool {
	return &TypeCheckTool{Workspace: workspace}
}

func (t *TypeCheckTool) Name() string { return "typecheck" }
func (t *TypeCheckTool) Description() string {
	return "Run type checking (go build, tsc --noEmit, mypy, etc.)"
}
func (t *TypeCheckTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}
}

func (t *TypeCheckTool) Execute(argsJSON string) (string, error) {
	ctx, cancel := contextWithTimeout(60 * time.Second)
	defer cancel()

	// Go type check
	if t.fileExists("go.mod") {
		cmd := exec.CommandContext(ctx, "go", "build", "./...")
		cmd.Dir = t.Workspace
		var output strings.Builder
		cmd.Stdout = &output
		cmd.Stderr = &output
		err := cmd.Run()
		result := output.String()
		if err != nil {
			return result, fmt.Errorf("type errors: %w", err)
		}
		return "go build: all packages compile successfully", nil
	}

	// TypeScript type check
	if t.fileExists("tsconfig.json") {
		cmd := exec.CommandContext(ctx, "npx", "tsc", "--noEmit")
		cmd.Dir = t.Workspace
		var output strings.Builder
		cmd.Stdout = &output
		cmd.Stderr = &output
		err := cmd.Run()
		result := output.String()
		if err != nil {
			return result, fmt.Errorf("type errors: %w", err)
		}
		return "tsc: no type errors", nil
	}

	// Python type check with mypy
	if t.fileExists("pyproject.toml") || t.fileExists("setup.cfg") {
		cmd := exec.CommandContext(ctx, "mypy", ".")
		cmd.Dir = t.Workspace
		var output strings.Builder
		cmd.Stdout = &output
		cmd.Stderr = &output
		err := cmd.Run()
		result := output.String()
		if err != nil {
			return result, fmt.Errorf("type errors: %w", err)
		}
		return "mypy: no type errors", nil
	}

	return "", fmt.Errorf("no type checker configured for this project type")
}

func (t *TypeCheckTool) fileExists(name string) bool {
	path := filepath.Join(t.Workspace, name)
	_, err := exec.Command("test", "-f", path).CombinedOutput()
	if runtime.GOOS == "windows" {
		_, err = exec.Command("cmd", "/c", "if", "exist", path, "echo yes").CombinedOutput()
	}
	return err == nil
}

// helper to create context with timeout
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
