package tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchURLToolDownloadsIntoWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lib/chess.min.js" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte("var Chess=function(){};\n"))
	}))
	defer server.Close()

	workspace := t.TempDir()
	tool := NewFetchURLTool(workspace)

	result, err := tool.Execute(`{"url":"` + server.URL + `/lib/chess.min.js"}`)
	if err != nil {
		t.Fatalf("fetch_url failed: %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(workspace, "chess.min.js"))
	if readErr != nil {
		t.Fatalf("downloaded file missing: %v", readErr)
	}
	if !strings.Contains(string(data), "var Chess") {
		t.Fatalf("unexpected content: %q", string(data))
	}
	if !strings.Contains(result, "chess.min.js") || !strings.Contains(result, "saved") {
		t.Fatalf("result should mention saved path, got: %q", result)
	}
}

func TestFetchURLToolCustomPathAndRejectsNonHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.txt" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()

	workspace := t.TempDir()
	tool := NewFetchURLTool(workspace)

	if _, err := tool.Execute(`{"url":"` + server.URL + `/x.bin","path":"vendor/libs/x.bin"}`); err != nil {
		t.Fatalf("custom path fetch failed: %v", err)
	}
	if info, err := os.Stat(filepath.Join(workspace, "vendor", "libs", "x.bin")); err != nil || info.IsDir() {
		t.Fatalf("expected vendor/libs/x.bin in workspace, err=%v", err)
	}

	if _, err := tool.Execute(`{"url":"ftp://example.com/file.txt"}`); err == nil {
		t.Fatal("expected error for non-http scheme")
	}
	if _, err := tool.Execute(`{"url":""}`); err == nil {
		t.Fatal("expected error for empty url")
	}
	if _, err := tool.Execute(`{"url":"` + server.URL + `/missing.txt"}`); err == nil {
		t.Fatal("expected error for 404 response")
	}
	if _, err := tool.Execute(`{"url":"` + server.URL + `/../../etc"}`); err != nil {
		// Path traversal inside the URL is allowed by the HTTP fetch itself;
		// what must be blocked is escaping the workspace on save.
		_ = err
	}
}

func TestFetchURLToolRejectsWorkspaceEscape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("evil"))
	}))
	defer server.Close()

	workspace := t.TempDir()
	outside := filepath.Join(filepath.Dir(workspace), filepath.Base(workspace)+"-outside.txt")
	t.Cleanup(func() { os.Remove(outside) })

	tool := NewFetchURLTool(workspace)
	if _, err := tool.Execute(`{"url":"` + server.URL + `/e.txt","path":"../escaped.txt"}`); err == nil {
		t.Fatal("expected error when destination escapes the workspace")
	}
}

func TestDecodeDDGLink(t *testing.T) {
	cases := map[string]string{
		`//duckduckgo.com/l/?uddg=https%3A%2F%2Fcdn.jsdelivr.net%2Fnpm%2Fchess.js&rut=abc`: "https://cdn.jsdelivr.net/npm/chess.js",
		`//example.com/page`:         "https://example.com/page",
		`https://direct.example.com`: "https://direct.example.com",
	}
	for input, want := range cases {
		if got := decodeDDGLink(input); got != want {
			t.Errorf("decodeDDGLink(%q) = %q, want %q", input, got, want)
		}
	}
}
