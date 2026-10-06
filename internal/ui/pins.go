package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Pinned messages: P in visual mode pins the selected message for everyone
// (7 days, like the phone; the palette also offers 24 hours and 30 days),
// or unpins it. The newest pin shows in a bar under the chat's header;
// clicking it (or "Go: Pinned Message") jumps to it, then to the next.
//
// Pinning and muting chats live here too.

// Pinner pins messages; *messages.SessionManager implements it.
type Pinner interface {
	PinMessage(ctx context.Context, m messages.Message, d time.Duration) error
	PinnedMessages(ctx context.Context, chat string) ([]messages.Message, error)
}

// ChatFlagger pins and mutes chats; *messages.SessionManager implements it.
type ChatFlagger interface {
	PinChat(ctx context.Context, jid string, pin bool) error
	MuteChat(ctx context.Context, jid string, d time.Duration) error
}

func (m Model) pinner() Pinner {
	p, _ := m.actions.(Pinner)
	return p
}

func (m Model) flagger() ChatFlagger {
	if f, ok := m.triage.(ChatFlagger); ok {
		return f
	}
	f, _ := m.actions.(ChatFlagger)
	return f
}

type pinsMsg struct {
	chat string
	msgs []messages.Message
	err  error
}

// loadPins fetches the open chat's pinned messages.
func (m Model) loadPins() tea.Cmd {
	p := m.pinner()
	if p == nil || m.current == nil {
		return nil
	}
	chat := m.current.JID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		msgs, err := p.PinnedMessages(ctx, chat)
		return pinsMsg{chat: chat, msgs: msgs, err: err}
	}
}

func (m Model) applyPins(r pinsMsg) (tea.Model, tea.Cmd) {
	if m.current == nil || r.chat != m.current.JID || r.err != nil {
		return m, nil
	}
	before := m.pinRows()
	m.pins = r.msgs
	if m.pinIdx >= len(m.pins) {
		m.pinIdx = 0
	}
	if m.pinRows() != before {
		atBottom := m.vp.AtBottom()
		m.resize()
		if atBottom {
			m.vp.GotoBottom()
		}
	}
	return m, nil
}

// pinRows is 1 while the pinned bar shows.
func (m Model) pinRows() int {
	if m.screen == screenChat && m.current != nil && len(m.pins) > 0 {
		return 1
	}
	return 0
}

// renderPinBar draws the pinned message the bar points at.
func (m Model) renderPinBar(width int) string {
	if m.pinRows() == 0 {
		return ""
	}
	i := min(m.pinIdx, len(m.pins)-1)
	p := m.pins[i]
	bg := lipgloss.NewStyle().Background(colorBarBg)
	who := "You"
	if !p.FromMe {
		who = p.ContactName
		if who == "" {
			who = p.ContactShort
		}
	}
	text := plainText(prettyTags(strings.ReplaceAll(p.Text, "\n", " ")))
	left := bg.Foreground(pal.Gold).Render(" 📌 ") + bg.Foreground(pal.Iris).Render(who+": ") + bg.Foreground(pal.Text).Render(text)
	right := "click to see "
	if len(m.pins) > 1 {
		right = fmt.Sprintf("%d of %d · click for the next ", i+1, len(m.pins))
	}
	return fitRow(left, bg.Foreground(pal.Muted).Render(right), width, bg)
}

// jumpToPin selects the pinned message the bar shows, and moves the bar on
// to the next pin.
func (m Model) jumpToPin() (tea.Model, tea.Cmd) {
	if len(m.pins) == 0 {
		m.notice, m.noticeErr = "no pinned messages in this chat", true
		return m, nil
	}
	p := m.pins[min(m.pinIdx, len(m.pins)-1)]
	m.pinIdx = (m.pinIdx + 1) % len(m.pins)
	m.focus = paneMessages
	for i, x := range m.msgs {
		if x.Id == p.Id {
			m.selectJumped(i)
			return m, nil
		}
	}
	m.notice, m.noticeErr = "Finding the pinned message…", false
	return m, m.loadAndSelect(m.current.JID, p.Id)
}

// pinSelected pins the selected message for d, or unpins it if it's
// pinned (d < 0 unpins only, d = 0 toggles with the default length).
func (m Model) pinSelected(d time.Duration) (tea.Model, tea.Cmd) {
	sel, ok := m.selected()
	p := m.pinner()
	if !ok || p == nil {
		return m, nil
	}
	if sel.Deleted != 0 || strings.HasPrefix(sel.Text, "[REVOKED]") {
		m.notice, m.noticeErr = "a deleted message can't be pinned", true
		return m, nil
	}
	if d == 0 {
		d = messages.DefaultPinDuration
		if sel.Pinned {
			d = -1
		}
	}
	done := "Pinned for " + pinLength(d)
	if d < 0 {
		done, d = "Unpinned", 0
	}
	m.exitVisual()
	return m, m.action(done, func(ctx context.Context) (string, error) {
		return "", p.PinMessage(ctx, sel, d)
	})
}

func pinLength(d time.Duration) string {
	switch {
	case d >= 30*24*time.Hour:
		return "30 days"
	case d >= 7*24*time.Hour:
		return "7 days"
	}
	return "24 hours"
}

// pinChat pins or unpins a chat in the list (on all your devices).
func (m Model) pinChat(c *messages.Conversation) (tea.Model, tea.Cmd) {
	f := m.flagger()
	if c == nil || f == nil {
		return m, nil
	}
	pin := !c.IsPinned
	done := "Pinned " + chatName(c)
	if !pin {
		done = "Unpinned " + chatName(c)
	}
	jid := c.JID
	return m, m.action(done, func(ctx context.Context) (string, error) { return "", f.PinChat(ctx, jid, pin) })
}

// muteChat mutes a chat for d (< 0: always) or unmutes it (0).
func (m Model) muteChat(c *messages.Conversation, d time.Duration) (tea.Model, tea.Cmd) {
	f := m.flagger()
	if c == nil || f == nil {
		return m, nil
	}
	done := "Unmuted " + chatName(c)
	switch {
	case d < 0:
		done = "Muted " + chatName(c) + " (always; mentions of you still notify)"
	case d > 0:
		done = "Muted " + chatName(c) + " for " + muteLength(d)
	}
	jid := c.JID
	return m, m.action(done, func(ctx context.Context) (string, error) { return "", f.MuteChat(ctx, jid, d) })
}

func muteLength(d time.Duration) string {
	switch {
	case d >= 7*24*time.Hour:
		return "a week"
	case d >= 8*time.Hour:
		return "8 hours"
	}
	return "an hour"
}

// pinBarAt reports whether screen row y is the pinned bar.
func (m Model) pinBarAt(x, y int) bool {
	return m.pinRows() == 1 && y == headerRows && x > m.sidebarW && x <= m.sidebarW+m.rightWidth()
}

// muteDurations are :mute's lengths (WhatsApp offers 8 hours, a week and
// always).
var muteDurations = map[string]time.Duration{
	"1h": time.Hour, "8h": 8 * time.Hour, "1w": 7 * 24 * time.Hour, "week": 7 * 24 * time.Hour,
	"always": -1, "forever": -1,
}

// theChat is the chat a command acts on: the open one, else the one
// selected in the list.
func (m Model) theChat() *messages.Conversation {
	if inChat(m) && m.focus != paneList {
		return m.current
	}
	if c := m.selectedChat(); c != nil {
		return c
	}
	return m.current
}
