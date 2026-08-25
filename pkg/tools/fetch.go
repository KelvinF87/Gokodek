package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxFetchBytes   = 20 << 20
	fetchTimeout    = 30 * time.Second
	defaultFileName = "fetched_file"
)

// FetchURLTool downloads a remote file into the workspace so the agent can
// vendor libraries locally instead of looping on web searches.
type FetchURLTool struct {
	workspaceTool
	AllowedDomains      []string
	AllowedURLs         []string
	SourceDescriptions  map[string]string
	RequireConfirmation bool
}

func NewFetchURLTool(workspace string) *FetchURLTool {
	return &FetchURLTool{workspaceTool: newWorkspaceTool(workspace)}
}

func (t *FetchURLTool) Configure(domains, urls []string, requireConfirmation bool) {
	t.AllowedDomains = append([]string(nil), domains...)
	t.AllowedURLs = append([]string(nil), urls...)
	t.RequireConfirmation = requireConfirmation
}

// ConfigureSources adds human-readable purpose information to the tool
// description so small models can select an appropriate official source.
func (t *FetchURLTool) ConfigureSources(descriptions map[string]string) {
	t.SourceDescriptions = make(map[string]string, len(descriptions))
	for domain, description := range descriptions {
		t.SourceDescriptions[strings.ToLower(strings.TrimSpace(domain))] = strings.TrimSpace(description)
	}
}

func (t *FetchURLTool) Name() string { return "fetch_url" }
func (t *FetchURLTool) Description() string {
	var builder strings.Builder
	builder.WriteString("Download a file from an allowed absolute http(s) URL into the workspace. Arguments are distinct: url is the remote source, path is the local destination relative to the workspace. Example: url=https://cdn.jsdelivr.net/npm/three@0.152.2/build/three.min.js, path=libs/three.min.js. Never pass a local path as url. If the URL is rejected, do not retry it: use a configured source, ask for explicit authorization, or implement without the dependency.")
	if len(t.SourceDescriptions) > 0 {
		builder.WriteString(" Configured sources: ")
		keys := make([]string, 0, len(t.SourceDescriptions))
		for domain := range t.SourceDescriptions {
			keys = append(keys, domain)
		}
		sort.Strings(keys)
		for index, domain := range keys {
			if index > 0 {
				builder.WriteString("; ")
			}
			fmt.Fprintf(&builder, "%s: %s", domain, t.SourceDescriptions[domain])
		}
	}
	return builder.String()
}
func (t *FetchURLTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"url":  map[string]interface{}{"type": "string", "description": "Remote source only: absolute http(s) URL, for example https://cdn.jsdelivr.net/npm/three@0.152.2/build/three.min.js. Do not put libs/three.min.js here."},
		"path": map[string]interface{}{"type": "string", "description": "Local destination only, relative to the workspace, for example libs/three.min.js. Defaults to the remote file name."},
	}, "url")
}

func (t *FetchURLTool) Execute(argsJSON string) (string, error) {
	var args struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("fetch_url arguments: %w", err)
	}
	rawURL := strings.TrimSpace(args.URL)
	if rawURL == "" {
		return "", fmt.Errorf("url cannot be empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid URL %q", rawURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("only http and https URLs are supported")
	}
	if len(t.AllowedDomains) > 0 || len(t.AllowedURLs) > 0 {
		if !urlAllowed(parsed, t.AllowedDomains, t.AllowedURLs) {
			return "", fmt.Errorf("URL no permitida para scraping: %s", parsed.Host)
		}
	}

	client := &http.Client{Timeout: fetchTimeout}
	res, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("fetch %q: %w", rawURL, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("fetch %q: unexpected status %s", rawURL, res.Status)
	}

	destination := strings.TrimSpace(args.Path)
	if destination == "" {
		destination = fileNameFromURL(parsed)
	}
	target, err := t.resolve(destination)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create destination directory: %w", err)
	}

	file, err := os.Create(target)
	if err != nil {
		return "", fmt.Errorf("create %q: %w", destination, err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(res.Body, maxFetchBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(target)
		return "", fmt.Errorf("download %q: %w", rawURL, copyErr)
	}
	if closeErr != nil {
		os.Remove(target)
		return "", fmt.Errorf("save %q: %w", destination, closeErr)
	}
	if written > maxFetchBytes {
		os.Remove(target)
		return "", fmt.Errorf("file from %q exceeds %d bytes limit", rawURL, maxFetchBytes)
	}
	return fmt.Sprintf("saved %s (%d bytes) from %s. Reference it with a local script/link tag.", destination, written, rawURL), nil
}

func urlAllowed(parsed *url.URL, domains, urls []string) bool {
	for _, allowed := range urls {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(allowed), "/"), strings.TrimRight(parsed.String(), "/")) {
			return true
		}
	}
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		host := strings.ToLower(parsed.Hostname())
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// fileNameFromURL derives a safe workspace-relative file name from a parsed URL.
func fileNameFromURL(parsed *url.URL) string {
	name := filepath.Base(strings.TrimSuffix(parsed.Path, "/"))
	name = strings.Split(name, "?")[0]
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" || name == `\` {
		return defaultFileName
	}
	return name
}
