package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type BrowserBridge struct {
	port          int
	server        *http.Server
	commands      chan string
	responseChan  chan string
	consoleLogs   []string
	mu            sync.Mutex
	registeredTab string
	browserCmd    *exec.Cmd
	profileDir    string
}

var (
	globalBridge *BrowserBridge
	bridgeOnce   sync.Once
)

func startGlobalBridge() (*BrowserBridge, error) {
	var startErr error
	bridgeOnce.Do(func() {
		// Find a free port starting at 31337
		port := 31337
		for p := 31337; p <= 31360; p++ {
			l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
			if err == nil {
				_ = l.Close()
				port = p
				break
			}
		}

		globalBridge = &BrowserBridge{
			port:         port,
			commands:     make(chan string, 10),
			responseChan: make(chan string, 10),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/extension/register", globalBridge.handleRegister)
		mux.HandleFunc("/extension/poll", globalBridge.handlePoll)
		mux.HandleFunc("/extension/respond", globalBridge.handleRespond)
		mux.HandleFunc("/extension/log", globalBridge.handleLog)

		server := &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%d", port),
			Handler: corsMiddleware(mux),
		}
		globalBridge.server = server

		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("BrowserBridge error: %v", err)
			}
		}()
	})
	return globalBridge, startErr
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (b *BrowserBridge) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TabId string `json:"tabId"`
		URL   string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
		b.mu.Lock()
		b.registeredTab = req.TabId
		b.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"registered"}`))
}

func (b *BrowserBridge) handlePoll(w http.ResponseWriter, r *http.Request) {
	select {
	case cmd := <-b.commands:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(cmd))
	case <-time.After(3 * time.Second):
		w.WriteHeader(http.StatusNoContent)
	}
}

func (b *BrowserBridge) handleRespond(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err == nil {
		b.responseChan <- string(body)
	}
	w.WriteHeader(http.StatusOK)
}

func (b *BrowserBridge) handleLog(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
		b.mu.Lock()
		b.consoleLogs = append(b.consoleLogs, fmt.Sprintf("[%s] %s", payload.Type, payload.Message))
		if len(b.consoleLogs) > 100 {
			b.consoleLogs = b.consoleLogs[1:]
		}
		b.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

type commandEnvelope struct {
	Id       string `json:"id"`
	Action   string `json:"action"`
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
	Script   string `json:"script,omitempty"`
}

func (b *BrowserBridge) SendCommand(ctx context.Context, action, url, selector, text, script string) (string, error) {
	b.mu.Lock()
	id := fmt.Sprintf("cmd-%d", time.Now().UnixNano())
	env := commandEnvelope{
		Id:       id,
		Action:   action,
		URL:      url,
		Selector: selector,
		Text:     text,
		Script:   script,
	}
	payload, _ := json.Marshal(env)
	b.mu.Unlock()

	// Clear channels
	select {
	case <-b.commands:
	default:
	}

	// Queue command
	b.commands <- string(payload)

	// Wait for response
	timeout := 15 * time.Second
	if action == "wait" {
		timeout = 25 * time.Second
	} else if action == "eval" {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case resp := <-b.responseChan:
			var res struct {
				Id     string          `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			if err := json.Unmarshal([]byte(resp), &res); err == nil && res.Id == id {
				return string(res.Result), nil
			}
		case <-timer.C:
			return "", fmt.Errorf("timeout waiting for browser extension response")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

type BrowserControlTool struct {
	workspaceTool
}

func NewBrowserControlTool(workspace string) *BrowserControlTool {
	return &BrowserControlTool{workspaceTool: newWorkspaceTool(workspace)}
}

func (t *BrowserControlTool) Name() string { return "browser_control" }
func (t *BrowserControlTool) Description() string {
	return "Control Chrome or Edge via custom extension. Allows navigating, clicking, typing, waiting for selectors, executing JS on target context, and taking screenshots."
}

func (t *BrowserControlTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"action":   map[string]interface{}{"type": "string", "enum": []string{"open", "click", "type", "wait", "eval", "screenshot", "state", "close"}},
		"url":      map[string]interface{}{"type": "string", "description": "URL to open/navigate."},
		"selector": map[string]interface{}{"type": "string", "description": "CSS selector to interact with."},
		"text":     map[string]interface{}{"type": "string", "description": "Text to type into input selector."},
		"script":   map[string]interface{}{"type": "string", "description": "Javascript to evaluate in page context."},
		"visible":  map[string]interface{}{"type": "boolean", "description": "Whether to make the browser visible. Defaults to true."},
	}, "action")
}

func (t *BrowserControlTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Action   string `json:"action"`
		URL      string `json:"url"`
		Selector string `json:"selector"`
		Text     string `json:"text"`
		Script   string `json:"script"`
		Visible  *bool  `json:"visible"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("browser_control arguments: %w", err)
	}

	visible := true
	if args.Visible != nil {
		visible = *args.Visible
	}

	b, err := startGlobalBridge()
	if err != nil {
		return "", err
	}

	switch args.Action {
	case "open":
		return t.openBrowser(b, args.URL, visible)
	case "close":
		return t.closeBrowser(b)
	case "screenshot":
		return t.captureScreenshot(b)
	case "click", "type", "wait", "eval", "state":
		b.mu.Lock()
		active := b.browserCmd != nil && b.browserCmd.Process != nil
		b.mu.Unlock()

		if !active {
			return "", fmt.Errorf("no browser is currently running. Use open first.")
		}

		res, err := b.SendCommand(context.Background(), args.Action, args.URL, args.Selector, args.Text, args.Script)
		if err != nil {
			return "", err
		}
		return res, nil
	default:
		return "", fmt.Errorf("unsupported action: %s", args.Action)
	}
}

func (t *BrowserControlTool) openBrowser(b *BrowserBridge, address string, visible bool) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.browserCmd != nil && b.browserCmd.Process != nil {
		b.mu.Unlock()
		res, err := b.SendCommand(context.Background(), "open", address, "", "", "")
		b.mu.Lock()
		if err == nil {
			return res, nil
		}
		_ = b.browserCmd.Process.Kill()
		b.browserCmd = nil
		if b.profileDir != "" {
			os.RemoveAll(b.profileDir)
		}
	}

	browser, err := findBrowser()
	if err != nil {
		return "", err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	extDir := filepath.Join(homeDir, ".gokodek", "extension")
	if err := writeExtensionFiles(extDir, b.port); err != nil {
		return "", err
	}

	profileDir, err := os.MkdirTemp("", "gokodek-browser-control-")
	if err != nil {
		return "", err
	}
	b.profileDir = profileDir
	b.registeredTab = ""

	var args []string
	if visible {
		args = []string{
			"--new-window", "--no-first-run", "--no-default-browser-check",
			"--window-size=1440,900", "--user-data-dir=" + profileDir,
			"--load-extension=" + extDir, address,
		}
	} else {
		args = []string{
			"--headless=new", "--disable-gpu", "--hide-scrollbars", "--window-size=1440,900",
			"--no-first-run", "--no-default-browser-check", "--user-data-dir=" + profileDir,
			"--load-extension=" + extDir, address,
		}
	}

	cmd := exec.Command(browser, args...)
	if err := cmd.Start(); err != nil {
		os.RemoveAll(profileDir)
		return "", fmt.Errorf("launching browser: %w", err)
	}

	b.browserCmd = cmd
	b.consoleLogs = nil

	// Wait for connection
	b.mu.Unlock()
	connected := false
	for i := 0; i < 40; i++ {
		b.mu.Lock()
		if b.registeredTab != "" {
			connected = true
			b.mu.Unlock()
			break
		}
		b.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}
	b.mu.Lock()

	if !connected {
		_ = cmd.Process.Kill()
		b.browserCmd = nil
		os.RemoveAll(profileDir)
		return "", fmt.Errorf("browser extension connection timed out. Verify extension loads correctly.")
	}

	return fmt.Sprintf(`{"status":"opened","url":%q,"visible":%t,"port":%d}`, address, visible, b.port), nil
}

func (t *BrowserControlTool) closeBrowser(b *BrowserBridge) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.browserCmd == nil {
		return `{"status":"already_closed"}`, nil
	}

	if b.browserCmd.Process != nil {
		_ = b.browserCmd.Process.Kill()
		_, _ = b.browserCmd.Process.Wait()
	}
	b.browserCmd = nil

	if b.profileDir != "" {
		_ = os.RemoveAll(b.profileDir)
		b.profileDir = ""
	}
	b.registeredTab = ""

	return `{"status":"closed"}`, nil
}

func (t *BrowserControlTool) captureScreenshot(b *BrowserBridge) (string, error) {
	resStr, err := b.SendCommand(context.Background(), "screenshot", "", "", "", "")
	if err != nil {
		return "", err
	}

	var res struct {
		Success    bool   `json:"success"`
		Screenshot string `json:"screenshot"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal([]byte(resStr), &res); err != nil {
		return "", fmt.Errorf("invalid response payload: %w", err)
	}

	if !res.Success {
		return "", fmt.Errorf("failed capturing tab screenshot: %s", res.Error)
	}

	// The screenshot is returned as a Data URL: data:image/png;base64,...
	const prefix = "data:image/png;base64,"
	if len(res.Screenshot) < len(prefix) || res.Screenshot[:len(prefix)] != prefix {
		return "", fmt.Errorf("invalid screenshot format returned")
	}

	rawB64 := res.Screenshot[len(prefix):]
	data, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		return "", fmt.Errorf("failed decoding screenshot base64: %w", err)
	}

	outputPath, err := t.resolve(filepath.Join(".gokodek", "screenshots", fmt.Sprintf("test-%s.png", time.Now().Format("20060102-150405.000"))))
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", fmt.Errorf("create screenshot output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return "", fmt.Errorf("saving screenshot file: %w", err)
	}

	b.mu.Lock()
	logs := make([]string, len(b.consoleLogs))
	copy(logs, b.consoleLogs)
	b.mu.Unlock()

	return visionResult(outputPath, "browser_control_screenshot", true, "", "", strings.Join(logs, " | "), "chrome-extension")
}

func writeExtensionFiles(dir string, port int) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	manifest := `{
  "manifest_version": 3,
  "name": "Gokodek Browser Testing Agent Extension",
  "version": "1.0",
  "permissions": ["activeTab", "scripting", "tabs", "webNavigation"],
  "host_permissions": ["<all_urls>"],
  "background": {
    "service_worker": "background.js"
  },
  "content_scripts": [
    {
      "matches": ["<all_urls>"],
      "js": ["content.js"],
      "run_at": "document_start"
    }
  ]
}`

	bg := fmt.Sprintf(`const BRIDGE_URL = "http://localhost:%d";
let activeTabId = null;
let currentUrl = "";

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (changeInfo.status === 'complete' && tab.url && tab.url.startsWith('http')) {
    activeTabId = tabId;
    currentUrl = tab.url;
    registerTab(tabId, tab.url);
  }
});

chrome.tabs.onActivated.addListener((activeInfo) => {
  chrome.tabs.get(activeInfo.tabId, (tab) => {
    if (tab && tab.url && tab.url.startsWith('http')) {
      activeTabId = tab.id;
      currentUrl = tab.url;
      registerTab(tab.id, tab.url);
    }
  });
});

async function getActiveTabId() {
  if (activeTabId) return activeTabId;
  let [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (tab) return tab.id;
  let [anyTab] = await chrome.tabs.query({ active: true });
  if (anyTab) return anyTab.id;
  return null;
}

function registerTab(tabId, url) {
  fetch(BRIDGE_URL + "/extension/register", {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ tabId: tabId.toString(), url: url })
  }).catch(err => console.log("Bridge registration error:", err));
}

async function pollCommands() {
  while (true) {
    try {
      let tId = await getActiveTabId();
      if (!tId) {
        await new Promise(r => setTimeout(r, 1000));
        continue;
      }
      let resp = await fetch(BRIDGE_URL + "/extension/poll?tabId=" + tId);
      if (resp.status === 200) {
        let cmd = await resp.json();
        if (cmd && cmd.action) {
          executeCommand(tId, cmd);
        }
      }
    } catch (e) {
      await new Promise(r => setTimeout(r, 1000));
    }
    await new Promise(r => setTimeout(r, 100));
  }
}

pollCommands();

async function executeCommand(tId, cmd) {
  try {
    let result = null;
    if (cmd.action === "open") {
      await chrome.tabs.update(tId, { url: cmd.url });
      result = { success: true, message: "Navigated to " + cmd.url };
    } else if (cmd.action === "screenshot") {
      let dataUrl = await chrome.tabs.captureVisibleTab(null, { format: "png" });
      result = { success: true, screenshot: dataUrl };
    } else {
      result = await new Promise((resolve) => {
        chrome.tabs.sendMessage(tId, cmd, (response) => {
          if (chrome.runtime.lastError) {
            resolve({ success: false, error: chrome.runtime.lastError.message });
          } else {
            resolve(response);
          }
        });
      });
    }
    respondToBridge(cmd.id, result);
  } catch (err) {
    respondToBridge(cmd.id, { success: false, error: err.toString() });
  }
}

function respondToBridge(cmdId, result) {
  fetch(BRIDGE_URL + "/extension/respond", {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: cmdId, result: result })
  }).catch(err => console.log("Bridge respond error:", err));
}
`, port)

	content := fmt.Sprintf(`window.alert = function(msg) {
  console.log("ALERT ESCAPED:", msg);
  sendBridgeLog("ALERT", msg);
  return true;
};
window.confirm = function(msg) {
  console.log("CONFIRM ESCAPED:", msg);
  sendBridgeLog("CONFIRM", msg);
  return true;
};
window.prompt = function(msg, defaultVal) {
  console.log("PROMPT ESCAPED:", msg);
  sendBridgeLog("PROMPT", msg);
  return defaultVal || "";
};

const originalConsoleError = console.error;
console.error = function(...args) {
  originalConsoleError.apply(console, args);
  sendBridgeLog("CONSOLE_ERROR", args.map(a => typeof a === 'object' ? JSON.stringify(a) : a).join(' '));
};

window.addEventListener('error', function(e) {
  sendBridgeLog("UNCAUGHT_ERROR", e.message + " at " + e.filename + ":" + e.lineno);
});

function sendBridgeLog(type, message) {
  fetch("http://localhost:%d/extension/log", {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ type: type, message: message })
  }).catch(() => {});
}

chrome.runtime.onMessage.addListener((cmd, sender, sendResponse) => {
  executeDomCommand(cmd).then(sendResponse);
  return true;
});

async function executeDomCommand(cmd) {
  try {
    if (cmd.action === "click") {
      let el = document.querySelector(cmd.selector);
      if (!el) return { success: false, error: "Element not found: " + cmd.selector };
      el.scrollIntoView({ block: "center" });
      el.focus();
      let clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true, view: window });
      el.dispatchEvent(clickEvent);
      return { success: true, message: "Clicked element: " + cmd.selector };
    } else if (cmd.action === "type") {
      let el = document.querySelector(cmd.selector);
      if (!el) return { success: false, error: "Element not found: " + cmd.selector };
      el.scrollIntoView({ block: "center" });
      el.focus();
      el.value = cmd.text;
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      return { success: true, message: "Typed into element: " + cmd.selector };
    } else if (cmd.action === "wait") {
      let start = Date.now();
      while (Date.now() - start < 10000) {
        if (document.querySelector(cmd.selector)) {
          return { success: true, message: "Selector found: " + cmd.selector };
        }
        await new Promise(r => setTimeout(r, 200));
      }
      return { success: false, error: "Timeout waiting for selector: " + cmd.selector };
    } else if (cmd.action === "eval") {
      let result = await evalScriptInPage(cmd.script);
      return { success: true, result: result };
    } else if (cmd.action === "state") {
      return {
        success: true,
        title: document.title,
        url: window.location.href,
        excerpt: document.body.innerText.substring(0, 1000)
      };
    }
  } catch (err) {
    return { success: false, error: err.toString() };
  }
}

function evalScriptInPage(code) {
  return new Promise((resolve) => {
    const scriptId = 'gokodek-eval-script';
    let oldScript = document.getElementById(scriptId);
    if (oldScript) oldScript.remove();
    const eventName = 'gokodek-eval-result-' + Math.random().toString(36).substr(2, 9);
    window.addEventListener(eventName, function handler(e) {
      window.removeEventListener(eventName, handler);
      resolve(e.detail);
    });
    const script = document.createElement('script');
    script.id = scriptId;
    script.textContent = "try { let result = (() => { " + code + " })(); if (result instanceof Promise) { result.then(r => window.dispatchEvent(new CustomEvent('" + eventName + "', { detail: { success: true, value: r } }))).catch(err => window.dispatchEvent(new CustomEvent('" + eventName + "', { detail: { success: false, error: err.toString() } }))); } else { window.dispatchEvent(new CustomEvent('" + eventName + "', { detail: { success: true, value: result } })); } } catch (err) { window.dispatchEvent(new CustomEvent('" + eventName + "', { detail: { success: false, error: err.toString() } })); }";
    (document.head || document.documentElement).appendChild(script);
  });
}
`, port)

	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "background.js"), []byte(bg), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "content.js"), []byte(content), 0644); err != nil {
		return err
	}
	return nil
}
