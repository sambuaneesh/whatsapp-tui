// Package semantic finds messages by meaning: a local embedding model
// (Ollama's /api/embed, e.g. embeddinggemma) turns text into vectors, kept
// small (256 dimensions, one byte each) and compared by cosine similarity.
package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"
)

// Dims is how many dimensions are kept (EmbeddingGemma's vectors can be
// shortened to 256 with little loss).
const Dims = 256

// Embedder turns texts into vectors. query marks search queries (models
// embed queries and documents differently).
type Embedder interface {
	Embed(ctx context.Context, texts []string, query bool) ([][]float32, error)
}

// Ollama embeds with an Ollama server.
type Ollama struct {
	URL   string // e.g. http://127.0.0.1:11434
	Model string // e.g. embeddinggemma
	HTTP  *http.Client
}

// Available reports whether the server answers and has the model.
func (o Ollama) Available(ctx context.Context) bool {
	_, err := o.Embed(ctx, []string{"ping"}, true)
	return err == nil
}

// Embed asks the server for vectors, with the prompts EmbeddingGemma
// expects for queries and documents.
func (o Ollama) Embed(ctx context.Context, texts []string, query bool) ([][]float32, error) {
	in := make([]string, len(texts))
	for i, t := range texts {
		if query {
			in[i] = "task: search result | query: " + t
		} else {
			in[i] = "title: none | text: " + t
		}
	}
	body, _ := json.Marshal(map[string]any{"model": o.Model, "input": in})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.URL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := o.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if out.Error != "" {
		return nil, errors.New("embed: " + out.Error)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed: %d vectors for %d texts", len(out.Embeddings), len(texts))
	}
	return out.Embeddings, nil
}

// Quantize shortens a vector to Dims, normalises it and stores each value
// in one signed byte.
func Quantize(v []float32) []byte {
	if len(v) > Dims {
		v = v[:Dims]
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	out := make([]byte, len(v))
	if norm == 0 {
		return out
	}
	for i, x := range v {
		q := math.Round(float64(x) / norm * 127)
		out[i] = byte(int8(max(-127, min(127, q))))
	}
	return out
}

// Similarity is the cosine similarity of two quantized vectors (-1..1).
func Similarity(a, b []byte) float64 {
	n := min(len(a), len(b))
	var dot, na, nb int64
	for i := 0; i < n; i++ {
		x, y := int64(int8(a[i])), int64(int8(b[i]))
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float64(dot) / math.Sqrt(float64(na)*float64(nb))
}

// Scored is a match with its similarity.
type Scored struct {
	ID    string
	Score float64
}

// Top keeps the best n of a stream of scores, best first.
type Top struct {
	n     int
	items []Scored
}

// NewTop keeps n.
func NewTop(n int) *Top { return &Top{n: n} }

// Add considers one.
func (t *Top) Add(id string, score float64) {
	if len(t.items) < t.n {
		t.items = append(t.items, Scored{id, score})
		return
	}
	worst := 0
	for i, it := range t.items {
		if it.Score < t.items[worst].Score {
			worst = i
		}
	}
	if score > t.items[worst].Score {
		t.items[worst] = Scored{id, score}
	}
}

// Best returns the kept items, best first.
func (t *Top) Best() []Scored {
	sort.Slice(t.items, func(i, j int) bool { return t.items[i].Score > t.items[j].Score })
	return t.items
}
