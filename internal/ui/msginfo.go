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

// Message info (i on your own message in visual mode): who has read it,
// who has it, and in a group who's still waiting, with times. Like the
// phone's "Message info".

// ReceiptReader tells who has seen a message; *messages.SessionManager
// implements it.
type ReceiptReader interface {
	MessageReceipts(ctx context.Context, m messages.Message) (messages.MessageInfo, error)
}

type msgInfoView struct {
	msg    messages.Message
	info   *messages.MessageInfo
	err    error
	scroll int
}

type msgInfoMsg struct {
	id   string
	info messages.MessageInfo
	err  error
}

func (m Model) openMessageInfo(sel messages.Message) (tea.Model, tea.Cmd) {
	rr, ok := m.actions.(ReceiptReader)
	if !ok {
		return m, nil
	}
	if !sel.FromMe {
		m.notice, m.noticeErr = "message info is for your own messages", true
		return m, nil
	}
	m.minfo = &msgInfoView{msg: sel}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		info, err := rr.MessageReceipts(ctx, sel)
		return msgInfoMsg{id: sel.Id, info: info, err: err}
	}
}

func (m Model) applyMessageInfo(r msgInfoMsg) (tea.Model, tea.Cmd) {
	if m.minfo == nil || m.minfo.msg.Id != r.id {
		return m, nil
	}
	m.minfo.info, m.minfo.err = &r.info, r.err
	return m, nil
}

func (m Model) handleMessageInfo(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "i", "backspace":
		m.minfo = nil
	case "j", "down":
		m.minfo.scroll++
	case "k", "up":
		m.minfo.scroll = max(m.minfo.scroll-1, 0)
	}
	return m, nil
}

// receiptTime says when: "14:05", "yesterday 14:05", "Mon 14:05", "2 Oct
// 14:05".
func receiptTime(ms int64, now time.Time) string {
	if ms <= 0 {
		return ""
	}
	t := time.UnixMilli(ms)
	start, _ := dayBounds(now)
	switch {
	case !t.Before(start):
		return t.Format("15:04")
	case !t.Before(start.AddDate(0, 0, -1)):
		return "yesterday " + t.Format("15:04")
	case !t.Before(start.AddDate(0, 0, -6)):
		return t.Format("Mon 15:04")
	}
	return t.Format("2 Jan 15:04")
}

func (m Model) messageInfoLines(width int) []string {
	v := m.minfo
	now := time.Now()
	text := strings.ReplaceAll(prettyTags(v.msg.Text), "\n", " ")
	lines := []string{
		styleTitle.Render("Message info") + styleDim.Render("  ·  sent "+receiptTime(int64(v.msg.Timestamp)*1000, now)),
		styleDim.Render(ansi.Truncate(text, max(width-6, 10), "…")),
		"",
	}
	switch {
	case v.err != nil:
		return append(lines, styleErr.Render(v.err.Error()))
	case v.info == nil:
		return append(lines, styleMuted.Render("Asking who has seen it…"))
	}
	info := v.info
	if info.NoRecord {
		return append(lines, styleMuted.Render("Who read this wasn't recorded: it was sent before the app kept receipts per person."),
			styleMuted.Render("Messages you send from now on show who read them, and when."))
	}
	name := func(r messages.Receipt) string {
		if r.Name != "" {
			return r.Name
		}
		return r.User
	}
	section := func(title string, color lipgloss.Color, rs []messages.Receipt, at func(messages.Receipt) string) {
		if len(rs) == 0 {
			return
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("%s  %d", title, len(rs))))
		for _, r := range rs {
			row := "  " + styleBase.Render(padRight(name(r), 32))
			if when := at(r); when != "" {
				row += styleDim.Render(when)
			}
			lines = append(lines, row)
		}
		lines = append(lines, "")
	}
	readTitle := "✓✓ Read by"
	if !info.Group {
		readTitle = "✓✓ Read"
	}
	section(readTitle, pal.Foam, info.Read, func(r messages.Receipt) string {
		s := receiptTime(r.Read, now)
		if r.Played > 0 {
			s = "played " + receiptTime(r.Played, now)
		}
		return s
	})
	section("✓✓ Delivered to", pal.Subtle, info.Delivered, func(r messages.Receipt) string { return receiptTime(r.Delivered, now) })
	section("✓ Not delivered yet", pal.Muted, info.Waiting, func(messages.Receipt) string { return "" })
	if len(info.Read)+len(info.Delivered)+len(info.Waiting) == 0 {
		lines = append(lines, styleMuted.Render("No receipts yet."))
	}
	if info.Group && len(info.Read) > 0 && (len(info.Delivered) > 0 || len(info.Waiting) > 0) {
		lines = append(lines, styleMuted.Render("The ticks turn blue when everyone has read it, as on the phone."))
	}
	return lines
}

func (m Model) renderMessageInfo(width, height int) string {
	lines := m.messageInfoLines(width - 2)
	hint := styleMuted.Render("j/k scroll · esc close")
	body := lines
	if s := min(m.minfo.scroll, max(len(lines)-(height-2), 0)); s > 0 {
		body = lines[s:]
	}
	out := append(fitLines(strings.Join(body, "\n"), height-2), "", hint)
	return lipgloss.NewStyle().PaddingLeft(2).Width(width).Height(height).MaxHeight(height).Render(strings.Join(out, "\n"))
}
