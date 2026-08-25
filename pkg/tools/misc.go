package tools

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type CurrentDateTool struct{}

func (CurrentDateTool) Name() string { return "current_date" }
func (CurrentDateTool) Description() string {
	return "Return the current local date, time, and timezone."
}
func (CurrentDateTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{})
}
func (CurrentDateTool) Execute(argsJSON string) (string, error) {
	return time.Now().Format("2006-01-02 15:04:05 -0700 MST"), nil
}

type PlanTool struct{}

func (PlanTool) Name() string { return "plan" }
func (PlanTool) Description() string {
	return "Turn a development goal into a concise ordered checklist without editing files."
}
func (PlanTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"goal": map[string]interface{}{"type": "string", "description": "Development goal to decompose."},
	}, "goal")
}
func (PlanTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Goal string `json:"goal"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Goal) == "" {
		return "", fmt.Errorf("goal cannot be empty")
	}
	return "1. Inspect the existing project and constraints\n2. Define the smallest implementation change\n3. Implement and preserve existing behavior\n4. Run focused verification\n5. Review the result and report changed files\nGoal: " + args.Goal, nil
}

type WebSearchTool struct{}

func (WebSearchTool) Name() string { return "web_search" }
func (WebSearchTool) Description() string {
	return "Search the public web with DuckDuckGo and return compact result titles, URLs and snippets. Use it once; if results are irrelevant, change strategy instead of retrying."
}
func (WebSearchTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"query": map[string]interface{}{"type": "string", "description": "Search query."},
	}, "query")
}

// ddgResult is one parsed search hit from the DuckDuckGo HTML endpoint.
type ddgResult struct {
	Title   string
	URL     string
	Snippet string
}

func (WebSearchTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", fmt.Errorf("query cannot be empty")
	}
	results, err := webSearch(query)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for i, item := range results {
		if i >= 8 {
			break
		}
		fmt.Fprintf(&out, "%d. %s\n   %s\n", i+1, item.Title, item.URL)
		if item.Snippet != "" {
			fmt.Fprintf(&out, "   %s\n", item.Snippet)
		}
	}
	if out.Len() == 0 {
		return "No useful results for: " + query + "\nDo NOT retry the same search. If you need a library or file, download it directly with fetch_url from a known URL (for example https://cdn.jsdelivr.net or https://unpkg.com), otherwise continue working offline with the workspace files.", nil
	}
	return out.String(), nil
}

var (
	ddgHrefPattern  = regexp.MustCompile(`class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetPat   = regexp.MustCompile(`class="result__snippet"[^>]*>(.*?)</a>`)
	ddgTagPattern   = regexp.MustCompile(`<[^>]+>`)
	ddgURLDecodedRE = regexp.MustCompile(`uddg=([^&]+)`)
)

// webSearch queries the DuckDuckGo HTML endpoint (works for programming
// queries where the Instant Answer API returns nothing) and parses the
// top organic results without external dependencies.
func webSearch(query string) ([]ddgResult, error) {
	form := url.Values{"q": {query}, "kl": {"wt-wt"}}
	request, err := http.NewRequest(http.MethodPost, "https://html.duckduckgo.com/html/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("web search: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) gokodek")
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("web search: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("web search: %w", err)
	}
	htmlText := string(body)

	hrefs := ddgHrefPattern.FindAllStringSubmatch(htmlText, -1)
	snippets := ddgSnippetPat.FindAllStringSubmatch(htmlText, -1)
	results := make([]ddgResult, 0, len(hrefs))
	for i, match := range hrefs {
		if len(match) < 3 {
			continue
		}
		link := decodeDDGLink(match[1])
		title := strings.TrimSpace(ddgTagPattern.ReplaceAllString(match[2], ""))
		snippet := ""
		if i < len(snippets) && len(snippets[i]) >= 2 {
			snippet = strings.TrimSpace(ddgTagPattern.ReplaceAllString(snippets[i][1], ""))
		}
		if link == "" || title == "" {
			continue
		}
		results = append(results, ddgResult{Title: html.UnescapeString(title), URL: link, Snippet: html.UnescapeString(snippet)})
	}
	return results, nil
}

// decodeDDGLink unwraps DuckDuckGo redirect links (//duckduckgo.com/l/?uddg=...)
// into the real target URL.
func decodeDDGLink(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if match := ddgURLDecodedRE.FindStringSubmatch(value); match != nil {
		if decoded, err := url.QueryUnescape(match[1]); err == nil {
			return decoded
		}
	}
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	return value
}

type BuildTool struct{ workspace string }

func NewBuildTool(workspace string) *BuildTool { return &BuildTool{workspace: workspace} }
func (BuildTool) Name() string                 { return "build" }
func (BuildTool) Description() string {
	return "Run the detected project's focused build command in the workspace."
}
func (BuildTool) Parameters() map[string]interface{} { return objectSchema(map[string]interface{}{}) }
func (t BuildTool) Execute(argsJSON string) (string, error) {
	if _, err := os.Stat(filepath.Join(t.workspace, "go.mod")); err == nil {
		return runBuild(t.workspace, "go", "build", "./...")
	}
	if _, err := os.Stat(filepath.Join(t.workspace, "package.json")); err == nil {
		if runtime.GOOS == "windows" {
			return runBuild(t.workspace, "npm.cmd", "run", "build")
		}
		return runBuild(t.workspace, "npm", "run", "build")
	}
	return "No supported build manifest found (go.mod or package.json).", nil
}
func runBuild(workspace, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("build failed: %w", err)
	}
	if len(output) == 0 {
		return "build completed successfully", nil
	}
	return string(output), nil
}
