package mcp

import (
	"os"
	"strings"
)

// readEnviron returns the current process environment as "KEY=VALUE" strings.
func readEnviron() []string {
	return os.Environ()
}

// applyEnvVar sets an environment variable value into a KEY=VALUE slice.
func applyEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
