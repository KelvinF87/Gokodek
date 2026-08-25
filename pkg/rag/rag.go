package rag

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Embedder struct {
	Endpoint string
	Model    string
	Client   *http.Client
}

func NewEmbedder(endpoint, model string) *Embedder {
	if strings.TrimSpace(model) == "" {
		model = "nomic-embed-text"
	}
	return &Embedder{Endpoint: strings.TrimRight(endpoint, "/"), Model: model, Client: &http.Client{Timeout: 60 * time.Second}}
}
func (e *Embedder) Embed(ctx context.Context, text string) ([]float64, error) {
	b, err := json.Marshal(map[string]interface{}{"model": e.Model, "input": text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint+"/api/embed", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := e.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		d, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		return nil, fmt.Errorf("ollama embed HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(d)))
	}
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama embed returned no embeddings")
	}
	return out.Embeddings[0], nil
}

type Chunk struct {
	Path    string    `json:"path"`
	Content string    `json:"content"`
	Vector  []float64 `json:"vector"`
	Hash    string    `json:"hash,omitempty"`
	ModTime int64     `json:"mod_time,omitempty"`
}
type Index struct {
	mu       sync.Mutex
	Path     string
	Chunks   []Chunk
	Embedder *Embedder
}

func NewIndex(path string, embedder *Embedder) *Index { return &Index{Path: path, Embedder: embedder} }
func (ix *Index) Load() error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	d, e := os.ReadFile(ix.Path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	return json.Unmarshal(d, &ix.Chunks)
}
func (ix *Index) Save() error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if e := os.MkdirAll(filepath.Dir(ix.Path), 0700); e != nil {
		return e
	}
	d, e := json.MarshalIndent(ix.Chunks, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(ix.Path, d, 0600)
}

// AddFile incrementally replaces previous chunks for the file and embeds only its current content.
func (ix *Index) AddFile(ctx context.Context, path string, maxChunkBytes int) (int, error) {
	d, e := os.ReadFile(path)
	if e != nil {
		return 0, e
	}
	info, e := os.Stat(path)
	if e != nil {
		return 0, e
	}
	hash := sha256.Sum256(d)
	fingerprint := hex.EncodeToString(hash[:])
	ix.mu.Lock()
	for _, c := range ix.Chunks {
		if c.Path == path && c.Hash == fingerprint {
			ix.mu.Unlock()
			return 0, nil
		}
	}
	filtered := ix.Chunks[:0]
	for _, c := range ix.Chunks {
		if c.Path != path {
			filtered = append(filtered, c)
		}
	}
	ix.Chunks = filtered
	ix.mu.Unlock()
	chunks := chunkText(string(d), maxChunkBytes)
	added := 0
	for _, content := range chunks {
		vec, e := ix.Embedder.Embed(ctx, content)
		if e != nil {
			return added, e
		}
		ix.mu.Lock()
		ix.Chunks = append(ix.Chunks, Chunk{Path: path, Content: content, Vector: vec, Hash: fingerprint, ModTime: info.ModTime().UnixNano()})
		ix.mu.Unlock()
		added++
	}
	return added, nil
}

type Result struct {
	Path    string
	Content string
	Score   float64
}

func (ix *Index) Search(ctx context.Context, q string, k int) ([]Result, error) {
	v, e := ix.Embedder.Embed(ctx, q)
	if e != nil {
		return nil, e
	}
	ix.mu.Lock()
	cs := append([]Chunk(nil), ix.Chunks...)
	ix.mu.Unlock()
	r := make([]Result, 0, len(cs))
	for _, c := range cs {
		r = append(r, Result{c.Path, c.Content, cosine(v, c.Vector)})
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Score > r[j].Score })
	if k <= 0 {
		k = 5
	}
	if len(r) > k {
		r = r[:k]
	}
	return r, nil
}
func (ix *Index) Stats() string {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	p := map[string]bool{}
	for _, c := range ix.Chunks {
		p[c.Path] = true
	}
	return fmt.Sprintf("%d archivos indexados, %d fragmentos", len(p), len(ix.Chunks))
}
func chunkText(text string, max int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if max <= 0 {
		max = 1000
	}
	overlap := max / 4
	if overlap > 200 {
		overlap = 200
	}
	var out []string
	for len(text) > max {
		cut := max
		if i := strings.LastIndex(text[:cut], "\n"); i > cut/2 {
			cut = i + 1
		} else if i := strings.LastIndex(text[:cut], " "); i > cut/2 {
			cut = i + 1
		}
		out = append(out, strings.TrimSpace(text[:cut]))
		start := cut - overlap
		if start < 0 {
			start = 0
		}
		text = text[start:]
	}
	if strings.TrimSpace(text) != "" {
		out = append(out, strings.TrimSpace(text))
	}
	return out
}
func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
