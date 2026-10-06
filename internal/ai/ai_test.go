package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeOllama answers /api/chat with answer and records the request.
func fakeOllama(t *testing.T, answer any) (*Client, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen3:4b"}}})
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		b, _ := json.Marshal(answer)
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": string(b)}})
	}))
	t.Cleanup(srv.Close)
	return &Client{URL: srv.URL, Model: "qwen3:4b"}, &got
}

func TestTaskFromMessage(t *testing.T) {
	c, got := fakeOllama(t, map[string]string{"task": "Send Arjun the slides", "when": "before friday evening"})
	task, when, err := c.TaskFromMessage(context.Background(), "Arjun", "can you send me the slides before friday evening?")
	if err != nil || task != "Send Arjun the slides" || when != "before friday evening" {
		t.Fatalf("%q %q %v", task, when, err)
	}
	if (*got)["think"] != false || (*got)["keep_alive"] != keepAlive {
		t.Fatalf("request: %v", *got)
	}
	if err := c.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Model = "other"
	if err := c.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "ollama pull other") {
		t.Fatalf("missing model: %v", err)
	}
}

func TestGuessDateUsesTheCalendar(t *testing.T) {
	now := time.Date(2026, 10, 6, 17, 0, 0, 0, time.Local)
	c, got := fakeOllama(t, map[string]any{"found": true, "day": 24, "time": ""})
	d, hasTime, ok, err := c.GuessDate(context.Background(), "the last friday of this month", now)
	if err != nil || !ok || hasTime || d.Day() != 30 || d.Month() != 10 {
		t.Fatalf("%v %v %v %v", d, hasTime, ok, err)
	}
	msgs := (*got)["messages"].([]any)
	if !strings.Contains(msgs[0].(map[string]any)["content"].(string), "24: Friday 30 October 2026") {
		t.Fatal("calendar not in the prompt")
	}
	c, _ = fakeOllama(t, map[string]any{"found": false, "day": 0, "time": ""})
	if _, _, ok, _ := c.GuessDate(context.Background(), "banana", now); ok {
		t.Fatal("banana isn't a date")
	}
}

func TestDown(t *testing.T) {
	c := &Client{URL: "http://127.0.0.1:1", Model: "qwen3:4b"}
	if err := c.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "ollama serve") {
		t.Fatalf("down: %v", err)
	}
}

// With WT_AI=1 and Ollama running, ask the real model.
func TestRealModel(t *testing.T) {
	if os.Getenv("WT_AI") == "" {
		t.Skip("set WT_AI=1 to ask the real model")
	}
	c := &Client{URL: "http://127.0.0.1:11434", Model: "qwen3:4b"}
	ctx := context.Background()
	task, when, err := c.TaskFromMessage(ctx, "Ravi", "lab report due on the 12th at 11:59pm, don't forget")
	t.Logf("task %q when %q err %v", task, when, err)
	sugg, err := c.TasksInChat(ctx, "Hostel", []string{
		"Priya: who's booking the cab for saturday?", "You: I'll do it tonight",
		"Ravi: also someone send me the wifi password", "Priya: thanks for the cake yesterday!",
		"Arjun: @You can you bring the projector to the lab on monday morning?"})
	t.Logf("tasks %+v err %v", sugg, err)
	sum, err := c.Summarize(ctx, "Hostel", []string{
		"Priya: dinner at 8 at Paradise?", "Ravi: I'm in", "Arjun: can't, exam tomorrow",
		"Priya: ok 7 of us then, booking", "Priya: @You you're coming right?"})
	t.Logf("summary %+v err %v", sum, err)
	slots, note, err := c.PlanDay(ctx, []PlanTask{{ID: 1, Text: "call mom", At: "18:00"}, {ID: 2, Text: "finish slides", Important: true},
		{ID: 3, Text: "gym"}, {ID: 4, Text: "pay electricity bill"}}, time.Date(2026, 10, 6, 10, 0, 0, 0, time.Local))
	t.Logf("plan %+v %q err %v", slots, note, err)
}
