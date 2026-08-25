package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// runGitCheckpoint creates a recoverable commit before a risky change. It
// initializes Git when the workspace is not a repository and excludes local
// secrets and gokodek metadata from the checkpoint.
func runGitCheckpoint(workspace, reason string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git no está instalado; no se puede proteger la operación crítica")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	initialized := false
	if err := runGit(ctx, workspace, "rev-parse", "--is-inside-work-tree"); err != nil {
		if _, initErr := runGitOutput(ctx, workspace, "init"); initErr != nil {
			return "", fmt.Errorf("no se pudo inicializar git: %w", initErr)
		}
		initialized = true
	}

	status, err := runGitOutput(ctx, workspace, "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("consultar estado git: %w", err)
	}
	if strings.TrimSpace(status) == "" {
		return "checkpoint listo: el repositorio ya estaba limpio", nil
	}

	// Keep secrets and gokodek's local state out of automatic commits.
	if _, err := runGitOutput(ctx, workspace, "add", "-A", "--", ".", ":!.gokodek", ":!.env", ":!*.env", ":!*.key", ":!credentials*", ":!config.local.json"); err != nil {
		return "", fmt.Errorf("preparar checkpoint: %w", err)
	}
	message := fmt.Sprintf("gokodek checkpoint: %s", strings.TrimSpace(reason))
	if _, err := runGitOutput(ctx, workspace, "commit", "-m", message); err != nil {
		return "", fmt.Errorf("crear checkpoint git: %w", err)
	}
	prefix := "checkpoint creado"
	if initialized {
		prefix = "repositorio git inicializado y checkpoint creado"
	}
	return fmt.Sprintf("%s antes de la operación crítica: %s", prefix, message), nil
}

func runGit(ctx context.Context, workspace string, args ...string) error {
	_, err := runGitOutput(ctx, workspace, args...)
	return err
}

func runGitOutput(ctx context.Context, workspace string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output)), err
	}
	return strings.TrimSpace(string(output)), nil
}
