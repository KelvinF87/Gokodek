package rag

import (
	"math"
	"testing"
)

func TestChunkText(t *testing.T) {
	text := ""
	for i := 0; i < 100; i++ {
		text += "palabra de ejemplo numero uno dos tres cuatro cinco seis siete ocho nueve "
	}
	chunks := chunkText(text, 200)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	// Verify no chunk exceeds the limit by much.
	for _, chunk := range chunks {
		if len(chunk) > 300 {
			t.Errorf("chunk too large: %d bytes", len(chunk))
		}
	}
	// Empty input yields no chunks.
	if got := chunkText("   ", 100); len(got) != 0 {
		t.Errorf("expected no chunks for blank input, got %d", len(got))
	}
}

func TestCosine(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{1, 0, 0}
	if got := cosine(a, b); math.Abs(got-1) > 1e-9 {
		t.Errorf("identical vectors should score 1, got %f", got)
	}
	orth := []float64{0, 1, 0}
	if got := cosine(a, orth); math.Abs(got) > 1e-9 {
		t.Errorf("orthogonal vectors should score 0, got %f", got)
	}
	if got := cosine([]float64{1, 2}, []float64{1}); got != 0 {
		t.Errorf("mismatched lengths should score 0, got %f", got)
	}
}
