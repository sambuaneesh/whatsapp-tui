package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Opening a chat used to show an empty pane until the backend had loaded
// it. Now the chats you've just looked at, and the one highlighted in the
// list or the palette, are kept here, so opening one draws it at once; the
// backend's full load replaces it a moment later.

// preloadMax is how many chats are kept.
const preloadMax = 24

// preloadDelay is how long the highlight rests on a chat before it's
// loaded, so scrolling past chats doesn't load each one.
const preloadDelay = 60 * time.Millisecond

type preload struct {
	chats   map[string]preloaded
	order   []string // oldest first
	want    string   // the chat waiting for preloadDelay
	loading map[string]bool
}

type preloaded struct {
	last int64 // the chat's LastMsgTime when kept: newer means stale
	msgs []messages.Message
}

type preloadTick struct{ jid string }

type preloadedMsg struct {
	jid  string
	last int64
	msgs []messages.Message
	err  error
}

func newPreload() *preload {
	return &preload{chats: map[string]preloaded{}, loading: map[string]bool{}}
}

// get returns a copy of c's messages if they're still current.
func (p *preload) get(c *messages.Conversation) ([]messages.Message, bool) {
	if p == nil || c == nil {
		return nil, false
	}
	e, ok := p.chats[c.JID]
	if !ok || e.last != c.LastMsgTime || len(e.msgs) == 0 {
		return nil, false
	}
	return append([]messages.Message(nil), e.msgs...), true
}

func (p *preload) put(jid string, last int64, msgs []messages.Message) {
	if p == nil || jid == "" || len(msgs) == 0 {
		return
	}
	if _, ok := p.chats[jid]; ok {
		for i, j := range p.order {
			if j == jid {
				p.order = append(p.order[:i], p.order[i+1:]...)
				break
			}
		}
	}
	p.chats[jid] = preloaded{last: last, msgs: append([]messages.Message(nil), msgs...)}
	p.order = append(p.order, jid)
	for len(p.order) > preloadMax {
		delete(p.chats, p.order[0])
		p.order = p.order[1:]
	}
}

// lastMsgTime is the chat's LastMsgTime in the current list.
func (m Model) lastMsgTime(jid string) int64 {
	for _, c := range m.chats {
		if c.JID == jid {
			return c.LastMsgTime
		}
	}
	if m.current != nil && m.current.JID == jid {
		return m.current.LastMsgTime
	}
	return 0
}

// keepOpenChat remembers the open chat's messages, to show them at once
// when it's opened again.
func (m Model) keepOpenChat() {
	if m.current != nil {
		m.preload.put(m.current.JID, m.lastMsgTime(m.current.JID), m.msgs)
	}
}

// highlighted is the chat that enter would open: the palette's choice, or
// the list's while it has the focus.
func (m Model) highlighted() *messages.Conversation {
	if m.qo != nil {
		return m.qo.chosenChat()
	}
	if m.mode == modeFilter || m.screen == screenList || m.focus == paneList {
		c, _ := m.itemAt(m.cursor)
		return c
	}
	return nil
}

// prefetch starts loading c once the highlight has rested on it.
func (m Model) prefetch(c *messages.Conversation) tea.Cmd {
	p := m.preload
	if p == nil || c == nil || m.chatReader == nil || c.LastMsgTime == 0 || isPersonal(c.JID) || p.want == c.JID || p.loading[c.JID] {
		return nil
	}
	if m.current != nil && m.current.JID == c.JID {
		return nil
	}
	if _, ok := p.get(c); ok {
		return nil
	}
	p.want = c.JID
	jid := c.JID
	return tea.Tick(preloadDelay, func(time.Time) tea.Msg { return preloadTick{jid: jid} })
}

func (m Model) onPreloadTick(t preloadTick) tea.Cmd {
	p := m.preload
	if p == nil || p.want != t.jid || m.chatReader == nil {
		return nil
	}
	p.want = ""
	p.loading[t.jid] = true
	r, jid, last := m.chatReader, t.jid, m.lastMsgTime(t.jid)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := r.ChatMessages(ctx, jid)
		return preloadedMsg{jid: jid, last: last, msgs: msgs, err: err}
	}
}

func (m Model) onPreloaded(r preloadedMsg) {
	if m.preload == nil {
		return
	}
	delete(m.preload.loading, r.jid)
	if r.err == nil {
		m.preload.put(r.jid, r.last, r.msgs)
	}
}
