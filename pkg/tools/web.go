package tools

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	stylesheetPattern = regexp.MustCompile(`(?is)<link\b[^>]*\bhref\s*=\s*["']([^"']+)["']`)
	scriptPattern     = regexp.MustCompile(`(?is)<script\b[^>]*\bsrc\s*=\s*["']([^"']+)["']`)
	bodyPattern       = regexp.MustCompile(`(?is)<body\b[^>]*>(.*?)</body>`)
	tagPattern        = regexp.MustCompile(`(?is)<[^>]+>`)
	bodyRulePattern   = regexp.MustCompile(`(?is)(?:^|})\s*body\s*\{([^}]*)\}`)
)

type CheckWebTool struct {
	workspaceTool
}

func NewCheckWebTool(workspace string) *CheckWebTool {
	return &CheckWebTool{workspaceTool: newWorkspaceTool(workspace)}
}

func (t *CheckWebTool) Name() string { return "check_web" }
func (t *CheckWebTool) Description() string {
	return "Inspect an HTML file and verify that its local CSS and JavaScript references point to files that exist in the workspace."
}
func (t *CheckWebTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"path": map[string]interface{}{"type": "string", "description": "HTML file path relative to the workspace; defaults to index.html."},
	})
}
func (t *CheckWebTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("check_web arguments: %w", err)
	}
	if strings.TrimSpace(args.Path) == "" {
		args.Path = "index.html"
	}
	htmlPath, err := t.resolve(args.Path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(htmlPath)
	if err != nil {
		return "", fmt.Errorf("read HTML %q: %w", args.Path, err)
	}

	var report strings.Builder
	fmt.Fprintf(&report, "HTML: %s (exists, %d bytes)\n", args.Path, len(content))
	issues := 0
	cssMatches := stylesheetPattern.FindAllStringSubmatch(string(content), -1)
	jsMatches := scriptPattern.FindAllStringSubmatch(string(content), -1)
	cssCount, cssIssues := t.checkReferences(&report, htmlPath, cssMatches, "CSS")
	jsCount, jsIssues := t.checkReferences(&report, htmlPath, jsMatches, "JavaScript")
	issues += cssIssues + jsIssues
	issues += t.inspectVisualBasics(&report, htmlPath, content, cssMatches)
	if cssCount == 0 {
		report.WriteString("CSS: no local stylesheet reference found\n")
		issues++
	}
	if jsCount == 0 {
		report.WriteString("JavaScript: no local script reference found\n")
	}
	for _, match := range cssMatches {
		if len(match) < 2 || strings.Contains(match[1], "://") || strings.HasPrefix(match[1], "data:") {
			continue
		}
		ref := strings.Split(strings.Split(strings.TrimSpace(match[1]), "?")[0], "#")[0]
		cssPath := filepath.Join(filepath.Dir(htmlPath), filepath.FromSlash(ref))
		if cssInfo, statErr := os.Stat(cssPath); statErr == nil && !cssInfo.IsDir() {
			if cssInfo.Size() == 0 {
				fmt.Fprintf(&report, "CSS: %s is empty\n", match[1])
				issues++
			}
		}
	}
	if issues > 0 {
		fmt.Fprintf(&report, "Result: %d issue(s) found", issues)
		return report.String(), fmt.Errorf("web check found %d issue(s)", issues)
	}
	report.WriteString("Result: all local references are valid")
	return report.String(), nil
}

func (t CheckWebTool) inspectVisualBasics(report *strings.Builder, htmlPath string, content []byte, cssMatches [][]string) int {
	issues := 0
	bodyMatches := bodyPattern.FindSubmatch(content)
	if len(bodyMatches) == 0 {
		report.WriteString("Visual: no <body> element found\n")
		issues++
	} else {
		bodyText := strings.TrimSpace(tagPattern.ReplaceAllString(string(bodyMatches[1]), ""))
		bodyText = strings.TrimSpace(strings.ReplaceAll(bodyText, "&nbsp;", ""))
		if bodyText == "" && !strings.Contains(strings.ToLower(string(bodyMatches[1])), "<canvas") {
			report.WriteString("Visual: body has no visible text; inspect generated content\n")
			issues++
		}
	}

	for _, match := range cssMatches {
		if len(match) < 2 || strings.Contains(match[1], "://") || strings.HasPrefix(match[1], "data:") {
			continue
		}
		reference := strings.Split(strings.Split(strings.TrimSpace(match[1]), "?")[0], "#")[0]
		candidate := filepath.Join(filepath.Dir(htmlPath), filepath.FromSlash(reference))
		css, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		for _, rule := range bodyRulePattern.FindAllSubmatch(css, -1) {
			declarations := strings.ToLower(string(rule[1]))
			if strings.Contains(declarations, "display: none") || strings.Contains(declarations, "visibility: hidden") || strings.Contains(declarations, "opacity: 0") {
				report.WriteString("Visual: body is hidden by CSS\n")
				issues++
			}
			if (strings.Contains(declarations, "color: #000") || strings.Contains(declarations, "color: black")) && (strings.Contains(declarations, "background: #000") || strings.Contains(declarations, "background-color: #000") || strings.Contains(declarations, "background: black")) {
				report.WriteString("Visual: body text and background may both be black\n")
				issues++
			}
		}
	}
	return issues
}

func (t CheckWebTool) checkReferences(report *strings.Builder, htmlPath string, matches [][]string, kind string) (int, int) {
	count := 0
	issues := 0
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		reference := strings.TrimSpace(match[1])
		if reference == "" || strings.HasPrefix(reference, "#") || strings.HasPrefix(reference, "data:") || strings.Contains(reference, "://") {
			continue
		}
		count++
		pathValue := strings.Split(reference, "?")[0]
		pathValue = strings.Split(pathValue, "#")[0]
		pathValue, err := url.PathUnescape(pathValue)
		if err != nil {
			fmt.Fprintf(report, "%s: %s (invalid URL encoding)\n", kind, reference)
			issues++
			continue
		}
		candidate := filepath.Join(filepath.Dir(htmlPath), filepath.FromSlash(pathValue))
		relative, err := filepath.Rel(t.workspace, candidate)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			fmt.Fprintf(report, "%s: %s (outside workspace)\n", kind, reference)
			issues++
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			fmt.Fprintf(report, "%s: %s (missing)\n", kind, reference)
			issues++
			continue
		}
		fmt.Fprintf(report, "%s: %s (ok, %d bytes)\n", kind, reference, info.Size())
	}
	return count, issues
}
