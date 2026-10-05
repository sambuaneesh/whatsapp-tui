package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// The activity feed (I, :activity): mentions of you, replies to your
// messages and reactions to them, across all chats, newest first. enter (or
// a click) opens the message in its chat.

// ActivitySource lists your activity; *messages.SessionManager implements
// it.
type ActivitySource interface {
	Activity(ctx context.Context) ([]messages.ActivityItem, error)
}

type activityView struct {
	items   []messages.ActivityItem
	cursor  int
	offset  int
	loading bool
	err     error
}

type activityMsg struct {
	items []messages.ActivityItem
	err   error
}

// activityTop is the first row of the list (below the title and a blank).
const activityTop = 2

func (m *Model) openActivity() tea.Cmd {
	if m.activity == nil {
		m.notice, m.noticeErr = "the activity feed isn't available", true
		return nil
	}
	m.act = &activityView{loading: true}
	src := m.activity
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		items, err := src.Activity(ctx)
		return activityMsg{items: items, err: err}
	}
}

func (m Model) activityRows() int { return max(m.mainHeight()-activityTop-2, 1) }

func (m *Model) moveActivity(d int) {
	v := m.act
	v.cursor = min(max(v.cursor+d, 0), max(len(v.items)-1, 0))
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if rows := m.activityRows(); v.cursor >= v.offset+rows {
		v.offset = v.cursor - rows + 1
	}
}

func (m Model) handleActivity(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "I":
		m.act = nil
	case "j", "down":
		m.moveActivity(1)
	case "k", "up":
		m.moveActivity(-1)
	case "ctrl+d":
		m.moveActivity(m.activityRows() / 2)
	case "ctrl+u":
		m.moveActivity(-m.activityRows() / 2)
	case "G":
		m.moveActivity(len(m.act.items))
	case "g":
		m.moveActivity(-len(m.act.items))
	case "enter", "l":
		return m.openActivityItem()
	}
	return m, nil
}

func (m Model) mouseActivity(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.moveActivity(-3)
	case tea.MouseButtonWheelDown:
		m.moveActivity(3)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if i := m.act.offset + msg.Y - activityTop; msg.Y >= activityTop && i < len(m.act.items) {
			m.act.cursor = i
			return m.openActivityItem()
		}
	}
	return m, nil
}

// openActivityItem opens the chat at the selected item's message.
func (m Model) openActivityItem() (tea.Model, tea.Cmd) {
	v := m.act
	if v.cursor >= len(v.items) {
		return m, nil
	}
	it := v.items[v.cursor]
	m.act = nil
	var conv *messages.Conversation
	for _, c := range m.chats {
		if c.JID == it.ChatId {
			conv = c
		}
	}
	if conv == nil {
		conv = &messages.Conversation{JID: it.ChatId, Name: it.ChatName}
	}
	open := m.openChat(conv)
	return m, tea.Batch(open, m.loadAndSelect(it.ChatId, it.Msg.Id))
}

// activityLine is one item: when, what, where, and the message.
func (m Model) activityLine(it messages.ActivityItem, width int) string {
	when := toTime(it.Time).Format("Mon 2 Jan 15:04")
	text := strings.Join(strings.Fields(prettyTags(it.Msg.Text)), " ")
	var head string
	switch it.Kind {
	case messages.ActivityMention:
		head = "@ " + it.Who + " mentioned you in " + it.ChatName
	case messages.ActivityReply:
		head = "↩ " + it.Who + " replied in " + it.ChatName
	case messages.ActivityReaction:
		head = it.Emoji + " " + it.Who + " reacted in " + it.ChatName + " to"
	}
	return ansi.Truncate(fmt.Sprintf(" %-16s %s: %s", when, head, text), width, "…")
}

func (m Model) renderActivity(width, height int) string {
	v := m.act
	lines := []string{styleTitle.Render("Activity") + styleDim.Render("  · mentions, replies and reactions to you, across all chats"), ""}
	switch {
	case v.loading:
		lines = append(lines, styleDim.Render("Loading…"))
	case v.err != nil:
		lines = append(lines, styleErr.Render(v.err.Error()))
	case len(v.items) == 0:
		lines = append(lines, styleMuted.Render("Nothing yet: mentions of you, replies to you and reactions to your messages show up here."))
	}
	for i := v.offset; i < len(v.items) && i < v.offset+m.activityRows(); i++ {
		line := m.activityLine(v.items[i], width-4)
		if i == v.cursor {
			line = lipgloss.NewStyle().Background(pal.HighlightMed).Render(line)
		} else {
			line = styleBase.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", styleMuted.Render("j/k move · enter or click open · esc close"))
	return lipgloss.NewStyle().PaddingLeft(2).Width(width).Height(height).MaxHeight(height).
		Render(strings.Join(lines, "\n"))
}
