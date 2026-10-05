// Package api is whatsapp-tui's local API: a Unix socket speaking JSON
// lines, for scripts and tools, and hooks (executables run on events).
// See docs/API.md.
package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/when"
)

// Backend is what the API uses; *messages.SessionManager implements it.
type Backend interface {
	Connected() bool
	Conversations() []*messages.Conversation
	ChatName(ctx context.Context, jid string) string
	Messages(ctx context.Context, chat string, limit int, before int64) ([]messages.Message, error)
	Message(id string) (messages.Message, error)
	SearchAll(ctx context.Context, query string) ([]messages.SearchHit, error)
	SendText(ctx context.Context, chat, text string, mentions []string) error
	SendReply(ctx context.Context, chat, text string, quoted messages.Message, mentions []string) error
	MarkChatUnread(ctx context.Context, chat string, unread bool) error
	Schedule(ctx context.Context, it messages.Scheduled) (messages.Scheduled, error)
	Subscribe(f func(messages.Event)) (cancel func())
}

// Options configures the server.
type Options struct {
	AllowSend   bool         // send/reply/schedule a send; off by default
	SendsPerMin int          // rate limit for sends (default 20)
	HooksDir    string       // run executables here on events ("" for none)
	Log         func(string) // where hook failures go; may be nil
	Now         func() time.Time
}

// SocketPath is where the API listens: next to the background app's socket.
func SocketPath(daemonSocket string) string {
	return filepath.Join(filepath.Dir(daemonSocket), "whatsapp-tui-api.sock")
}

// Request is one JSON line from a client.
type Request struct {
	ID     any             `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers a request (same ID): Result, or Error.
type Response struct {
	ID     any    `json:"id,omitempty"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Server serves the API.
type Server struct {
	b    Backend
	opts Options
	ln   net.Listener

	sendMu    sync.Mutex
	sendTimes []time.Time
}

// Listen starts serving on path (a Unix socket only you can use) and runs
// hooks; Close stops both.
func Listen(path string, b Backend, opts Options) (*Server, error) {
	if opts.SendsPerMin <= 0 {
		opts.SendsPerMin = 20
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	_ = os.Remove(path) // left by an app that crashed
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	s := &Server{b: b, opts: opts, ln: ln}
	go s.accept()
	if opts.HooksDir != "" {
		s.runHooks()
	}
	return s, nil
}

// Close stops the server.
func (s *Server) Close() error {
	_ = os.Remove(s.ln.Addr().String())
	return s.ln.Close()
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

// conn is one client; writes are whole lines, one at a time.
type conn struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (c *conn) write(v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(v)
}

func (s *Server) serve(nc net.Conn) {
	defer nc.Close()
	c := &conn{enc: json.NewEncoder(nc)}
	var cancels []func()
	defer func() {
		for _, f := range cancels {
			f()
		}
	}()
	sc := bufio.NewScanner(nc)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			c.write(Response{Error: "not JSON: " + err.Error()})
			continue
		}
		if req.Method == "subscribe" {
			cancel, err := s.subscribe(c, req.Params)
			if err != nil {
				c.write(Response{ID: req.ID, Error: err.Error()})
				continue
			}
			cancels = append(cancels, cancel)
			c.write(Response{ID: req.ID, Result: "subscribed"})
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		res, err := s.call(ctx, req.Method, req.Params)
		cancel()
		if err != nil {
			c.write(Response{ID: req.ID, Error: err.Error()})
		} else {
			c.write(Response{ID: req.ID, Result: res})
		}
	}
}

// call runs one method (everything but subscribe).
func (s *Server) call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	var p struct {
		Chat    string `json:"chat"`
		Text    string `json:"text"`
		Query   string `json:"query"`
		Limit   int    `json:"limit"`
		Before  int64  `json:"before"`
		ReplyTo string `json:"reply_to"`
		At      string `json:"at"`
		Kind    string `json:"kind"`
		Unread  bool   `json:"unread"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("params: %w", err)
		}
	}
	need := func(field, v string) error {
		if v == "" {
			return fmt.Errorf("%s needs %q", method, field)
		}
		return nil
	}
	switch method {
	case "status":
		return map[string]any{"connected": s.b.Connected(), "send_allowed": s.opts.AllowSend}, nil
	case "chats":
		cs := s.b.Conversations()
		out := make([]ChatJSON, 0, len(cs))
		for _, c := range cs {
			out = append(out, chatJSON(c))
		}
		sortChats(out)
		if p.Limit > 0 && len(out) > p.Limit {
			out = out[:p.Limit]
		}
		return out, nil
	case "messages":
		if err := need("chat", p.Chat); err != nil {
			return nil, err
		}
		if p.Limit <= 0 || p.Limit > 1000 {
			p.Limit = 50
		}
		msgs, err := s.b.Messages(ctx, p.Chat, p.Limit, p.Before)
		if err != nil {
			return nil, err
		}
		name := s.b.ChatName(ctx, p.Chat)
		out := make([]MessageJSON, len(msgs))
		for i, m := range msgs {
			out[i] = messageJSON(m, name)
		}
		return out, nil
	case "search":
		if err := need("query", p.Query); err != nil {
			return nil, err
		}
		hits, err := s.b.SearchAll(ctx, p.Query)
		if err != nil {
			return nil, err
		}
		out := make([]MessageJSON, 0, len(hits))
		for _, h := range hits {
			if p.Chat == "" || h.ChatId == p.Chat {
				out = append(out, messageJSON(h.Message, h.ChatName))
			}
		}
		if p.Limit > 0 && len(out) > p.Limit {
			out = out[:p.Limit]
		}
		return out, nil
	case "send", "reply":
		if err := s.maySend(); err != nil {
			return nil, err
		}
		if err := need("chat", p.Chat); err != nil {
			return nil, err
		}
		if err := need("text", p.Text); err != nil {
			return nil, err
		}
		if method == "reply" || p.ReplyTo != "" {
			if err := need("reply_to", p.ReplyTo); err != nil {
				return nil, err
			}
			q, err := s.b.Message(p.ReplyTo)
			if err != nil {
				return nil, fmt.Errorf("reply_to: %w", err)
			}
			return "sent", s.b.SendReply(ctx, p.Chat, p.Text, q, nil)
		}
		return "sent", s.b.SendText(ctx, p.Chat, p.Text, nil)
	case "mark_read", "mark_unread":
		if err := need("chat", p.Chat); err != nil {
			return nil, err
		}
		return "ok", s.b.MarkChatUnread(ctx, p.Chat, method == "mark_unread")
	case "schedule":
		if err := need("chat", p.Chat); err != nil {
			return nil, err
		}
		if err := need("at", p.At); err != nil {
			return nil, err
		}
		kind := p.Kind
		if kind == "" {
			kind = messages.ScheduleSend
		}
		if kind == messages.ScheduleSend {
			if err := s.maySend(); err != nil {
				return nil, err
			}
		}
		now := s.opts.Now()
		due, err := when.Parse(p.At, now)
		if err != nil {
			return nil, err
		}
		it, err := s.b.Schedule(ctx, messages.Scheduled{Kind: kind, Chat: p.Chat, Text: p.Text, Due: due, Created: now})
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": it.ID, "due": due.Unix(), "when": when.Describe(due, now)}, nil
	}
	return nil, fmt.Errorf("unknown method %q (see docs/API.md)", method)
}

// maySend checks that sending is allowed and under the rate limit.
func (s *Server) maySend() error {
	if !s.opts.AllowSend {
		return errors.New("sending through the API is off: set api_allow_send = true in the config")
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	now := s.opts.Now()
	recent := s.sendTimes[:0]
	for _, t := range s.sendTimes {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	s.sendTimes = recent
	if len(recent) >= s.opts.SendsPerMin {
		return fmt.Errorf("rate limit: at most %d sends a minute", s.opts.SendsPerMin)
	}
	s.sendTimes = append(s.sendTimes, now)
	return nil
}

// subscribe streams events of the asked kinds (all when none given).
func (s *Server) subscribe(c *conn, raw json.RawMessage) (func(), error) {
	var p struct {
		Events []string `json:"events"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("params: %w", err)
		}
	}
	want := map[string]bool{}
	for _, e := range p.Events {
		switch e {
		case messages.EventMessage, messages.EventReaction, messages.EventReminder, "mention":
			want[e] = true
		default:
			return nil, fmt.Errorf("unknown event %q: message, mention, reaction or reminder", e)
		}
	}
	return s.b.Subscribe(func(e messages.Event) {
		for _, out := range s.eventsFor(e) {
			if len(want) == 0 || want[out.Event] {
				c.write(out)
			}
		}
	}), nil
}

// EventJSON is an event as clients and hooks see it.
type EventJSON struct {
	Event    string        `json:"event"` // message, mention, reaction, reminder
	Message  *MessageJSON  `json:"message,omitempty"`
	Reaction *ReactionJSON `json:"reaction,omitempty"`
	Reminder *ReminderJSON `json:"reminder,omitempty"`
}

// eventsFor turns a backend event into API events; a message that
// mentions you is also a "mention".
func (s *Server) eventsFor(e messages.Event) []EventJSON {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch e.Kind {
	case messages.EventMessage:
		m := messageJSON(e.Message, s.b.ChatName(ctx, e.Message.ChatId))
		out := []EventJSON{{Event: "message", Message: &m}}
		if e.MentionsYou {
			out = append(out, EventJSON{Event: "mention", Message: &m})
		}
		return out
	case messages.EventReaction:
		r := ReactionJSON{Chat: e.Reaction.Chat, ChatName: s.b.ChatName(ctx, e.Reaction.Chat), MessageID: e.Reaction.MessageID,
			Sender: e.Reaction.Sender, Emoji: e.Reaction.Emoji, FromMe: e.Reaction.FromMe}
		if r.Sender != "" {
			r.SenderName = s.b.ChatName(ctx, r.Sender)
		}
		return []EventJSON{{Event: "reaction", Reaction: &r}}
	case messages.EventReminder:
		return []EventJSON{{Event: "reminder", Reminder: &ReminderJSON{Chat: e.Reminder.Chat,
			ChatName: s.b.ChatName(ctx, e.Reminder.Chat), Text: e.Reminder.Text}}}
	}
	return nil
}
