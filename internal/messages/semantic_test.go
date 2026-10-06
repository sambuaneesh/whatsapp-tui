package messages

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Srindot/whatsapp-tui/internal/semantic"
)

// fakeOllama embeds by meaning, crudely: words with the same meaning share
// a dimension, other words get their own.
func fakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	concept := map[string]int{"address": 0, "location": 0, "where": 0, "flat": 1, "apartment": 1, "house": 1, "food": 2, "dinner": 2}
	embed := func(text string) []float32 {
		v := make([]float32, 300)
		for _, w := range strings.Fields(strings.ToLower(text)) {
			w = strings.Trim(w, "?.,!:|")
			if c, ok := concept[w]; ok {
				v[c] += 3
			} else if w != "" && w != "task" && w != "search" && w != "result" && w != "query" && w != "title" && w != "none" && w != "text" {
				h := fnv.New32a()
				h.Write([]byte(w))
				v[10+int(h.Sum32()%280)]++
			}
		}
		return v
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/api/embed" || req.Model != "embeddinggemma" {
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "model not found"})
			return
		}
		var out [][]float32
		for _, in := range req.Input {
			out = append(out, embed(in))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
	}))
}

func TestSearchByMeaning(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()
	sm := &SessionManager{uiHandler: NewMockUiHandler(), db: newTestDB(t), convByJID: map[string]*Conversation{}}
	for i, text := range []string{"send me the location of the apartment please", "what's for dinner tonight guys",
		"the flat address is 12B", "ok", "random chatter about cricket"} {
		_ = sm.db.AddMessage(Message{Id: string(rune('a' + i)), ChatId: "c@g.us", Text: text, Timestamp: uint64(100 + i)})
	}
	sm.StartSemantic(semantic.Ollama{URL: srv.URL, Model: "embeddinggemma"})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if left, _ := sm.db.UnembeddedMessages(10); len(left) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if left, _ := sm.db.UnembeddedMessages(10); len(left) != 0 {
		t.Fatalf("%d messages not indexed", len(left))
	}
	var n int
	sm.db.db.QueryRow(`SELECT count(*) FROM embeddings`).Scan(&n)
	if n != 4 { // "ok" is too short
		t.Fatalf("%d embedded", n)
	}
	hits, err := sm.SearchAll(context.Background(), "flat address")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range hits {
		tag := ""
		if h.Similar {
			tag = "~"
		}
		got = append(got, tag+h.Id)
	}
	// the words find "c"; by meaning, "a" (location of the apartment) too;
	// not dinner or cricket
	if strings.Join(got, ",") != "c,~a" {
		t.Fatalf("hits %v", got)
	}
}

func TestSemanticOffWithoutModel(t *testing.T) {
	sm := &SessionManager{uiHandler: NewMockUiHandler(), db: newTestDB(t), convByJID: map[string]*Conversation{}}
	sm.StartSemantic(semantic.Ollama{URL: "http://127.0.0.1:1", Model: "embeddinggemma"}) // nothing there
	time.Sleep(100 * time.Millisecond)
	_ = sm.db.AddMessage(Message{Id: "a", ChatId: "c@g.us", Text: "the flat address is 12B", Timestamp: 1})
	hits, err := sm.SearchAll(context.Background(), "flat")
	if err != nil || len(hits) != 1 || hits[0].Similar {
		t.Fatalf("%v %v", hits, err)
	}
}

func TestQuantizedSimilarity(t *testing.T) {
	a := semantic.Quantize([]float32{1, 0, 0})
	b := semantic.Quantize([]float32{2, 0.1, 0})
	c := semantic.Quantize([]float32{0, 1, 0})
	if s := semantic.Similarity(a, b); s < 0.99 {
		t.Fatalf("near: %f", s)
	}
	if s := semantic.Similarity(a, c); s > 0.01 {
		t.Fatalf("far: %f", s)
	}
	if len(semantic.Quantize(make([]float32, 768))) != semantic.Dims {
		t.Fatal("not shortened")
	}
}
