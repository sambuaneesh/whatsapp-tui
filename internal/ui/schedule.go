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
	"github.com/Srindot/whatsapp-tui/internal/when"
)

// Doing things later, run by the background app:
//
//	:later <when>   send what's in the input box then
//	:snooze <when>  archive the chat until then; it comes back unread
//	:nudge <when>   remind you then if they haven't replied
//	:scheduled      what's pending (x cancels)

// Scheduler keeps and runs scheduled items; *messages.SessionManager
// implements it.
type Scheduler interface {
	Schedule(ctx context.Context, it messages.Scheduled) (messages.Scheduled, error)
	Unschedule(ctx context.Context, id int64) error
	ScheduledItems() []messages.Scheduled
}

// scheduledMsg is the pending list, after any change.
type scheduledMsg []messages.Scheduled

// schedView is the list of what's scheduled.
type schedView struct{ cursor int }

// Rows of the list start below the title and a blank line.
const schedTop = 2

func (m *Model) schedCmd(ok string, f func(ctx context.Context, s Scheduler) error) tea.Cmd {
	s := m.scheduler
	if s == nil {
		m.notice, m.noticeErr = "scheduling isn't available", true
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return actionDoneMsg{ok: ok, err: f(ctx, s)}
	}
}

// scheduleCommand handles :later, :snooze and :nudge.
func (m Model) scheduleCommand(kind string, args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		if kind == messages.ScheduleSend {
			return m, m.openScheduled()
		}
		m.notice, m.noticeErr = ":"+map[string]string{messages.ScheduleSnooze: "snooze", messages.ScheduleNudge: "nudge"}[kind]+
			" <when>, e.g. 9am, tomorrow, in 2h, fri 5pm", true
		return m, nil
	}
	now := clickNow()
	due, err := when.Parse(strings.Join(args, " "), now)
	if err != nil {
		m.notice, m.noticeErr = err.Error(), true
		return m, nil
	}
	c := m.current
	if m.screen == screenList || m.focus == paneList {
		c = m.selectedChat()
	}
	if c == nil {
		m.notice, m.noticeErr = "open a chat first", true
		return m, nil
	}
	at, name := when.Describe(due, now), chatName(c)
	it := messages.Scheduled{Kind: kind, Chat: c.JID, Due: due, Created: now}
	switch kind {
	case messages.ScheduleSend:
		text := strings.TrimSpace(m.compose.Value())
		if text == "" || m.current == nil || c.JID != m.current.JID {
			m.notice, m.noticeErr = "write the message first (i), then :later <when>", true
			return m, nil
		}
		it.Text, it.Mentions = mentionsForSend(text, m.chosen)
		m.compose.SetValue("")
		m.chosen, m.mention = nil, nil
		m.fitCompose()
		return m, m.schedCmd("Will send to "+name+" "+at+" · :scheduled to see or cancel", func(ctx context.Context, s Scheduler) error {
			_, err := s.Schedule(ctx, it)
			return err
		})
	case messages.ScheduleSnooze:
		cmd := m.schedCmd("Snoozed "+name+" until "+at, func(ctx context.Context, s Scheduler) error {
			_, err := s.Schedule(ctx, it)
			return err
		})
		if m.screen == screenChat && m.current != nil && c.JID == m.current.JID {
			return m, tea.Batch(cmd, m.back())
		}
		return m, cmd
	default: // nudge
		return m, m.schedCmd("Will remind you "+at+" if "+name+" hasn't replied", func(ctx context.Context, s Scheduler) error {
			_, err := s.Schedule(ctx, it)
			return err
		})
	}
}

func (m *Model) openScheduled() tea.Cmd {
	m.sched = &schedView{}
	if m.scheduler == nil {
		return nil
	}
	s := m.scheduler
	return func() tea.Msg { return scheduledMsg(s.ScheduledItems()) }
}

func (m Model) handleScheduled(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.sched
	switch msg.String() {
	case "esc", "q":
		m.sched = nil
	case "j", "down":
		v.cursor = min(v.cursor+1, max(len(m.scheduled)-1, 0))
	case "k", "up":
		v.cursor = max(v.cursor-1, 0)
	case "x", "d":
		if v.cursor < len(m.scheduled) {
			id := m.scheduled[v.cursor].ID
			return m, m.schedCmd("Cancelled", func(ctx context.Context, s Scheduler) error {
				return s.Unschedule(ctx, id)
			})
		}
	}
	return m, nil
}

func (m Model) mouseScheduled(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
		if i := msg.Y - schedTop; i >= 0 && i < len(m.scheduled) {
			m.sched.cursor = i
		}
	}
	return m, nil
}

// scheduledChatName names a chat in the list.
func (m Model) scheduledChatName(jid string) string {
	for _, c := range m.chats {
		if c.JID == jid {
			return chatName(c)
		}
	}
	return chatName(&messages.Conversation{JID: jid})
}

func (m Model) renderScheduled(width, height int) string {
	lines := []string{styleTitle.Render("Scheduled") + styleDim.Render("  · run by the background app, even with the window closed"), ""}
	if len(m.scheduled) == 0 {
		lines = append(lines, styleMuted.Render("Nothing scheduled. In a chat: :later 9am (sends what you wrote), :snooze tomorrow, :nudge 3h"))
	}
	now := clickNow()
	cur := min(m.sched.cursor, max(len(m.scheduled)-1, 0))
	for i, it := range m.scheduled {
		what := ""
		switch it.Kind {
		case messages.ScheduleSend:
			what = "✉ send  " + strings.ReplaceAll(it.Text, "\n", " ")
		case messages.ScheduleSnooze:
			what = "💤 back from snooze"
		case messages.ScheduleNudge:
			what = "⏰ remind if no reply"
		}
		line := fmt.Sprintf(" %-16s %-22s %s", when.Describe(it.Due, now), ansi.Truncate(m.scheduledChatName(it.Chat), 22, "…"), what)
		line = ansi.Truncate(line, width-4, "…")
		if i == cur {
			line = lipgloss.NewStyle().Background(pal.HighlightMed).Render(line)
		} else {
			line = styleBase.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", styleMuted.Render("j/k move · x cancel · esc close"))
	return lipgloss.NewStyle().PaddingLeft(2).Width(width).Height(height).MaxHeight(height).
		Render(strings.Join(lines, "\n"))
}

// scheduledBadge is the status bar's count of pending items ("" for none).
func (m Model) scheduledBadge() string {
	if len(m.scheduled) == 0 {
		return ""
	}
	return lipgloss.NewStyle().Background(colorBarBg).Foreground(colorWarm).
		Render(fmt.Sprintf(" ⏰ %d ", len(m.scheduled)))
}
