package ui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Split view: a second chat beside the open one. v in the chat list opens
// the selected chat there; it stays current as messages arrive. W swaps
// the two (you write in the left one), :only closes it.

// ChatReader loads a chat's latest messages; *messages.SessionManager
// implements it.
type ChatReader interface {
	ChatMessages(ctx context.Context, jid string) ([]messages.Message, error)
}

type splitView struct {
	conv    *messages.Conversation
	msgs    []messages.Message
	last    int64        // the chat's LastMsgTime when loaded
	bubbles *bubbleCache // its own: the panes would evict each other's
	lines   []string     // rendered at splitWidth
	width   int
}

type splitMsg struct {
	jid  string
	msgs []messages.Message
	err  error
}

// splitWidth is the right pane's width; rightWidth is the left one's.
func (m Model) splitWidth() int {
	if m.split == nil {
		return 0
	}
	return m.width - m.sidebarW - 1 - m.rightWidth() - 1
}

// openSplit shows c beside the open chat.
func (m *Model) openSplit(c *messages.Conversation) tea.Cmd {
	if c == nil {
		return nil
	}
	if m.chatReader == nil {
		m.notice, m.noticeErr = "split view isn't available", true
		return nil
	}
	if m.current == nil || m.screen != screenChat {
		return m.openChat(c) // nothing to put it beside
	}
	if c.JID == m.current.JID {
		m.notice, m.noticeErr = "that chat is already open on the left", true
		return nil
	}
	m.split = &splitView{conv: c, bubbles: newBubbleCache(), last: c.LastMsgTime}
	m.focus = paneMessages
	m.resize()
	m.refreshMessages(false)
	return m.loadSplit()
}

func (m *Model) closeSplit() {
	if m.split == nil {
		return
	}
	m.split = nil
	m.resize()
	m.refreshMessages(false)
}

func (m Model) loadSplit() tea.Cmd {
	if m.split == nil || m.chatReader == nil {
		return nil
	}
	r, jid := m.chatReader, m.split.conv.JID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := r.ChatMessages(ctx, jid)
		return splitMsg{jid: jid, msgs: msgs, err: err}
	}
}

func (m Model) applySplit(r splitMsg) (tea.Model, tea.Cmd) {
	if m.split == nil || m.split.conv.JID != r.jid {
		return m, nil
	}
	if r.err != nil {
		m.notice, m.noticeErr = r.err.Error(), true
		return m, nil
	}
	m.split.msgs = r.msgs
	m.renderSplit()
	return m, nil
}

// splitChanged reloads the split chat when the chat list shows it got
// something new.
func (m *Model) splitChanged() tea.Cmd {
	if m.split == nil {
		return nil
	}
	for _, c := range m.chats {
		if c.JID == m.split.conv.JID && c.LastMsgTime != m.split.last {
			m.split.last = c.LastMsgTime
			return m.loadSplit()
		}
	}
	return nil
}

// swapSplit makes the split chat the one you write in, and puts the open
// one beside it.
func (m Model) swapSplit() (tea.Model, tea.Cmd) {
	if m.split == nil || m.current == nil {
		return m, nil
	}
	left := m.current
	right := m.split.conv
	open := m.openChat(right)
	m.split = &splitView{conv: left, bubbles: newBubbleCache(), last: left.LastMsgTime}
	m.resize()
	m.refreshMessages(true)
	return m, tea.Batch(open, m.loadSplit())
}

// renderSplit draws the split chat's messages at its width, with a copy of
// the model showing that chat (read-only: no selection, search or images
// loading state of its own).
func (m *Model) renderSplit() {
	s := m.split
	if s == nil {
		return
	}
	w := m.splitWidth()
	view := *m
	view.current, view.msgs, view.bubbles = s.conv, s.msgs, s.bubbles
	view.mode, view.search, view.unreadID, view.rangeFrom = modeNormal, nil, "", noRange
	s.lines, _ = view.renderMessages(w)
	s.width = w
}

// splitPane is the right pane: a header and the newest messages that fit.
func (m Model) splitPane(height int) []string {
	s := m.split
	w := m.splitWidth()
	if s.width != w {
		m.renderSplit() // resized (s is shared, so this sticks)
	}
	// the header, with a ✕ at its right end that closes the split
	title := " " + styleTitle.Render(chatName(s.conv)) + styleDim.Render("  beside · W swap · X close")
	head := padLine(ansi.Truncate(title, w-splitCloseW, "…"), w-splitCloseW) + styleErr.Render(" ✕  ")
	lines := []string{head, lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", w))}
	body := height - len(lines)
	msgs := s.lines
	if len(s.msgs) == 0 {
		msgs = []string{"", styleDim.Render(" Loading…")}
	}
	if len(msgs) > body {
		msgs = msgs[len(msgs)-body:]
	}
	for _, l := range msgs {
		lines = append(lines, padLine(l, w))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return lines
}

// splitCloseW is the width of the ✕ at the end of the split's header.
const splitCloseW = 4

// splitCloseAt reports whether screen cell (x, y) is on the split's ✕.
func (m Model) splitCloseAt(x, y int) bool {
	if m.split == nil || y != 0 {
		return false
	}
	end := m.sidebarW + 1 + m.rightWidth() + 1 + m.splitWidth()
	return x >= end-splitCloseW && x < end
}

// splitByName opens the chat whose name matches q beside the open one.
func (m *Model) splitByName(q string) tea.Cmd {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		m.notice, m.noticeErr = ":split <name>, e.g. :split priya (or v on a chat in the list)", true
		return nil
	}
	for _, c := range m.chats {
		if !c.IsArchived && filterMatches(c, q) && (m.current == nil || c.JID != m.current.JID) {
			return m.openSplit(c)
		}
	}
	m.notice, m.noticeErr = "no chat matches "+q, true
	return nil
}

// splitDivider is the line between the panes.
func splitDivider() string { return lipgloss.NewStyle().Foreground(colorBorder).Render("│") }
