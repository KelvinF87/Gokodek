package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type ServerTool struct{ Workspace string }

type serverState struct {
	Runtime  string `json:"runtime"`
	URL      string `json:"url"`
	Launcher string `json:"launcher"`
	Log      string `json:"log"`
	PID      int    `json:"pid"`
	Started  string `json:"started"`
}

type serverPlan struct {
	Runtime string
	Command string
	Args    []string
}

func NewServerTool(workspace string) *ServerTool {
	return &ServerTool{Workspace: filepath.Clean(workspace)}
}
func (t *ServerTool) Name() string { return "start_server" }
func (t *ServerTool) Description() string {
	return "Start or reuse a verified HTTP server owned by gokodek for the current project. Creates one reusable .gokodek/gokodek-start.bat launcher on Windows, detects PHP/Node/Vite/Python/static HTML, stores the real URL, and never reports success for a 404 or an unrelated server."
}
func (t *ServerTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"port":      map[string]interface{}{"type": "integer", "description": "Optional port; default 4173. The tool chooses the next free port when necessary."},
		"directory": map[string]interface{}{"type": "string", "description": "Optional directory path relative to workspace or absolute path to the project to serve."},
		"url":       map[string]interface{}{"type": "string", "description": "Optional already-configured HTTP URL to verify, such as http://localhost/project/. Use this only when the user explicitly requests an existing Apache/Laragon server."},
	})
}

func (t *ServerTool) Execute(raw string) (string, error) {
	var args struct {
		Port      int    `json:"port"`
		Directory string `json:"directory"`
		Path      string `json:"project_path"`
		URL       string `json:"url"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", fmt.Errorf("start_server arguments: %w", err)
	}

	targetDir := t.Workspace
	dirInput := strings.TrimSpace(args.Directory)
	if dirInput == "" {
		dirInput = strings.TrimSpace(args.Path)
	}
	if dirInput != "" {
		if filepath.IsAbs(dirInput) {
			targetDir = filepath.Clean(dirInput)
		} else {
			targetDir = filepath.Clean(filepath.Join(t.Workspace, dirInput))
		}
	}

	if _, err := os.Stat(targetDir); err != nil {
		return "", fmt.Errorf("el directorio del proyecto especificado no existe: %s", targetDir)
	}

	if strings.TrimSpace(args.URL) != "" {
		if probeURL(args.URL) {
			state := serverState{Runtime: "existing", URL: args.URL, Started: time.Now().Format(time.RFC3339)}
			_ = saveServerState(targetDir, state)
			return fmt.Sprintf("server_ready runtime=existing url=%s verified=true", args.URL), nil
		}
		return "", fmt.Errorf("configured URL does not respond with a usable page: %s", args.URL)
	}

	if previous, ok := loadServerState(targetDir); ok && probeURL(previous.URL) {
		return fmt.Sprintf("server_ready runtime=%s url=%s verified=true reused=true launcher=%s", previous.Runtime, previous.URL, previous.Launcher), nil
	}

	port := args.Port
	if port < 1 || port > 65535 {
		port = 4173
	}
	port = chooseFreePort(port)
	plan, err := t.detectPlanInDir(targetDir)
	if err != nil {
		return "", err
	}

	launcher, logPath, err := writeProjectLauncher(targetDir, plan, port)
	if err != nil {
		return "", err
	}
	cmd, err := launchProjectLauncher(launcher, targetDir)
	if err != nil {
		return "", err
	}

	address := fmt.Sprintf("http://127.0.0.1:%d/", port)
	for i := 0; i < 48; i++ {
		if probeURL(address) {
			state := serverState{
				Runtime:  plan.Runtime,
				URL:      address,
				Launcher: launcher,
				Log:      logPath,
				PID:      cmd.Process.Pid,
				Started:  time.Now().Format(time.RFC3339),
			}
			if err := saveServerState(t.Workspace, state); err != nil {
				return "", err
			}
			return fmt.Sprintf("server_ready runtime=%s url=%s verified=true pid=%d launcher=%s log=%s", plan.Runtime, address, cmd.Process.Pid, launcher, logPath), nil
		}
		if cmd.ProcessState != nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return "", fmt.Errorf("server %s was launched with %s but %s did not respond; inspect %s", plan.Runtime, launcher, address, logPath)
}

func (t *ServerTool) detectPlan() (serverPlan, error) {
	return t.detectPlanInDir(t.Workspace)
}

func (t *ServerTool) detectPlanInDir(dir string) (serverPlan, error) {
	// A PHP entry point wins over package.json because many PHP apps use npm
	// only for asset compilation. Laravel is started through its own command.
	if fileExists(filepath.Join(dir, "artisan")) && commandExists("php") {
		return serverPlan{Runtime: "laravel", Command: "php", Args: []string{"artisan", "serve", "--host=127.0.0.1", "--port=PORT"}}, nil
	}
	if hasExtension(dir, ".php") {
		if !commandExists("php") {
			return serverPlan{}, fmt.Errorf("archivos PHP detectados en %s pero 'php' no está instalado o en el PATH", dir)
		}
		return serverPlan{Runtime: "php", Command: "php", Args: []string{"-S", "127.0.0.1:PORT", "-t", dir}}, nil
	}
	if packagePath := filepath.Join(dir, "package.json"); fileExists(packagePath) {
		if !commandExists("npm") && !commandExists("npm.cmd") {
			return serverPlan{}, fmt.Errorf("package.json detectado pero npm no está instalado")
		}
		for _, script := range []string{"dev", "start", "preview"} {
			if hasPackageScript(packagePath, script) {
				command := "npm"
				if runtime.GOOS == "windows" {
					command = "npm.cmd"
				}
				return serverPlan{Runtime: "node-" + script, Command: command, Args: []string{"run", script}}, nil
			}
		}
	}
	if fileExists(filepath.Join(dir, "index.html")) || fileExists(filepath.Join(dir, "index.htm")) || hasExtension(dir, ".html") {
		if commandExists("python") {
			return serverPlan{Runtime: "static-python", Command: "python", Args: []string{"-m", "http.server", "PORT", "--bind", "127.0.0.1"}}, nil
		}
		if commandExists("py") {
			return serverPlan{Runtime: "static-python", Command: "py", Args: []string{"-m", "http.server", "PORT", "--bind", "127.0.0.1"}}, nil
		}
		if commandExists("npx") || commandExists("npx.cmd") {
			cmd := "npx"
			if runtime.GOOS == "windows" {
				cmd = "npx.cmd"
			}
			return serverPlan{Runtime: "static-serve", Command: cmd, Args: []string{"-y", "serve", "-l", "PORT"}}, nil
		}
		// Fallback utilizando la herramienta integrada de servidor Go de gokodek
		return serverPlan{Runtime: "static-gokodek", Command: os.Args[0], Args: []string{"-serve-static", dir, "PORT"}}, nil
	}
	return serverPlan{}, fmt.Errorf("no se encontró punto de entrada ejecutable en %s; esperado index.html, .php, package.json o artisan", dir)
}

func writeProjectLauncher(workspace string, plan serverPlan, port int) (string, string, error) {
	metaDir := filepath.Join(workspace, ".gokodek")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		return "", "", fmt.Errorf("create .gokodek directory: %w", err)
	}
	logPath := filepath.Join(metaDir, "server.log")
	if runtime.GOOS == "windows" {
		path := filepath.Join(metaDir, "gokodek-start.bat")
		content := windowsLauncherContent(workspace, plan, port, logPath)
		if err := writeIfChanged(path, []byte(content), 0644); err != nil {
			return "", "", fmt.Errorf("write server launcher: %w", err)
		}
		return path, logPath, nil
	}
	path := filepath.Join(metaDir, "gokodek-start.sh")
	content := unixLauncherContent(workspace, plan, port, logPath)
	if err := writeIfChanged(path, []byte(content), 0755); err != nil {
		return "", "", fmt.Errorf("write server launcher: %w", err)
	}
	if err := os.Chmod(path, 0755); err != nil {
		return "", "", fmt.Errorf("make server launcher executable: %w", err)
	}
	return path, logPath, nil
}

func windowsLauncherContent(workspace string, plan serverPlan, port int, logPath string) string {
	command := launcherCommand(plan, port)
	return fmt.Sprintf("@echo off\r\nsetlocal\r\ncd /d %s\r\nset PORT=%d\r\n%s > %s 2>&1\r\n", batQuote(workspace), port, command, batQuote(logPath))
}

func unixLauncherContent(workspace string, plan serverPlan, port int, logPath string) string {
	command := launcherCommand(plan, port)
	return fmt.Sprintf("#!/bin/sh\ncd %s || exit 1\nexport PORT=%d\nexec %s >> %s 2>&1\n", shellQuote(workspace), port, command, shellQuote(logPath))
}

func launcherCommand(plan serverPlan, port int) string {
	args := make([]string, len(plan.Args))
	for i, arg := range plan.Args {
		args[i] = strings.ReplaceAll(arg, "PORT", fmt.Sprint(port))
		if strings.Contains(args[i], " ") {
			args[i] = quoteArg(args[i])
		}
	}
	command := plan.Command
	if strings.HasPrefix(plan.Runtime, "node-") {
		args = append(args, "--", "--host", "127.0.0.1", "--port", fmt.Sprint(port))
	}
	if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(command), ".cmd") {
		command = "call " + command
	}
	if len(args) > 0 {
		command += " " + strings.Join(args, " ")
	}
	return command
}

func launchProjectLauncher(launcher, workspace string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/d", "/s", "/c", "call "+batQuote(launcher))
	} else {
		cmd = exec.Command("sh", launcher)
	}
	cmd.Dir = workspace
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start launcher %s: %w", launcher, err)
	}
	return cmd, nil
}

func writeIfChanged(path string, content []byte, mode os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(content) {
		return nil
	}
	return os.WriteFile(path, content, mode)
}

func saveServerState(workspace string, state serverState) error {
	metaDir := filepath.Join(workspace, ".gokodek")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		return fmt.Errorf("create server metadata directory: %w", err)
	}
	path := filepath.Join(metaDir, "server.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode server state: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func loadServerState(workspace string) (serverState, bool) {
	data, err := os.ReadFile(filepath.Join(workspace, ".gokodek", "server.json"))
	if err != nil {
		return serverState{}, false
	}
	var state serverState
	if json.Unmarshal(data, &state) != nil || strings.TrimSpace(state.URL) == "" {
		return serverState{}, false
	}
	return state, true
}

func chooseFreePort(start int) int {
	for port := start; port <= start+20; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = listener.Close()
			return port
		}
	}
	return start
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func hasExtension(root, ext string) bool {
	found := false
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", ".gokodek", "node_modules", "vendor", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ext) {
			found = true
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

func hasPackageScript(path, script string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var packageData struct {
		Scripts map[string]string `json:"scripts"`
	}
	return json.Unmarshal(data, &packageData) == nil && strings.TrimSpace(packageData.Scripts[script]) != ""
}

func probeURL(address string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode >= 200 && res.StatusCode < 400
}

func commandExists(name string) bool { _, err := exec.LookPath(name); return err == nil }

func quoteArg(value string) string {
	if strings.ContainsAny(value, " \t") {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return value
}

func batQuote(value string) string   { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// GitTool runs common git commands inside the workspace.
type GitTool struct{ Workspace string }

func NewGitTool(workspace string) *GitTool { return &GitTool{Workspace: workspace} }
func (t *GitTool) Name() string            { return "git" }
func (t *GitTool) Description() string {
	return "Run safe git commands in the workspace; push and destructive reset operations are disabled."
}
func (t *GitTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{"action": map[string]interface{}{"type": "string", "description": "status, add, commit, log, diff, or branch"}, "message": map[string]interface{}{"type": "string"}, "files": map[string]interface{}{"type": "string"}})
}
func (t *GitTool) Execute(raw string) (string, error) {
	var a struct{ Action, Message, Files string }
	if e := json.Unmarshal([]byte(raw), &a); e != nil {
		return "", e
	}
	action := strings.ToLower(strings.TrimSpace(a.Action))
	if action == "" {
		action = "status"
	}
	if action == "push" {
		return "", fmt.Errorf("git push is disabled")
	}
	var ga []string
	switch action {
	case "status":
		ga = []string{"status"}
	case "add":
		f := strings.TrimSpace(a.Files)
		if f == "" {
			f = "."
		}
		ga = append([]string{"add"}, strings.Fields(f)...)
	case "commit":
		if strings.TrimSpace(a.Message) == "" {
			return "", fmt.Errorf("git commit requires a message")
		}
		ga = []string{"commit", "-m", a.Message}
	case "log":
		ga = []string{"log", "--oneline", "-10"}
	case "diff":
		ga = []string{"diff"}
	case "branch":
		ga = []string{"branch", "-a"}
	default:
		return "", fmt.Errorf("unknown git action %q", action)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", ga...)
	cmd.Dir = t.Workspace
	out, e := cmd.CombinedOutput()
	if e != nil {
		return string(out), fmt.Errorf("git %s failed: %w", action, e)
	}
	if len(out) == 0 {
		return "git " + action + ": sin salida", nil
	}
	return string(out), nil
}
