package agent

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func SecretsPath() string { return filepath.Join(filepath.Dir(ConfigPath()), ".env") }

func normalizeSecretKey(key string, value string) string {
	key = strings.ToUpper(strings.TrimSpace(key))
	// Gemini keys commonly begin with AIza or AQ.; if a Gemini-looking value
	// was accidentally saved under OPENAI_API_KEY, repair the mapping safely.
	if key == "OPENAI_API_KEY" && (strings.HasPrefix(value, "AQ.") || strings.HasPrefix(value, "AIza")) {
		return "GEMINI_API_KEY"
	}
	return key
}

func LoadSecrets() (map[string]string, error) {
	values := map[string]string{}
	paths := []string{SecretsPath(), filepath.Join(filepath.Dir(ConfigPath()), "credentials.env")}
	var file *os.File
	var err error
	for _, path := range paths {
		file, err = os.Open(path)
		if err == nil {
			break
		}
	}
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, fmt.Errorf("open local secrets: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if key != "" {
			values[normalizeSecretKey(key, value)] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read local secrets: %w", err)
	}
	return values, nil
}

func Secret(values map[string]string, key string) string {
	if value := strings.TrimSpace(values[key]); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(key))
}

func SaveSecrets(values map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(SecretsPath()), 0700); err != nil {
		return err
	}
	keys := []string{"OPENAI_API_KEY", "OPENROUTER_API_KEY", "GEMINI_API_KEY", "OLLAMA_URL"}
	var builder strings.Builder
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			fmt.Fprintf(&builder, "%s=%s\n", key, value)
		}
	}
	return os.WriteFile(SecretsPath(), []byte(builder.String()), 0600)
}
