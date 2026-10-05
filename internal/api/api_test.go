package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

type fakeBackend struct {
	mu        sync.Mutex
	sent      []string
	scheduled []messages.Scheduled
	read      []string
	subs      []func(messages.Event)
}

func (f *fakeBackend) Connected() bool { return true }
func (f *fakeBackend) Conversations() []*messages.Conversation {
	return []*messages.Conversation{
		{JID: "1@s.whatsapp.net", Name: "Arjun", LastMsgTime: 100},
		{JID: "2@g.us", Name: "Hostel", LastMsgTime: 300, Unread: 2},
		{JID: "3@s.whatsapp.net", Name: "Pinned", LastMsgTime: 1, IsPinned: true},
	}
}
func (f *fakeBackend) ChatName(_ context.Context, jid string) string {
	return map[string]string{"1@s.whatsapp.net": "Arjun", "2@g.us": "Hostel", "9@s.whatsapp.net": "Priya"}[jid]
}
func (f *fakeBackend) Messages(_ context.Context, chat string, limit int, before int64) ([]messages.Message, error) {
	return []messages.Message{{Id: "m1", ChatId: chat, ContactId: "9@s.whatsapp.net", ContactShort: "Priya", Timestamp: 50, Text: "hi"},
		{Id: "m2", ChatId: chat, FromMe: true, Timestamp: 60, Text: "hello", Reactions: []messages.Reaction{{Sender: "9@s.whatsapp.net", Emoji: "👍"}}}}, nil
}
func (f *fakeBackend) Message(id string) (messages.Message, error) {
	if id == "m1" {
		return messages.Message{Id: "m1", ChatId: "2@g.us", Text: "hi"}, nil
	}
	return messages.Message{}, errors.New("no such message")
}
func (f *fakeBackend) SearchAll(_ context.Context, q string) ([]messages.SearchHit, error) {
	return []messages.SearchHit{{Message: messages.Message{Id: "a", ChatId: "2@g.us", Text: q}, ChatName: "Hostel"},
		{Message: messages.Message{Id: "b", ChatId: "1@s.whatsapp.net", Text: q}, ChatName: "Arjun"}}, nil
}
func (f *fakeBackend) SendText(_ context.Context, chat, text string, _ []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, chat+":"+text)
	return nil
}
func (f *fakeBackend) SendReply(_ context.Context, chat, text string, q messages.Message, _ []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, chat+":"+text+" (reply to "+q.Id+")")
	return nil
}
func (f *fakeBackend) MarkChatUnread(_ context.Context, chat string, unread bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.read = append(f.read, chat)
	return nil
}
func (f *fakeBackend) Schedule(_ context.Context, it messages.Scheduled) (messages.Scheduled, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it.ID = int64(len(f.scheduled) + 1)
	f.scheduled = append(f.scheduled, it)
	return it, nil
}
func (f *fakeBackend) Subscribe(fn func(messages.Event)) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs = append(f.subs, fn)
	return func() {}
}
func (f *fakeBackend) emit(e messages.Event) {
	f.mu.Lock()
	subs := make([]func(messages.Event), len(f.subs))
	copy(subs, f.subs)
	f.mu.Unlock()
	for _, s := range subs {
		s(e)
	}
}

func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "wtapi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// rpc sends one request and decodes the response.
func rpc(t *testing.T, path, method, params string) (any, string) {
	t.Helper()
	var out strings.Builder
	err := Call(path, method, params, &out)
	if err != nil {
		return nil, err.Error()
	}
	var v any
	_ = json.Unmarshal([]byte(out.String()), &v)
	return v, ""
}

func TestMethods(t *testing.T) {
	b := &fakeBackend{}
	path := filepath.Join(shortDir(t), "api.sock")
	now := time.Date(2026, 10, 7, 14, 0, 0, 0, time.Local)
	s, err := Listen(path, b, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", st.Mode())
	}

	v, _ := rpc(t, path, "status", "")
	if v.(map[string]any)["send_allowed"] != false {
		t.Fatalf("status %v", v)
	}
	v, _ = rpc(t, path, "chats", "")
	chats := v.([]any)
	if chats[0].(map[string]any)["name"] != "Pinned" || chats[1].(map[string]any)["name"] != "Hostel" ||
		chats[1].(map[string]any)["unread"].(float64) != 2 || chats[1].(map[string]any)["group"] != true {
		t.Fatalf("chats %v", chats)
	}
	v, _ = rpc(t, path, "messages", `{"chat":"2@g.us","limit":10}`)
	msgs := v.([]any)
	m2 := msgs[1].(map[string]any)
	if msgs[0].(map[string]any)["sender_name"] != "Priya" || m2["from_me"] != true || m2["sender_name"] != "You" ||
		m2["chat_name"] != "Hostel" || len(m2["reactions"].([]any)) != 1 {
		t.Fatalf("messages %v", msgs)
	}
	v, _ = rpc(t, path, "search", `{"query":"flat","chat":"2@g.us"}`)
	if hits := v.([]any); len(hits) != 1 || hits[0].(map[string]any)["text"] != "flat" {
		t.Fatalf("search %v", v)
	}
	if _, e := rpc(t, path, "messages", `{}`); !strings.Contains(e, `needs "chat"`) {
		t.Fatalf("missing param: %q", e)
	}
	if _, e := rpc(t, path, "nope", ""); !strings.Contains(e, "unknown method") {
		t.Fatalf("unknown method: %q", e)
	}
	// sending is off by default
	if _, e := rpc(t, path, "send", `{"chat":"1@s.whatsapp.net","text":"hi"}`); !strings.Contains(e, "api_allow_send") {
		t.Fatalf("send allowed by default: %q", e)
	}
	// a nudge isn't a send
	v, e := rpc(t, path, "schedule", `{"chat":"1@s.whatsapp.net","kind":"nudge","at":"in 2h"}`)
	if e != "" || v.(map[string]any)["when"] != "today 16:00" {
		t.Fatalf("schedule %v %q", v, e)
	}
	if _, e := rpc(t, path, "mark_read", `{"chat":"2@g.us"}`); e != "" || len(b.read) != 1 {
		t.Fatalf("mark_read %q", e)
	}
}

func TestSendingAllowedAndRateLimited(t *testing.T) {
	b := &fakeBackend{}
	path := filepath.Join(shortDir(t), "api.sock")
	s, err := Listen(path, b, Options{AllowSend: true, SendsPerMin: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, e := rpc(t, path, "send", `{"chat":"1@s.whatsapp.net","text":"hi"}`); e != "" {
		t.Fatal(e)
	}
	if _, e := rpc(t, path, "reply", `{"chat":"2@g.us","text":"yes","reply_to":"m1"}`); e != "" {
		t.Fatal(e)
	}
	if _, e := rpc(t, path, "send", `{"chat":"1@s.whatsapp.net","text":"third"}`); !strings.Contains(e, "rate limit") {
		t.Fatalf("not limited: %q", e)
	}
	if strings.Join(b.sent, "|") != "1@s.whatsapp.net:hi|2@g.us:yes (reply to m1)" {
		t.Fatalf("sent %v", b.sent)
	}
}

func TestSubscribeAndHooks(t *testing.T) {
	b := &fakeBackend{}
	dir := shortDir(t)
	hooks := filepath.Join(dir, "hooks")
	_ = os.Mkdir(hooks, 0o700)
	got := filepath.Join(dir, "got")
	script := "#!/bin/sh\necho \"$WT_EVENT $WT_CHAT $(cat)\" >> " + got + "\n"
	if err := os.WriteFile(filepath.Join(hooks, "on-mention"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(hooks, "on-reaction"), []byte(script), 0o600) // not executable: ignored
	path := filepath.Join(dir, "api.sock")
	s, err := Listen(path, b, Options{HooksDir: hooks})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write([]byte(`{"id":1,"method":"subscribe","params":{"events":["mention","reaction"]}}` + "\n"))
	r := bufio.NewReader(c)
	if line, _ := r.ReadString('\n'); !strings.Contains(line, "subscribed") {
		t.Fatalf("subscribe: %s", line)
	}
	b.emit(messages.Event{Kind: messages.EventMessage, Message: messages.Message{Id: "x", ChatId: "2@g.us", Text: "@919876 call"}, MentionsYou: true})
	b.emit(messages.Event{Kind: messages.EventMessage, Message: messages.Message{Id: "y", ChatId: "2@g.us", Text: "plain"}})
	b.emit(messages.Event{Kind: messages.EventReaction, Reaction: messages.ReactionEvent{Chat: "2@g.us", MessageID: "x", Sender: "9@s.whatsapp.net", Emoji: "❤️"}})
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	l1, _ := r.ReadString('\n')
	l2, _ := r.ReadString('\n')
	if !strings.Contains(l1, `"event":"mention"`) || !strings.Contains(l1, `"chat_name":"Hostel"`) ||
		!strings.Contains(l2, `"event":"reaction"`) || !strings.Contains(l2, `"sender_name":"Priya"`) {
		t.Fatalf("events:\n%s%s", l1, l2)
	}
	// the on-mention hook ran with the event; on-reaction (not executable) didn't
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, _ := os.ReadFile(got); len(data) > 0 {
			if s := string(data); !strings.HasPrefix(s, "mention 2@g.us {") || strings.Contains(s, "reaction") ||
				!strings.Contains(s, `"text":"@919876 call"`) {
				t.Fatalf("hook got %q", s)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("hook didn't run")
}
