package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// msgSpan records which content lines a message occupies in the viewport.
type msgSpan struct {
	idx        int       // index into Model.msgs
	id         string    // that message's id when drawn (msgs can change since)
	start, end int       // first and last line (inclusive)
	quote      lineRange // the quoted message, for clicking through to it
	media      lineRange // the picture, for clicking to view it
	reacts     lineRange // the reactions, for clicking to see who reacted
}

// lineRange is n content lines from start; n == 0 means none.
type lineRange struct{ start, n int }

func (r lineRange) has(line int) bool { return r.n > 0 && line >= r.start && line < r.start+r.n }

// offset moves the range down by d lines.
func (r lineRange) offset(d int) lineRange {
	if r.n == 0 {
		return r
	}
	return lineRange{r.start + d, r.n}
}

// bubbleParts locates parts of a bubble, in lines from its top.
type bubbleParts struct{ quote, media, reacts lineRange }

// Size limits for inline media, in cells.
const (
	imageMaxCols, imageMaxRows     = 40, 14
	stickerMaxCols, stickerMaxRows = 16, 7
)

// tagLabels turns the backend's "[IMAGE]"-style markers into friendlier text.
var tagLabels = []struct{ tag, label string }{
	{"[IMAGE]", "📷 Photo"},
	{"[STICKER]", "✨ Sticker"},
	{"[GIF]", "🎞 GIF"},
	{"[VIDEO]", "🎬 Video"},
	{"[VOICE NOTE]", "🎤 Voice note"},
	{"[AUDIO]", "🎵 Audio"},
	{"[DOCUMENT]", "📄"},
	{"[CONTACT]", "👤"},
	{"[LOCATION]", "📍"},
	{"[LIVE LOCATION]", "📍 Live location"},
	{"[POLL]", "📊 Poll:"},
	{"[EVENT]", "📅"},
	{"[CONTACTS]", "👥"},
	{"[REACTION]", "Reacted"},
}

// splitTag returns the leading media tag of text (if any) and the rest.
func splitTag(text string) (label, rest string) {
	for _, t := range tagLabels {
		if strings.HasPrefix(text, t.tag) {
			return t.label, strings.TrimSpace(strings.TrimPrefix(text, t.tag))
		}
	}
	return "", text
}

// prettyTags replaces a leading media tag with its label.
func prettyTags(text string) string {
	label, rest := splitTag(text)
	switch {
	case label == "":
		return text
	case rest == "":
		return label
	}
	return label + " " + rest
}

func (m Model) bubbleMaxInner(width int) int {
	w := width*3/4 - 4 // border + padding
	if w < 16 {
		w = 16
	}
	return w
}

// mediaCells is the cell size used to show a message's media.
func (m Model) mediaCells(meta messages.MediaMeta, maxInner int) (cols, rows int) {
	maxC, maxR := imageMaxCols, imageMaxRows
	if meta.Type == messages.MediaSticker {
		maxC, maxR = stickerMaxCols, stickerMaxRows
	}
	if maxC > maxInner {
		maxC = maxInner
	}
	cw, ch := 8, 16
	if m.img != nil {
		cw, ch = m.img.cellW, m.img.cellH
	}
	return termimg.FitCells(meta.Width, meta.Height, maxC, maxR, cw, ch)
}

// mediaBlock renders a message's image area, or "" when it has none to show.
func (m Model) mediaBlock(msg messages.Message, maxInner int) (block string, meta messages.MediaMeta, ok bool) {
	if !m.img.enabled() {
		return "", meta, false
	}
	meta, ok = msg.MediaMeta()
	if !ok {
		return "", meta, false
	}
	cols, rows := m.mediaCells(meta, maxInner)
	e := m.img.get(imgKey{imgMessage, msg.Id, cols, rows})
	switch {
	case e != nil && e.state == imgReady:
		return e.text, meta, true
	case e != nil && e.state == imgFailed:
		return "", meta, false
	}
	// Reserve the space while loading so the layout doesn't jump.
	lines := make([]string, rows)
	for i := range lines {
		lines[i] = strings.Repeat(" ", cols)
	}
	lines[(rows-1)/2] = lipgloss.PlaceHorizontal(cols, lipgloss.Center, styleMuted.Render("loading…"))
	return strings.Join(lines, "\n"), meta, true
}

func (m Model) renderChatPane(width, height int) string {
	title := ""
	if m.current != nil && m.inPersonal() {
		title = " " + styleTitle.Render(chatName(m.current)) + styleDim.Render("  "+m.personalKind())
	} else if m.current != nil {
		kind := "contact"
		if isGroup(m.current.JID) {
			kind = "group"
		}
		title = " " + m.avatarCells(m.current, avatarSmallCols, avatarSmallRows, false)[0] + " " +
			styleTitle.Render(chatName(m.current)) + styleDim.Render("  "+kind)
		if !m.chatIndexed(m.current.JID) {
			title += styleMuted.Render("  · not indexed for search")
		}
		if m.readReceiptsOff && kind == "contact" && !m.selfChat {
			// explains why messages here stop at delivered
			title += styleMuted.Render("  ·  your read receipts are off: ") +
				statusMark(messages.StatusDelivered) + styleMuted.Render(" is as far as it goes")
		}
	}
	parts := []string{m.renderHeader(title, width, m.focus == paneMessages)}
	if m.pinRows() > 0 {
		parts = append(parts, m.renderPinBar(width))
	}
	parts = append(parts, m.vp.View()) // already padded to the pane
	if m.replyTo != nil || m.editing != nil {
		parts = append(parts, m.renderReplyBar(width))
	}
	if len(m.attachments) > 0 {
		parts = append(parts, m.renderAttachment(width))
	}
	if m.mention != nil {
		parts = append(parts, m.renderMentionPicker(width))
	}
	parts = append(parts, m.renderCompose(width))
	// each part is drawn at the pane's width; only the height is fitted
	return strings.Join(fitLines(strings.Join(parts, "\n"), height), "\n")
}

// renderCompose draws the input as a rounded box with the text centred on
// its middle line.
func (m Model) renderCompose(width int) string {
	border := pal.Muted
	if m.mode == modeInsert {
		border = colorWarm
	}
	view := m.compose.View()
	if m.selectAll {
		view = m.renderSelectedCompose()
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(0, 1).Width(width - 2).
		Render(view)
}

func (m Model) renderMessages(width int) ([]string, []msgSpan) {
	if m.current == nil {
		return nil, nil
	}
	if len(m.msgs) == 0 {
		return strings.Split("\n"+styleDim.PaddingLeft(2).Width(width).Render(
			"No messages yet. Fetching recent history from your phone… (press i to write one)"), "\n"), nil
	}
	group := isGroup(m.current.JID)
	now := time.Now()
	maxInner := m.bubbleMaxInner(width)
	cache := m.bubbles
	defer cache.done()
	m.img.beginPass()

	out := make([]string, 0, len(m.msgs)*6)
	add := func(s string) { out = append(out, s) }
	// centred labels (dates, the unread line), cached like bubbles
	centred := func(s string) {
		key := cache.keyer().str("centred").str(s).int(width).sum()
		if b, ok := cache.get(key); ok {
			add(b.lines[0])
			return
		}
		line := lipgloss.PlaceHorizontal(width, lipgloss.Center, s)
		cache.put(key, cachedBubble{lines: []string{line}})
		add(line)
	}
	spans := make([]msgSpan, 0, len(m.msgs))
	var lastDay time.Time
	lastSender := ""
	for i, msg := range m.msgs {
		t := toTime(int64(msg.Timestamp))
		if lastDay.IsZero() || !sameDay(t, lastDay) {
			add("")
			centred(styleDate.Render(" " + dayLabel(t, now) + " "))
			lastDay = t
			lastSender = ""
		}
		sender := msg.ContactId
		if msg.FromMe {
			sender = "me"
		}
		// Blank lines between messages (Settings: message spacing), one more
		// when the sender changes.
		for g := 0; g < look.gap; g++ {
			add("")
		}
		if sender != lastSender && lastSender != "" && look.senderGap {
			add("")
		}
		if m.unreadID != "" && msg.Id == m.unreadID {
			label := fmt.Sprintf(" %d unread messages ", m.unreadCount)
			if m.unreadCount == 1 {
				label = " 1 unread message "
			}
			add("")
			centred(lipgloss.NewStyle().Foreground(colorWarm).Render("── ") + styleUnread.Bold(true).Render(label) +
				lipgloss.NewStyle().Foreground(colorWarm).Render(" ──"))
		}
		start := len(out)
		selected := m.mode == modeVisual && (i == m.sel || m.inRange(i))
		showSender := group && sender != lastSender
		key := m.bubbleKey(msg, showSender, selected, maxInner, width)
		b, ok := cache.get(key)
		if !ok {
			bubble, parts := m.renderBubble(msg, showSender, selected, maxInner, width, t)
			b = cachedBubble{lines: strings.Split(bubble, "\n"), parts: parts}
			cache.put(key, b)
		}
		out = append(out, b.lines...)
		spans = append(spans, msgSpan{idx: i, id: msg.Id, start: start, end: len(out) - 1,
			quote: b.parts.quote.offset(start), media: b.parts.media.offset(start), reacts: b.parts.reacts.offset(start)})
		lastSender = sender
	}
	add("")
	return out, spans
}

// quoteBlock renders the message a reply quotes, WhatsApp-style.
func (m Model) quoteBlock(msg messages.Message, maxInner int) []string {
	if msg.QuotedID == "" {
		return nil
	}
	who := m.quotedSenderName(msg)
	bar := lipgloss.NewStyle().Foreground(senderColor(msg.QuotedSender))
	text := strings.ReplaceAll(prettyTags(msg.QuotedText), "\n", " ")
	if text == "" {
		text = "message"
	}
	return []string{
		bar.Render("▎") + senderStyle(msg.QuotedSender).Render(ansi.Truncate(who, maxInner-1, "…")),
		bar.Render("▎") + styleDim.Render(ansi.Truncate(text, maxInner-1, "…")),
	}
}

// quotedSenderName names a quoted message's author using names already on
// screen.
func (m Model) quotedSenderName(msg messages.Message) string {
	q := msg.QuotedSender
	if q == "" {
		if msg.FromMe {
			return chatName(m.current)
		}
		return "You"
	}
	for _, x := range m.msgs {
		if x.Id == msg.QuotedID {
			return m.senderName(x)
		}
	}
	user := strings.Split(q, "@")[0]
	for _, x := range m.msgs {
		if x.FromMe && strings.Split(x.ContactId, "@")[0] == user {
			return "You"
		}
		if !x.FromMe && strings.Split(x.ContactId, "@")[0] == user && x.ContactShort != "" {
			return x.ContactShort
		}
	}
	if m.current != nil && !isGroup(m.current.JID) && strings.HasPrefix(m.current.JID, user+"@") {
		return chatName(m.current)
	}
	return "+" + user
}

// reactionChip is one emoji's part of the line under a bubble: "👍 2".
type reactionChip struct {
	emoji, label string
	mine         bool // you reacted with it
}

// reactionChips groups reactions by emoji, in the order first used.
func reactionChips(rs []messages.Reaction) []reactionChip {
	counts := map[string]int{}
	mine := map[string]bool{}
	var order []string
	for _, r := range rs {
		if counts[r.Emoji] == 0 {
			order = append(order, r.Emoji)
		}
		counts[r.Emoji]++
		if r.Sender == "" {
			mine[r.Emoji] = true
		}
	}
	chips := make([]reactionChip, len(order))
	for i, e := range order {
		label := e
		if counts[e] > 1 {
			label += " " + fmt.Sprint(counts[e])
		}
		chips[i] = reactionChip{emoji: e, label: label, mine: mine[e]}
	}
	return chips
}

// reactionGap separates the chips on the line.
const reactionGap = "  "

// reactionLine summarises reactions like "👍 2  ❤️".
func reactionLine(rs []messages.Reaction) string {
	var parts []string
	for _, c := range reactionChips(rs) {
		if c.mine {
			parts = append(parts, lipgloss.NewStyle().Foreground(colorWarm).Render(c.label))
		} else {
			parts = append(parts, styleDim.Render(c.label))
		}
	}
	return strings.Join(parts, reactionGap)
}

func (m Model) renderBubble(msg messages.Message, showSender, selected bool, maxInner, width int, t time.Time) (string, bubbleParts) {
	border := pal.Muted
	stampStyle := styleStampThem
	if msg.FromMe {
		border, stampStyle = pal.Iris, styleStampMe
	}
	failed := msg.FromMe && msg.Status == messages.StatusFailed
	mentionsYou := !msg.FromMe && mentionsYou(msg)
	if failed || mentionsYou {
		border = pal.Love
	}
	if msg.Deleted != 0 {
		border = pal.HighlightMed // faded: no longer there for the others
	}
	if selected {
		border = pal.Rose
	}

	block, meta, hasMedia := m.mediaBlock(msg, maxInner)
	label, text := splitTag(strings.TrimRight(msg.Text, "\n"))
	if !hasMedia && label != "" {
		text = prettyTags(msg.Text)
	}

	var lines []string
	if showSender && !msg.FromMe {
		name := msg.ContactShort
		if name == "" {
			name = msg.ContactName
		}
		lines = append(lines, senderStyle(msg.ContactId).Render(ansi.Truncate(name, maxInner, "…")))
	}
	if mentionsYou {
		lines = append(lines, styleMention.Render("@ mentioned you"))
	}
	if msg.Forwarded {
		lines = append(lines, styleMuted.Italic(true).Render("↪ Forwarded"))
	}
	if msg.Deleted != 0 && !isDeletedNote(msg) {
		// deleted for everyone, but we'd seen it: shown, marked
		who := "🚫 deleted by " + m.senderName(msg)
		if msg.Deleted == messages.DeletedByYou || msg.FromMe {
			who = "🚫 you deleted this for everyone"
		}
		lines = append(lines, styleErr.Italic(true).Render(ansi.Truncate(who, maxInner, "…")))
	}
	var parts bubbleParts
	quote := m.quoteBlock(msg, maxInner)
	parts.quote = lineRange{len(lines), len(quote)}
	lines = append(lines, quote...)
	if hasMedia {
		media := strings.Split(block, "\n")
		parts.media = lineRange{len(lines), len(media)}
		lines = append(lines, media...)
		switch meta.Type {
		case messages.MediaGIF:
			text = strings.TrimSpace("GIF  " + text)
		case messages.MediaVideo:
			dur := ""
			if meta.Seconds > 0 {
				dur = fmt.Sprintf(" %d:%02d", meta.Seconds/60, meta.Seconds%60)
			}
			text = strings.TrimSpace("▶ Video" + dur + "  " + text)
		}
	}
	if isDeletedNote(msg) {
		lines = append(lines, styleMuted.Italic(true).Render(msg.Text))
		text = ""
	}
	if playableAudio(msg) && text != "" {
		text = "▶ " + text // click (or space) plays it
		parts.media = lineRange{len(lines), 1}
	}
	if text != "" {
		if m.search != nil && matchesQuery(text, m.search.query) {
			// plain, so search highlights line up with what was typed
			for _, l := range strings.Split(ansi.Wrap(text, maxInner, ""), "\n") {
				lines = append(lines, m.highlight(l, selected, styleBase))
			}
		} else {
			lines = append(lines, formatText(text, maxInner, styleBase, msg.Mentions)...)
		}
	}

	stamp := stampStyle.Render(t.Format("15:04"))
	if msg.Pinned {
		stamp = "📌 " + stamp
	}
	if msg.Edited {
		stamp = stampStyle.Render("edited ") + stamp
	}
	if msg.FromMe {
		if mark := statusMark(msg.Status); mark != "" {
			stamp += " " + mark
		}
	}
	inner := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > inner {
			inner = w
		}
	}
	// Put the time on the last text line when it fits, like WhatsApp does;
	// otherwise on its own line.
	sw := lipgloss.Width(stamp)
	if n := len(lines); n > 0 && text != "" && lipgloss.Width(lines[n-1])+2+sw <= maxInner {
		last := lines[n-1]
		if w := lipgloss.Width(last) + 2 + sw; w > inner {
			inner = w
		}
		lines[n-1] = last + strings.Repeat(" ", inner-lipgloss.Width(last)-sw) + stamp
	} else {
		if sw > inner {
			inner = sw
		}
		lines = append(lines, lipgloss.PlaceHorizontal(inner, lipgloss.Right, stamp))
	}

	if failed {
		lines = append(lines, styleErr.Render("not sent · v then R to retry"))
		if w := lipgloss.Width(lines[len(lines)-1]); w > inner {
			inner = w
		}
	}
	// The frame is drawn by hand (lipgloss re-measures every line several
	// times for a border, padding and alignment; this measures each once).
	// Every line of blk is exactly blkW cells wide.
	var blk []string
	blkW := 0
	if hasMedia && meta.Type == messages.MediaSticker && !selected {
		// stickers float without a bubble, like on the phone
		blk, blkW = make([]string, len(lines)), inner
		for i, l := range lines {
			blk[i] = padRight(l, inner)
		}
	} else {
		parts.quote, parts.media = parts.quote.offset(1), parts.media.offset(1) // below the top border
		bc := lipgloss.NewStyle().Foreground(border)
		ch := look.corners // Settings: bubble corners
		side := bc.Render(ch[5])
		edge := strings.Repeat(ch[4], inner+2)
		blk = make([]string, 0, len(lines)+2)
		blk = append(blk, bc.Render(ch[0]+edge+ch[1]))
		for _, l := range lines {
			blk = append(blk, side+" "+padRight(l, inner)+" "+side)
		}
		blk = append(blk, bc.Render(ch[2]+edge+ch[3]))
		blkW = inner + 4
	}
	if r := reactionLine(msg.Reactions); r != "" {
		// under the bubble, on its side
		rl := " " + r + " "
		rw := ansi.StringWidth(rl)
		if rw > blkW {
			for i, l := range blk {
				if msg.FromMe {
					blk[i] = strings.Repeat(" ", rw-blkW) + l
				} else {
					blk[i] = l + strings.Repeat(" ", rw-blkW)
				}
			}
			blkW = rw
		}
		parts.reacts = lineRange{len(blk), 1}
		if msg.FromMe {
			rl = strings.Repeat(" ", blkW-rw) + rl
		} else {
			rl += strings.Repeat(" ", blkW-rw)
		}
		blk = append(blk, rl)
	}
	// place it: yours on the right, theirs on the left; a marker in the
	// gutter makes the selection (or a mention of you) easy to spot
	var prefix, suffix string
	switch {
	case selected || mentionsYou:
		gutterColor := pal.Love
		if selected {
			gutterColor = pal.Rose
		}
		gutter := lipgloss.NewStyle().Foreground(gutterColor).Render("▌")
		if msg.FromMe {
			prefix, suffix = strings.Repeat(" ", max(width-2-blkW, 0)), gutter
		} else {
			prefix = gutter
		}
	case msg.FromMe:
		prefix = strings.Repeat(" ", max(width-1-blkW, 0))
	default:
		prefix = " "
	}
	for i, l := range blk {
		blk[i] = prefix + l + suffix
	}
	return strings.Join(blk, "\n"), parts
}

// padRight pads a line with spaces to w cells.
func padRight(line string, w int) string {
	if n := ansi.StringWidth(line); n < w {
		return line + strings.Repeat(" ", w-n)
	}
	return line
}

// statusMark shows how far one of your messages got, as two small blocks
// that fill up, coloured like WhatsApp's ticks (grey until read, then blue):
//
//	□□ muted   sending
//	■□ subtle  sent (reached WhatsApp)
//	■■ subtle  delivered
//	■■ foam    read (iris: voice note or video played)
//	✕  love    not sent
func statusMark(status int) string {
	fg := func(c lipgloss.Color, s string) string { return lipgloss.NewStyle().Foreground(c).Render(s) }
	switch status {
	case messages.StatusPending:
		return fg(pal.Muted, "□□")
	case messages.StatusSent:
		return fg(pal.Subtle, "■") + fg(pal.Muted, "□")
	case messages.StatusDelivered:
		return fg(pal.Subtle, "■■")
	case messages.StatusRead:
		return fg(pal.Foam, "■■")
	case messages.StatusPlayed:
		return fg(pal.Iris, "■■")
	case messages.StatusFailed:
		return fg(pal.Love, "✕")
	}
	return ""
}

// mentionsYou reports whether a message @mentions you.
func mentionsYou(msg messages.Message) bool {
	for _, name := range msg.Mentions {
		if name == "You" {
			return true
		}
	}
	return false
}

// isDeletedNote reports a message that was deleted for everyone.
func isDeletedNote(msg messages.Message) bool {
	return len(msg.Media) == 0 && (msg.Text == "🚫 This message was deleted" || msg.Text == "🚫 You deleted this message")
}
