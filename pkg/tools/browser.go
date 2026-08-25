package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	maxKeyboardText = 4000
	maxScreenWidth  = 10000
	maxScreenHeight = 10000
)

type BrowserScreenshotTool struct {
	workspaceTool
	visible bool
}

// NewBrowserScreenshotTool opens an isolated visible browser by default.
func NewBrowserScreenshotTool(workspace string) *BrowserScreenshotTool {
	return NewBrowserScreenshotToolWithMode(workspace, true)
}

func NewBrowserScreenshotToolWithMode(workspace string, visible bool) *BrowserScreenshotTool {
	return &BrowserScreenshotTool{workspaceTool: newWorkspaceTool(workspace), visible: visible}
}

func (t *BrowserScreenshotTool) Name() string { return "browser_screenshot" }
func (t *BrowserScreenshotTool) Description() string {
	return "Open a URL in an isolated Chrome or Edge instance, visibly when enabled, capture the rendered page, and provide the screenshot to the configured vision model."
}
func (t *BrowserScreenshotTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"url":  map[string]interface{}{"type": "string", "description": "Optional HTTP(S) URL. Do not use file:///path/to; for local files use path."},
		"path": map[string]interface{}{"type": "string", "description": "HTML path relative to the workspace, for example index.html. Defaults to index.html."},
	})
}
func (t *BrowserScreenshotTool) Execute(argsJSON string) (string, error) {
	var args struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("browser_screenshot arguments: %w", err)
	}
	address := strings.TrimSpace(args.URL)
	if strings.TrimSpace(args.Path) != "" && address == "" {
		// A path is resolved to the agent's verified HTTP server. Never open
		// HTML/PHP through file:// because that bypasses server-side execution.
		if serverURL := t.localServerURL(); serverURL != "" {
			address = serverURL
		} else if phpURL := t.apacheProjectURL(); phpURL != "" {
			address = phpURL
		} else {
			return "", fmt.Errorf("no hay servidor HTTP disponible; ejecuta start_server antes de browser_screenshot")
		}
	}
	if address == "" || strings.HasPrefix(strings.ToLower(address), "file:") {
		if phpURL := t.apacheProjectURL(); phpURL != "" {
			address = phpURL
		} else if strings.HasPrefix(strings.ToLower(address), "file:") {
			return "", fmt.Errorf("PHP debe abrirse mediante Apache/Laragon: no se permite file:// para ejecutar %s", args.Path)
		}
	}
	if address == "" {
		return "", fmt.Errorf("no hay servidor HTTP disponible para el proyecto; inicia Apache/Laragon o usa start_server antes de browser_screenshot")
	}
	if address == "" {
		localPath, resolveErr := t.resolve("index.html")
		if resolveErr != nil {
			return "", fmt.Errorf("no browser URL or path supplied; expected index.html in workspace: %w", resolveErr)
		}
		address = fileURL(localPath)
	}
	if strings.Contains(strings.ToLower(address), "/path/to/") {
		// Models sometimes copy the placeholder from the tool documentation.
		// Recover using its basename only when that file exists in the workspace.
		placeholderName := filepath.Base(strings.TrimSuffix(strings.Split(address, "?")[0], "/"))
		localPath, resolveErr := t.resolve(placeholderName)
		if resolveErr != nil {
			return "", fmt.Errorf("placeholder URL %q is not a real workspace path; use path: %q", address, placeholderName)
		}
		address = fileURL(localPath)
	}
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") && !strings.HasPrefix(address, "file://") {
		return "", fmt.Errorf("provide an http(s) URL or a workspace path")
	}
	browser, err := findBrowser()
	if err != nil {
		return "", err
	}
	outputPath, err := t.resolve(filepath.Join(".gokodek", "screenshots", fmt.Sprintf("browser-%s.png", time.Now().Format("20060102-150405.000"))))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", fmt.Errorf("create screenshot directory: %w", err)
	}
	if t.visible {
		visibleProfile, profileErr := os.MkdirTemp("", "gokodek-browser-visible-")
		if profileErr != nil {
			return "", fmt.Errorf("create visible browser profile: %w", profileErr)
		}
		visibleCmd := exec.Command(browser,
			"--new-window", "--disable-extensions", "--no-first-run", "--no-default-browser-check",
			"--window-size=1440,900", "--user-data-dir="+visibleProfile, address,
		)
		if err := visibleCmd.Start(); err != nil {
			os.RemoveAll(visibleProfile)
			return "", fmt.Errorf("open visible browser: %w", err)
		}
		// Give the visible window time to load while the clean capture below
		// uses its own temporary profile and cannot interfere with it.
		time.Sleep(1500 * time.Millisecond)
	}

	profile, err := os.MkdirTemp("", "gokodek-browser-capture-")
	if err != nil {
		return "", fmt.Errorf("create isolated capture profile: %w", err)
	}
	defer os.RemoveAll(profile)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, browser,
		"--headless=new", "--disable-gpu", "--hide-scrollbars", "--window-size=1440,900",
		"--run-all-compositor-stages-before-draw", "--virtual-time-budget=2000",
		"--disable-extensions", "--no-first-run", "--no-default-browser-check",
		"--user-data-dir="+profile, "--screenshot="+outputPath, address,
	)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("browser screenshot timed out")
	}
	if err != nil {
		return "", fmt.Errorf("browser screenshot failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	if _, err := os.Stat(outputPath); err != nil {
		return "", fmt.Errorf("browser did not create screenshot: %w", err)
	}
	domExcerpt, domTitle := dumpDOM(ctx, browser, profile, address)
	return visionResult(outputPath, address, t.visible, domExcerpt, domTitle, browser)
}

type CaptureScreenTool struct {
	workspaceTool
	allowed bool
}

func NewCaptureScreenTool(workspace string, allowed bool) *CaptureScreenTool {
	return &CaptureScreenTool{workspaceTool: newWorkspaceTool(workspace), allowed: allowed}
}

func (t *CaptureScreenTool) Name() string { return "capture_screen" }
func (t *CaptureScreenTool) Description() string {
	return "Capture the current Windows desktop and send the PNG to the vision model. Requires explicit UI permission."
}
func (t *CaptureScreenTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{})
}
func (t *CaptureScreenTool) Execute(argsJSON string) (string, error) {
	if !t.allowed {
		return "", fmt.Errorf("capture_screen is disabled; restart with -allow-ui=true")
	}
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("capture_screen currently supports Windows only")
	}
	outputPath, err := t.resolve(filepath.Join(".gokodek", "screenshots", fmt.Sprintf("desktop-%s.png", time.Now().Format("20060102-150405.000"))))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", fmt.Errorf("create screenshot directory: %w", err)
	}
	_, err = runPowerShell(`
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bitmap = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size)
$bitmap.Save($env:GOKODEK_SCREEN_PATH, [System.Drawing.Imaging.ImageFormat]::Png)
$graphics.Dispose()
$bitmap.Dispose()
`, map[string]string{"GOKODEK_SCREEN_PATH": outputPath})
	if err != nil {
		return "", fmt.Errorf("capture desktop: %w", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		return "", fmt.Errorf("desktop screenshot was not created: %w", err)
	}
	return visionResult(outputPath, "desktop", false, "", "", "")
}

type MouseClickTool struct{ allowed bool }

func NewMouseClickTool(allowed bool) *MouseClickTool { return &MouseClickTool{allowed: allowed} }

func (t *MouseClickTool) Name() string { return "mouse_click" }
func (t *MouseClickTool) Description() string {
	return "Move the real Windows mouse cursor and click at screen coordinates. Requires -allow-ui=true and affects the active desktop."
}
func (t *MouseClickTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"x":      map[string]interface{}{"type": "integer", "description": "Horizontal screen coordinate."},
		"y":      map[string]interface{}{"type": "integer", "description": "Vertical screen coordinate."},
		"button": map[string]interface{}{"type": "string", "enum": []string{"left", "right", "middle"}, "description": "Mouse button; defaults to left."},
	}, "x", "y")
}
func (t *MouseClickTool) Execute(argsJSON string) (string, error) {
	if !t.allowed {
		return "", fmt.Errorf("mouse_click is disabled; restart with -allow-ui=true")
	}
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("mouse_click currently supports Windows only")
	}
	var args struct {
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Button string `json:"button"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("mouse_click arguments: %w", err)
	}
	if args.X < 0 || args.X > maxScreenWidth || args.Y < 0 || args.Y > maxScreenHeight {
		return "", fmt.Errorf("mouse coordinates out of range")
	}
	if args.Button == "" {
		args.Button = "left"
	}
	flags := map[string]string{"left": "0x0002,0x0004", "right": "0x0008,0x0010", "middle": "0x0020,0x0040"}
	mouseFlags, ok := flags[strings.ToLower(args.Button)]
	if !ok {
		return "", fmt.Errorf("unsupported mouse button %q", args.Button)
	}
	parts := strings.Split(mouseFlags, ",")
	_, err := runPowerShell(`
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class GokodekMouse {
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint flags, uint dx, uint dy, uint data, UIntPtr extra);
}
"@
[GokodekMouse]::SetCursorPos([int]$env:GOKODEK_X, [int]$env:GOKODEK_Y)
[GokodekMouse]::mouse_event([uint32]$env:GOKODEK_DOWN, 0, 0, 0, [UIntPtr]::Zero)
Start-Sleep -Milliseconds 60
[GokodekMouse]::mouse_event([uint32]$env:GOKODEK_UP, 0, 0, 0, [UIntPtr]::Zero)
`, map[string]string{
		"GOKODEK_X": argsInt(args.X), "GOKODEK_Y": argsInt(args.Y),
		"GOKODEK_DOWN": parts[0], "GOKODEK_UP": parts[1],
	})
	if err != nil {
		return "", fmt.Errorf("mouse click: %w", err)
	}
	return fmt.Sprintf("clicked %s at (%d,%d)", args.Button, args.X, args.Y), nil
}

type KeyboardTypeTool struct{ allowed bool }

func NewKeyboardTypeTool(allowed bool) *KeyboardTypeTool { return &KeyboardTypeTool{allowed: allowed} }

func (t *KeyboardTypeTool) Name() string { return "keyboard_type" }
func (t *KeyboardTypeTool) Description() string {
	return "Type text into the currently focused Windows application using SendKeys. Requires -allow-ui=true and affects the active desktop."
}
func (t *KeyboardTypeTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"text": map[string]interface{}{"type": "string", "description": "Text or SendKeys sequence to type into the focused application."},
	}, "text")
}
func (t *KeyboardTypeTool) Execute(argsJSON string) (string, error) {
	if !t.allowed {
		return "", fmt.Errorf("keyboard_type is disabled; restart with -allow-ui=true")
	}
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("keyboard_type currently supports Windows only")
	}
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("keyboard_type arguments: %w", err)
	}
	if args.Text == "" {
		return "", fmt.Errorf("text cannot be empty")
	}
	if len(args.Text) > maxKeyboardText {
		return "", fmt.Errorf("text exceeds %d bytes", maxKeyboardText)
	}
	_, err := runPowerShell(`
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.SendKeys]::SendWait($env:GOKODEK_KEYS)
`, map[string]string{"GOKODEK_KEYS": args.Text})
	if err != nil {
		return "", fmt.Errorf("keyboard type: %w", err)
	}
	return fmt.Sprintf("typed %d characters", len([]rune(args.Text))), nil
}

func (t *BrowserScreenshotTool) localServerURL() string {
	// Only reuse the URL recorded by start_server. Probing arbitrary ports can
	// accidentally open another project or a generic 404 page.
	if state, ok := loadServerState(t.workspace); ok && probeBrowserURL(state.URL) {
		return state.URL
	}
	return ""
}

func probeBrowserURL(address string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
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
	return res.StatusCode >= 200 && res.StatusCode < 500
}

func (t *BrowserScreenshotTool) apacheProjectURL() string {
	name := filepath.Base(t.workspace)
	address := "http://localhost/" + name + "/"
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return ""
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 500 {
		return address
	}
	return ""
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func findBrowser() (string, error) {
	candidates := []string{"chrome", "google-chrome", "chromium", "chromium-browser", "msedge", "microsoft-edge"}
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		programFiles := os.Getenv("PROGRAMFILES")
		programFilesX86 := os.Getenv("PROGRAMFILES(X86)")
		candidates = append([]string{
			filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
		}, candidates...)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no Chrome, Chromium, or Edge executable found")
}

func runPowerShell(script string, environment map[string]string) ([]byte, error) {
	shell := "powershell.exe"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "pwsh.exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.Env = os.Environ()
	for key, value := range environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("PowerShell: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func argsInt(value int) string { return strconv.Itoa(value) }

var (
	titlePattern      = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	browserTagPattern = regexp.MustCompile(`(?is)<[^>]+>`)
)

func dumpDOM(ctx context.Context, browser, profile, address string) (string, string) {
	cmd := exec.CommandContext(ctx, browser,
		"--headless=new", "--disable-gpu", "--disable-extensions", "--no-first-run",
		"--no-default-browser-check", "--user-data-dir="+profile, "--dump-dom", address,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", ""
	}
	dom := string(output)
	lower := strings.ToLower(dom)
	if start := strings.Index(lower, "<html"); start >= 0 {
		dom = dom[start:]
	}
	title := ""
	if match := titlePattern.FindStringSubmatch(dom); len(match) > 1 {
		title = strings.TrimSpace(html.UnescapeString(browserTagPattern.ReplaceAllString(match[1], "")))
	}
	text := browserTagPattern.ReplaceAllString(dom, " ")
	text = html.UnescapeString(text)
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 2000 {
		text = text[:2000] + "..."
	}
	return text, title
}

func visionResult(imagePath, source string, visible bool, domExcerpt, domTitle, browser string) (string, error) {
	result, err := json.Marshal(map[string]interface{}{
		"status":       "screenshot_captured",
		"image_path":   imagePath,
		"source":       source,
		"browser_open": visible,
		"opened_url":   source,
		"dom_title":    domTitle,
		"dom_excerpt":  domExcerpt,
		"browser":      browser,
	})
	if err != nil {
		return "", fmt.Errorf("encode screenshot result: %w", err)
	}
	return string(result), nil
}
