package agent

import (
	"context"
	"testing"
)

func TestDetectOllamaModelsFallsBackGracefully(t *testing.T) {
	// With an unreachable server and no ollama binary this should return an
	// error, not panic. It exercises the fallback path.
	ctx := context.Background()
	_, err := DetectOllamaModels(ctx, "http://127.0.0.1:1")
	if err == nil {
		// If a real ollama server is running on the machine, detection succeeds.
		t.Log("ollama detection succeeded (server reachable)")
		return
	}
	t.Logf("detection returned expected error: %v", err)
}
