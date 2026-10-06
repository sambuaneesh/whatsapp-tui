// Package ai asks a small local chat model (Ollama, qwen3:4b by default)
// for help with things that are hard to do by rule: wording a message as
// a task, finding the to-dos in a chat, planning a day, and catching you
// up on a chat. It runs on your machine; nothing
// leaves it.
//
// The model never does calendar arithmetic (a 4B model gets it wrong
// most of the time, as tried): it copies time phrases out ("before friday
// evening") and the app's own parser (internal/when) turns them into
// dates.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client talks to an Ollama server.
type Client struct {
	URL   string // e.g. http://127.0.0.1:11434
	Model string // e.g. qwen3:4b
	HTTP  *http.Client
}

// keepAlive: how long Ollama keeps the model in GPU memory after an answer
// (another question soon after is quick; then the memory is freed, say for
// a game).
const keepAlive = "2m"

// ErrNoModel: the server doesn't answer or doesn't have the model.
var ErrNoModel = errors.New("the local model isn't available")

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 3 * time.Minute}
}

// Ready checks the server is up and has the model.
func (c *Client) Ready(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/tags", nil)
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("%w: Ollama isn't running (start it: ollama serve)", ErrNoModel)
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct{ Name string } `json:"models"`
	}
	json.NewDecoder(resp.Body).Decode(&tags)
	for _, m := range tags.Models {
		if m.Name == c.Model || strings.TrimSuffix(m.Name, ":latest") == c.Model {
			return nil
		}
	}
	return fmt.Errorf("%w: get it with: ollama pull %s", ErrNoModel, c.Model)
}

// ask sends one question and decodes the JSON answer (shaped by schema)
// into out. Thinking is off: answers come in about a second.
func (c *Client) ask(ctx context.Context, system, user string, schema map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{
		"model": c.Model, "stream": false, "think": false, "keep_alive": keepAlive,
		"options": map[string]any{"temperature": 0},
		"format":  schema,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoModel, err)
	}
	defer resp.Body.Close()
	var r struct {
		Message struct{ Content string } `json:"message"`
		Error   string                   `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("the model's answer: %w", err)
	}
	if r.Error != "" {
		if strings.Contains(r.Error, "not found") {
			return fmt.Errorf("%w: get it with: ollama pull %s", ErrNoModel, c.Model)
		}
		return errors.New(r.Error)
	}
	if err := json.Unmarshal([]byte(r.Message.Content), out); err != nil {
		return fmt.Errorf("the model's answer isn't what was asked: %w", err)
	}
	return nil
}

func object(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

var str = map[string]any{"type": "string"}

// ---------- a message as a task ----------

const taskSystem = `You read a chat message someone sent me and write the to-do it gives me.
"task": what I should do, as a short command (at most 8 words), naming the person if it helps.
"when": the words in the message that say by when or at what time, copied exactly as written (for example "before friday evening", "tomorrow at 4", "on the 12th at 11:59pm"); "" if the message gives no time.`

// TaskFromMessage words a message as a to-do, with the time phrase in it
// (for the app's parser) or "".
func (c *Client) TaskFromMessage(ctx context.Context, sender, text string) (task, when string, err error) {
	var out struct{ Task, When string }
	msg := text
	if sender != "" {
		msg = sender + ": " + text
	}
	err = c.ask(ctx, taskSystem, msg, object(map[string]any{"task": str, "when": str}, "task", "when"), &out)
	return strings.TrimSpace(out.Task), strings.TrimSpace(out.When), err
}

// ---------- the to-dos in a chat ----------

// Suggestion is a to-do found in a chat.
type Suggestion struct {
	Task string `json:"task"`
	When string `json:"when"` // the time phrase as written, or ""
	From string `json:"from"` // who asked
}

const chatTasksSystem = `You read the latest messages of a chat (one per line, "name: text"; "You" is me) and list what I still have to do: things people asked me (or everyone) to do, and things I promised.
For each, "task" is a complete short to-do that makes sense on its own, starting with a verb and saying what and for whom (at most 8 words), for example "Pay Kiran the rent" or "Send Mom the trip photos". "when" is the words that say by when, copied exactly as written, or "". "from" is who asked, or "You" for my own promises.
Leave out anything already done or answered, thanks and small talk. At most 8 items; an empty list if there's nothing.`

// TasksInChat finds the to-dos in a chat's latest messages (oldest first).
func (c *Client) TasksInChat(ctx context.Context, chat string, lines []string) ([]Suggestion, error) {
	var out struct {
		Tasks []Suggestion `json:"tasks"`
	}
	schema := object(map[string]any{"tasks": map[string]any{"type": "array",
		"items": object(map[string]any{"task": str, "when": str, "from": str}, "task", "when", "from")}}, "tasks")
	err := c.ask(ctx, chatTasksSystem, "Chat: "+chat+"\n\n"+strings.Join(lines, "\n"), schema, &out)
	var kept []Suggestion
	for _, s := range out.Tasks {
		if s.Task = strings.TrimSpace(s.Task); s.Task != "" {
			kept = append(kept, s)
		}
	}
	return kept, err
}

// ---------- planning a day ----------

// PlanTask is a task to plan.
type PlanTask struct {
	ID        int64  `json:"id"`
	Text      string `json:"text"`
	At        string `json:"at,omitempty"` // a fixed time, HH:MM
	Important bool   `json:"important,omitempty"`
}

// Slot is a task given a time.
type Slot struct {
	ID   int64  `json:"id"`
	Time string `json:"time"` // HH:MM
}

const planSystem = `You plan my day. I give the time now and my tasks as JSON: "at" is a fixed time that can't move; "important" ones should come early.
Give every task without a fixed time a start time (HH:MM, 24-hour) between now and 22:00, in a sensible order with breaks, short tasks between longer ones. Keep fixed times as they are and don't put other tasks on top of them.
Answer with "slots" (one per task: its id and time) and "note": one short sentence about the plan.`

// PlanDay suggests times for today's tasks.
func (c *Client) PlanDay(ctx context.Context, tasks []PlanTask, now time.Time) ([]Slot, string, error) {
	in, _ := json.Marshal(tasks)
	var out struct {
		Slots []Slot `json:"slots"`
		Note  string `json:"note"`
	}
	schema := object(map[string]any{"slots": map[string]any{"type": "array",
		"items": object(map[string]any{"id": map[string]any{"type": "integer"}, "time": str}, "id", "time")}, "note": str}, "slots", "note")
	err := c.ask(ctx, planSystem, "Now: "+now.Format("Monday 15:04")+"\nTasks: "+string(in), schema, &out)
	return out.Slots, strings.TrimSpace(out.Note), err
}

// ---------- catching up on a chat ----------

const summarySystem = `You catch me up on a chat. I give its latest messages, oldest first, one per line ("name: text"; "You" is me; "@You" means someone is talking to me).
"points": 2 to 5 short points (each under 15 words) on what was said or decided, most important first, naming people.
"asks": what someone is waiting for me to do or answer, as short to-dos starting with a verb (for example "Reply to Sam about the deadline"); [] if nothing.`

// Summary is a chat caught up.
type Summary struct {
	Points []string `json:"points"`
	Asks   []string `json:"asks"`
}

// Summarize catches you up on a chat's latest messages (oldest first).
func (c *Client) Summarize(ctx context.Context, chat string, lines []string) (Summary, error) {
	var out Summary
	arr := map[string]any{"type": "array", "items": str}
	err := c.ask(ctx, summarySystem, "Chat: "+chat+"\n\n"+strings.Join(lines, "\n"), object(map[string]any{"points": arr, "asks": arr}, "points", "asks"), &out)
	return out, err
}
