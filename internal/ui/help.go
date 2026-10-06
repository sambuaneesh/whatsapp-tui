package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// helpSections is every key, grouped by where it works. Keep it in sync
// with the handlers (and the README).
var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Palette (like VS Code)", [][2]string{
		{"ctrl+p", "go to any chat or contact (fuzzy)"},
		{"ctrl+p enter", "back to the previous chat"},
		{"F1 · ctrl+shift+p", "every command, by name"},
		{"> · #", "in ctrl+p: commands · search messages"},
		{"enter · alt+enter", "open · open beside (split view)"},
		{"ctrl+f · F3 shift+F3", "find in chat · next / previous"},
		{"ctrl+shift+f", "search all chats"},
	}},
	{"Chat list", [][2]string{
		{"j k · gg G", "move · top / bottom"},
		{"ctrl+d ctrl+u", "half page down / up"},
		{"enter / l", "open chat"},
		{"backspace / q", "back · close the window (keeps running)"},
		{"/", "filter chats & contacts · enter opens · esc clears"},
		{"S", "search messages in ALL chats"},
		{"u", "only unread chats"},
		{"e", "done: mark read + archive (archive: back to inbox)"},
		{"U · J", "mark unread / read · open the next unread chat"},
		{"I", "activity: mentions, replies, reactions to you"},
		{"v", "open beside the current chat (split view)"},
		{"A", "archived chats (or the Archived row)"},
		{"K", "chat info: picture, description"},
		{"V", "profile picture, full screen"},
		{"d then enter", "delete the chat"},
		{"P", "pin the chat to the top (or unpin)"},
		{"M", "notifications: all → popup → sound → off"},
	}},
	{"In a chat", [][2]string{
		{"j k · gg G", "scroll · top / bottom"},
		{"ctrl+d ctrl+u", "half page"},
		{"pgdown pgup", "full page (ctrl+b up)"},
		{"h / l / tab", "chat list / messages"},
		{"i / enter", "write a message"},
		{"v", "select messages, starting from what's on screen"},
		{"/ · n N", "search this chat · older / newer"},
		{"S", "search all chats"},
		{"@", "jump to messages that mention you"},
		{"e", "done: read + archive, then the next unread chat"},
		{"U · J", "mark unread (and leave) · next unread chat"},
		{"W · X", "swap split chats · close the split"},
		{"I", "activity: mentions, replies, reactions to you"},
		{"A", "archived chats in the sidebar"},
		{"a", "attach files (yazi)"},
		{"drag & drop", "drop files on the window to attach them"},
		{"s", "stickers & GIFs"},
		{"p / ctrl+v", "paste screenshot or text"},
		{"K", "chat info"},
		{"M", "notifications: all → popup → sound → off"},
		{"ctrl+x", "drop attachment / cancel reply or edit"},
		{"backspace / q", "back to the list"},
	}},
	{"Insert (typing)", [][2]string{
		{"enter", "send"},
		{"shift+enter", "new line (alt+enter, ctrl+j)"},
		{"ctrl+v", "paste image or text"},
		{"ctrl+a", "select all (type to replace, ctrl+c copy, ctrl+x cut)"},
		{"ctrl+← ctrl+→", "jump a word"},
		{"ctrl+backspace", "delete a word"},
		{"@ (groups)", "mention: tab pick, ctrl+n/p move"},
		{"ctrl+x", "drop attachment / cancel reply or edit"},
		{"esc", "back to normal mode"},
	}},
	{"Visual (v)", [][2]string{
		{"j k · gg G", "select another message"},
		{"V", "select several (j/k extend): y f d s act on all"},
		{"enter", "reply"},
		{"p", "reply privately (groups)"},
		{"r", "react: 1–6, x remove, + or a name: any emoji"},
		{"w", "who reacted (x removes yours)"},
		{"P", "pin for everyone, 7 days (again: unpin)"},
		{"e", "edit your message (first 15 min)"},
		{"f", "forward (space picks several)"},
		{"y", "copy text / image"},
		{"s", "save (download) media"},
		{"d", "delete: enter for me, e for everyone"},
		{"space", "view photo/sticker/GIF full screen, play video/voice note"},
		{"o", "open media, or the message's link"},
		{"R", "retry a message that failed"},
		{"/ · n N", "search · older / newer"},
		{"esc", "done"},
	}},
	{"Search", [][2]string{
		{"/ (list)", "chat names"},
		{"/ (chat)", "this chat, whole history"},
		{"S · :search <text>", "every chat"},
		{"ctrl+n ctrl+p", "older / newer while typing"},
		{"enter", "select the match (visual)"},
		{"n N", "older / newer match"},
		{"esc esc", "cancel · clear highlights"},
	}},
	{"Trays & panels", [][2]string{
		{"sticker tray (s)", "hjkl move · tab stickers/GIFs"},
		{"", "enter send · n new from file"},
		{"", "p paste as sticker · esc close"},
		{"forward (f)", "type to filter · space pick"},
		{"", "ctrl+n/p move · enter send"},
		{"all-chat search (S)", "j/k move · enter open · / edit"},
		{"info (K)", "j/k members · o open picture"},
		{"picture (V)", "o open in viewer · esc close"},
		{"viewer (space)", "o open in its app · s save · esc"},
		{"emoji grid (r +)", "type a name · arrows · enter/click · esc"},
		{"help (?)", "j/k scroll · esc close"},
		{"mouse", "click a chat · wheel scrolls"},
		{"", "click a link, picture or voice note to open it"},
		{"", "click a quote: jump to the original"},
		{"", "double-click a message to reply"},
		{"", "right-click a message to react"},
		{"", "click reactions to see who reacted"},
		{"", "click 🔔 in the status bar: notifications"},
		{"", "middle- or ctrl+click a chat: open beside"},
		{"", "click ✕ on the right pane: close the split"},
		{"", "click the 📌 bar: jump to the pinned message"},
	}},
	{"Commands", [][2]string{
		{":q", "close the window (keeps running)"},
		{":q!", "quit for real"},
		{":search <text>", "search all chats"},
		{":attach [path…]", "attach (no path: yazi)"},
		{":sticker [path]", "sticker tray / make one"},
		{":gif [path]", "GIF tray / make one"},
		{":download-dir [dir]", "show / set download folder"},
		{":notify [mode]", "notifications: all, popup, sound, off"},
		{":later <when>", "send what you wrote then (9am, in 2h…)"},
		{":snooze <when>", "archive the chat until then"},
		{":nudge <when>", "remind you if no reply by then"},
		{":scheduled", "what's scheduled (x cancels)"},
		{":pin · :unpin", "pin the chat to the top"},
		{":archive-chat · :unarchive", "archive (stays unread) · back to inbox"},
		{":resync", "archive/pins/mutes: match the phone again"},
		{":mute [8h|1w] · :unmute", "mute the chat (always without a time)"},
		{":pinned", "jump to the pinned messages"},
		{":split <name> · :close", "open a chat beside · close it"},
		{":activity · I", "mentions, replies and reactions to you"},
		{":private [on|off]", "read without blue ticks (U / :read marks read)"},
		{":read", "mark the open chat read"},
		{":info", "chat info"},
		{":unread", "only unread chats"},
		{":archive :inbox", "switch chat list"},
		{":backlog", "fetch older messages"},
		{":logout", "unlink this device"},
		{":help / ?", "this screen"},
	}},
}

// helpLines lays the sections out in as many columns as fit, and returns
// the lines (the screen scrolls them when they don't fit).
func (m Model) helpLines(width int) []string {
	keyW, descW := 20, 32
	colW := keyW + descW + 3
	keyStyle := lipgloss.NewStyle().Foreground(colorWarm).Width(keyW)
	title := lipgloss.NewStyle().Foreground(pal.Iris).Bold(true)

	var blocks [][]string
	for _, s := range helpSections {
		lines := []string{title.Render(s.title)}
		for _, k := range s.keys {
			lines = append(lines, keyStyle.Render(ansi.Truncate(k[0], keyW-1, "…"))+
				styleDim.Render(ansi.Truncate(k[1], descW, "…")))
		}
		blocks = append(blocks, append(lines, ""))
	}

	// fill columns top to bottom, keeping sections in order and the
	// tallest column as short as possible
	ncols := max(1, min(len(blocks), (width-4)/colW))
	sizes := make([]int, len(blocks))
	for i, b := range blocks {
		sizes[i] = len(b)
	}
	cols := make([][]string, ncols)
	for i, c := range balancedSplit(sizes, ncols) {
		cols[c] = append(cols[c], blocks[i]...)
	}
	var rendered []string
	for _, col := range cols {
		rendered = append(rendered, lipgloss.NewStyle().Width(colW).Render(strings.Join(col, "\n")))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	var legend []string
	for _, st := range []struct {
		status int
		label  string
	}{
		{messages.StatusPending, "sending"}, {messages.StatusSent, "sent"},
		{messages.StatusDelivered, "delivered"}, {messages.StatusRead, "read"},
		{messages.StatusPlayed, "played"}, {messages.StatusFailed, "not sent"},
	} {
		legend = append(legend, statusMark(st.status)+" "+styleDim.Render(st.label))
	}
	status := title.Render("Your messages") + "   " + strings.Join(legend, styleMuted.Render("  ·  "))
	if m.readReceiptsOff {
		status += "\n" + styleMuted.Render("Your read receipts are off in WhatsApp, so it doesn't send you others' either:\n"+
			"one-to-one chats stop at delivered. Groups and played voice notes still show.")
	}
	return strings.Split(lipgloss.JoinVertical(lipgloss.Left, body, status), "\n")
}

func (m Model) renderHelp(width, height int) string {
	lines := m.helpLines(width)
	room := helpRoom(height)
	footer := "press ? or esc to close"
	if len(lines) > room {
		maxScroll := len(lines) - room
		scroll := min(m.helpScroll, maxScroll)
		lines = lines[scroll : scroll+room]
		footer = "j/k scroll · " + footer
		if scroll < maxScroll {
			footer = "↓ more · " + footer
		}
	}
	body := lipgloss.JoinVertical(lipgloss.Left, strings.Join(lines, "\n"), "", styleDim.Render(footer))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top,
		lipgloss.NewStyle().PaddingTop(1).Render(body))
}

// helpRoom is how many help lines fit: minus the top padding, the blank
// line and the footer.
func helpRoom(height int) int { return max(height-3, 1) }

// helpMaxScroll is how far the help screen can scroll.
func (m Model) helpMaxScroll() int {
	return max(len(m.helpLines(m.width))-helpRoom(m.mainHeight()), 0)
}

// balancedSplit puts items (of the given heights, in order) into k columns
// so the tallest column is as short as possible; it returns each item's
// column.
func balancedSplit(sizes []int, k int) []int {
	n := len(sizes)
	sum := make([]int, n+1)
	for i, s := range sizes {
		sum[i+1] = sum[i] + s
	}
	// best[c][i]: the tallest column when the first i items fill c columns
	const inf = 1 << 30
	best := make([][]int, k+1)
	cut := make([][]int, k+1)
	for c := range best {
		best[c], cut[c] = make([]int, n+1), make([]int, n+1)
		for i := range best[c] {
			best[c][i] = inf
		}
	}
	best[0][0] = 0
	for c := 1; c <= k; c++ {
		for i := 0; i <= n; i++ {
			for j := 0; j <= i; j++ { // column c holds items j..i-1
				if best[c-1][j] == inf {
					continue
				}
				if h := max(best[c-1][j], sum[i]-sum[j]); h < best[c][i] {
					best[c][i], cut[c][i] = h, j
				}
			}
		}
	}
	col := make([]int, n)
	for c, i := k, n; c > 0; c-- {
		j := cut[c][i]
		for x := j; x < i; x++ {
			col[x] = c - 1
		}
		i = j
	}
	return col
}
