package messages

import (
	"context"
	"sync"
)

// Events for scripts: the local API and hooks subscribe to what happens.

// Kinds of events.
const (
	EventMessage  = "message"  // a message arrived, or you sent one
	EventReaction = "reaction" // someone (or you) reacted
	EventReminder = "reminder" // a snoozed chat came back, or a nudge
)

// Event is something that happened. Data is a Message (message), a
// ReactionEvent (reaction) or a ReminderEvent (reminder).
type Event struct {
	Kind        string
	Message     Message
	MentionsYou bool // message: it mentions you (or @all)
	Reaction    ReactionEvent
	Reminder    ReminderEvent
}

// ReactionEvent is a reaction to a message.
type ReactionEvent struct {
	Chat, MessageID, Sender, Emoji string // Sender "" is you; Emoji "" removes
	FromMe                         bool
}

// ReminderEvent is a scheduled reminder going off.
type ReminderEvent struct {
	Chat, Text string
}

type observers struct {
	mu   sync.Mutex
	next int
	fs   map[int]func(Event)
}

// Subscribe calls f for every event (on its own goroutine per event batch;
// f must not block for long). Call the returned func to stop.
func (sm *SessionManager) Subscribe(f func(Event)) (cancel func()) {
	o := &sm.observers
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fs == nil {
		o.fs = map[int]func(Event){}
	}
	id := o.next
	o.next++
	o.fs[id] = f
	return func() {
		o.mu.Lock()
		delete(o.fs, id)
		o.mu.Unlock()
	}
}

// emit tells every subscriber, without holding up the caller.
func (sm *SessionManager) emit(e Event) {
	o := &sm.observers
	o.mu.Lock()
	fs := make([]func(Event), 0, len(o.fs))
	for _, f := range o.fs {
		fs = append(fs, f)
	}
	o.mu.Unlock()
	for _, f := range fs {
		go f(e)
	}
}

// Message returns one stored message.
func (sm *SessionManager) Message(id string) (Message, error) { return sm.db.GetMessage(id) }

// Messages returns up to limit of a chat's messages before the given time
// (unix seconds; 0 means the newest), oldest first.
func (sm *SessionManager) Messages(_ context.Context, chat string, limit int, before int64) ([]Message, error) {
	msgs, err := sm.db.MessagesBefore(chat, limit, before)
	if err != nil {
		return nil, err
	}
	sm.resolveSenders(msgs)
	sm.attachReactions(msgs)
	sm.resolveMentions(msgs)
	return msgs, nil
}

// Connected reports whether WhatsApp is connected.
func (sm *SessionManager) Connected() bool {
	c := sm.getClient()
	return c != nil && c.IsConnected()
}

// MessagesBefore reads up to limit messages of a chat before a time (unix
// seconds; 0: the newest), oldest first.
func (md *MessageDatabase) MessagesBefore(chat string, limit int, before int64) ([]Message, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := md.db.Query(`SELECT * FROM (SELECT `+msgColumns+` FROM messages
		WHERE chat_id = ? AND timestamp < ? ORDER BY timestamp DESC LIMIT ?) ORDER BY timestamp ASC`, chat, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectMessages(rows)
}
