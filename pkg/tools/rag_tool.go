package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gokodek/pkg/rag"
)

const ragIndexRel = ".gokodek/rag_index.json"

// RAGIndexTool indexes the workspace files into a local vector store.
type RAGIndexTool struct {
	workspace string
	embedder  *rag.Embedder
}

func NewRAGIndexTool(workspace, ollamaEndpoint, embeddingModel string) *RAGIndexTool {
	return &RAGIndexTool{workspace: workspace, embedder: rag.NewEmbedder(normalizeRAGEndpoint(ollamaEndpoint), embeddingModel)}
}

func (t *RAGIndexTool) Name() string { return "rag_index" }
func (t *RAGIndexTool) Description() string {
	return "Incrementally index project files into a local vector store for later retrieval. Unchanged files are skipped and old chunks are replaced."
}
func (t *RAGIndexTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"glob": map[string]interface{}{"type": "string", "description": "Optional file pattern to limit indexing (e.g. *.go). Leave empty to index all text files."},
	})
}

func (t *RAGIndexTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Glob string `json:"glob"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("rag_index arguments: %w", err)
	}
	index := rag.NewIndex(filepath.Join(t.workspace, ragIndexRel), t.embedder)
	_ = index.Load()

	files, err := collectTextFiles(t.workspace, args.Glob)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "No se encontraron archivos de texto para indexar.", nil
	}
	ctx := context.Background()
	total := 0
	for _, file := range files {
		n, err := index.AddFile(ctx, file, 800)
		if err != nil {
			return fmt.Sprintf("indexado hasta %s: %v", file, err), nil
		}
		total += n
	}
	if err := index.Save(); err != nil {
		return "", fmt.Errorf("guardar índice: %w", err)
	}
	return fmt.Sprintf("Índice incremental actualizado: %d fragmentos nuevos de %d archivos. Los archivos sin cambios fueron omitidos.\n%s", total, len(files), index.Stats()), nil
}

// RAGSearchTool retrieves relevant project content by semantic similarity.
type RAGSearchTool struct {
	workspace string
	embedder  *rag.Embedder
}

func NewRAGSearchTool(workspace, ollamaEndpoint, embeddingModel string) *RAGSearchTool {
	return &RAGSearchTool{workspace: workspace, embedder: rag.NewEmbedder(normalizeRAGEndpoint(ollamaEndpoint), embeddingModel)}
}

func normalizeRAGEndpoint(endpoint string) string {
	value := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	value = strings.TrimSuffix(value, "/api/chat")
	value = strings.TrimSuffix(value, "/api")
	return value
}

func (t *RAGSearchTool) Name() string { return "rag_search" }
func (t *RAGSearchTool) Description() string {
	return "Search the project's vector index semantically and return relevant code/documentation fragments. Requires prior rag_index."
}
func (t *RAGSearchTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"query": map[string]interface{}{"type": "string", "description": "What to look for, in natural language."},
		"top_k": map[string]interface{}{"type": "integer", "description": "Number of results (default 5)."},
	})
}

func (t *RAGSearchTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("rag_search arguments: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", fmt.Errorf("rag_search requires a query")
	}

	var allResults []rag.Result

	// 1. Search local workspace index
	index := rag.NewIndex(filepath.Join(t.workspace, ragIndexRel), t.embedder)
	if err := index.Load(); err == nil {
		if results, err := index.Search(context.Background(), args.Query, args.TopK); err == nil {
			allResults = append(allResults, results...)
		}
	}

	// 2. Search global skills index (~/.gokodek/global_rag_index.json)
	if homeDir, err := os.UserHomeDir(); err == nil {
		globalIndexPath := filepath.Join(homeDir, ".gokodek", "global_rag_index.json")
		globalIndex := rag.NewIndex(globalIndexPath, t.embedder)
		if err := globalIndex.Load(); err == nil {
			if results, err := globalIndex.Search(context.Background(), args.Query, args.TopK); err == nil {
				allResults = append(allResults, results...)
			}
		}
	}

	if len(allResults) == 0 {
		return "No se encontraron resultados en el índice del proyecto ni en el almacén de habilidades globales. Ejecuta rag_index o create_global_skill.", nil
	}

	sort.Slice(allResults, func(i, j int) bool { return allResults[i].Score > allResults[j].Score })
	topK := args.TopK
	if topK <= 0 {
		topK = 5
	}
	if len(allResults) > topK {
		allResults = allResults[:topK]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Resultados RAG (Proyecto + Habilidades Globales) para %q:\n", args.Query))
	for i, result := range allResults {
		sb.WriteString(fmt.Sprintf("\n[%d] %s (similitud %.2f)\n%s\n", i+1, result.Path, result.Score, preview(result.Content, 400)))
	}
	return sb.String(), nil
}

// collectTextFiles walks the workspace and returns text file paths, optionally
// filtered by a glob pattern.
func collectTextFiles(root, glob string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == ".gokodek" || name == "node_modules" || name == "dist" || name == ".venv" || name == "venv" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isIndexableText(path) {
			return nil
		}
		if glob != "" {
			matched, matchErr := filepath.Match(glob, info.Name())
			if matchErr != nil || !matched {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func isIndexableText(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".html", ".htm", ".css", ".scss",
		".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".sh", ".ps1", ".sql", ".rs", ".java", ".c", ".h", ".cpp", ".cs":
		return true
	}
	return false
}

func preview(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
