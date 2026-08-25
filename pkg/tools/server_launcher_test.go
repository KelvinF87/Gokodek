package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteProjectLauncherContainsApplicationCommand(t *testing.T) {
	workspace := t.TempDir()
	plan := serverPlan{
		Runtime: "php",
		Command: "php",
		Args:    []string{"-S", "127.0.0.1:PORT", "-t", workspace},
	}
	launcher, logPath, err := writeProjectLauncher(workspace, plan, 4173)
	if err != nil {
		t.Fatalf("writeProjectLauncher failed: %v", err)
	}
	content, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatalf("launcher missing: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "php") || !strings.Contains(text, "4173") || !strings.Contains(text, workspace) {
		t.Fatalf("launcher does not contain the real application command: %s", text)
	}
	if !strings.Contains(text, filepath.Base(logPath)) {
		t.Fatalf("launcher does not redirect output to the project log: %s", text)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(launcher), ".bat") {
		t.Fatalf("expected a .bat launcher on Windows, got %s", launcher)
	}
}

func TestWriteProjectLauncherDoesNotRewriteUnchangedLauncher(t *testing.T) {
	workspace := t.TempDir()
	plan := serverPlan{Runtime: "static-python", Command: "python", Args: []string{"-m", "http.server", "PORT"}}
	launcher, _, err := writeProjectLauncher(workspace, plan, 4173)
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	firstContent, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := writeProjectLauncher(workspace, plan, 4173); err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	secondContent, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstContent) != string(secondContent) {
		t.Fatal("unchanged launcher content was modified")
	}
	if first.ModTime() != second.ModTime() {
		t.Fatal("unchanged launcher should not be rewritten")
	}
}

func TestServerStateRoundTrip(t *testing.T) {
	workspace := t.TempDir()
	want := serverState{Runtime: "static-python", URL: "http://127.0.0.1:4173/", Launcher: "launcher", Log: "server.log", PID: 123, Started: "now"}
	if err := saveServerState(workspace, want); err != nil {
		t.Fatal(err)
	}
	got, ok := loadServerState(workspace)
	if !ok {
		t.Fatal("expected persisted server state")
	}
	if got.URL != want.URL || got.Runtime != want.Runtime || got.Launcher != want.Launcher {
		t.Fatalf("unexpected server state: %#v", got)
	}
}
