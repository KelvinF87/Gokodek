package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// OllamaModel is a model entry returned by `ollama list` or the /api/tags endpoint.
type OllamaModel struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

// DetectOllamaModels lists models installed in the Ollama server. It first tries
// the HTTP /api/tags endpoint (works for remote servers), then falls back to
// running the local `ollama list` command.
func DetectOllamaModels(ctx context.Context, baseURL string) ([]OllamaModel, error) {
	if models, err := detectViaHTTP(ctx, baseURL); err == nil && len(models) > 0 {
		return models, nil
	}
	return detectViaCLI(ctx)
}

func detectViaHTTP(ctx context.Context, baseURL string) ([]OllamaModel, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultOllamaEndpoint
	}
	endpoint := strings.TrimRight(baseURL, "/")
	// Normalize: if the user provided the /api/chat endpoint, strip it.
	endpoint = strings.TrimSuffix(endpoint, "/api/chat")
	endpoint = strings.TrimSuffix(endpoint, "/api")
	endpoint += "/api/tags"

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama tags returned HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Models []OllamaModel `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Models, nil
}

func detectViaCLI(ctx context.Context) ([]OllamaModel, error) {
	cmd := exec.CommandContext(ctx, "ollama", "list")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ollama list: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var models []OllamaModel
	for i, line := range lines {
		if i == 0 {
			continue // header row
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		models = append(models, OllamaModel{Name: fields[0], Model: fields[0]})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("no models found via ollama list")
	}
	return models, nil
}

// isWindows reports the current OS for the CLI helper.
func isWindows() bool {
	return runtime.GOOS == "windows"
}
