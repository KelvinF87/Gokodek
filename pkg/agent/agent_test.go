package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type testEchoTool struct{}

func (testEchoTool) Name() string        { return "echo" }
func (testEchoTool) Description() string { return "Echo a value." }
func (testEchoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"value": map[string]interface{}{"type": "string"},
		},
		"required": []string{"value"},
	}
}
func (testEchoTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}
	return "echo:" + args.Value, nil
}

type testScreenshotTool struct{ path string }

func (testScreenshotTool) Name() string        { return "browser_screenshot" }
func (testScreenshotTool) Description() string { return "Capture a test screenshot." }
func (testScreenshotTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
func (t testScreenshotTool) Execute(string) (string, error) {
	result, err := json.Marshal(map[string]string{"image_path": t.path})
	return string(result), err
}

func TestVisionModelReceivesNoToolsAndCodingModelResumes(t *testing.T) {
	imagePath := t.TempDir() + "/shot.png"
	if err := os.WriteFile(imagePath, []byte("test-image"), 0644); err != nil {
		t.Fatal(err)
	}
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch requestCount {
		case 1:
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"browser_screenshot","arguments":{}}}]},"done":false}`))
		case 2:
			if request.Model != "vision-model" || len(request.Tools) != 0 || len(request.Messages[len(request.Messages)-1].Images) != 1 {
				t.Fatalf("vision request was not isolated: model=%s tools=%d messages=%+v", request.Model, len(request.Tools), request.Messages)
			}
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"La página tiene problemas visuales."},"done":true}`))
		default:
			if request.Model != "code-model" || len(request.Tools) == 0 {
				t.Fatalf("coding model did not resume: model=%s tools=%d", request.Model, len(request.Tools))
			}
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Corrección aplicada."},"done":true}`))
		}
	}))
	defer server.Close()

	registry := NewToolRegistry()
	if err := registry.Register(testScreenshotTool{path: imagePath}); err != nil {
		t.Fatal(err)
	}
	agent := NewAgent(NewOllamaClient(server.URL), registry, "code-model", nil)
	agent.VisionModel = "vision-model"
	var history []Message
	answer, err := agent.Run(context.Background(), &history, "analiza la captura")
	if err != nil || answer != "Corrección aplicada." || requestCount != 3 {
		t.Fatalf("answer=%q err=%v requests=%d", answer, err, requestCount)
	}
}

func TestAgentRetriesAFileRequestWhenModelOnlyExplains(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		switch requestCount {
		case 1:
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"No puedo crear archivos desde aquí."},"done":true}`))
		case 2:
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"echo","arguments":{"value":"created"}}}]},"done":false}`))
		default:
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Archivos creados."},"done":true}`))
		}
	}))
	defer server.Close()

	registry := NewToolRegistry()
	if err := registry.Register(testEchoTool{}); err != nil {
		t.Fatal(err)
	}
	agent := NewAgent(NewOllamaClient(server.URL), registry, "test-model", nil)
	var history []Message
	answer, err := agent.Run(context.Background(), &history, "crea una web en html")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Archivos creados." || requestCount != 3 {
		t.Fatalf("answer=%q requests=%d", answer, requestCount)
	}
}

func TestRequiresDiagnostic(t *testing.T) {
	if !requiresDiagnostic("se ve la pantalla negra") {
		t.Fatal("expected visual issue to require diagnostic")
	}
	if requiresDiagnostic("crea una página web") {
		t.Fatal("creation request should not require visual diagnostic")
	}
}

func TestParseTextToolCalls(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(testEchoTool{}); err != nil {
		t.Fatal(err)
	}
	content := "```json\n{\"name\":\"echo\",\"arguments\":{\"value\":\"ok\"}}\n```"
	calls := parseTextToolCalls(content, registry)
	if len(calls) != 1 || calls[0].Function.Name != "echo" || string(calls[0].Function.Arguments) != `{"value":"ok"}` {
		t.Fatalf("unexpected parsed calls: %+v", calls)
	}
}

func TestAgentRunsToolThenReturnsFinalAnswer(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			if len(request.Tools) != 1 || request.Tools[0].Function.Name != "echo" {
				t.Fatalf("unexpected tools: %+v", request.Tools)
			}
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"echo","arguments":{"value":"ok"}}}]},"done":false}`))
			return
		}
		if len(request.Messages) != 4 || request.Messages[3].Role != "tool" || request.Messages[3].ToolName != "echo" || request.Messages[3].Content != "echo:ok" {
			t.Fatalf("unexpected follow-up messages: %+v", request.Messages)
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Trabajo terminado."},"done":true}`))
	}))
	defer server.Close()

	registry := NewToolRegistry()
	if err := registry.Register(testEchoTool{}); err != nil {
		t.Fatal(err)
	}
	var progress bytes.Buffer
	agent := NewAgent(NewOllamaClient(server.URL), registry, "test-model", &progress)
	var history []Message
	answer, err := agent.Run(context.Background(), &history, "hazlo")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Trabajo terminado." {
		t.Fatalf("answer = %q", answer)
	}
	if requestCount != 2 || len(history) != 5 {
		t.Fatalf("requests=%d history=%d", requestCount, len(history))
	}
	if !strings.Contains(progress.String(), "echo") {
		t.Fatalf("progress did not report tool call: %q", progress.String())
	}
}
