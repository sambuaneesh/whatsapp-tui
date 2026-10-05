package ui

import (
	"context"
	"regexp"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Mouse support is deliberately small: click a chat to open it, a link,
// picture or quoted message to follow it, double-click a message to reply,
// and the wheel scrolls what's under the pointer. Everything else is
// keyboard.

// wheelLines is how far one wheel notch scrolls the messages.
const wheelLines = 3

// doubleClickTime is how close together two clicks on a message must be
// to reply to it.
const doubleClickTime = 400 * time.Millisecond

// clickNow is the clock for double clicks (swappable for tests).
var clickNow = time.Now

// leave closes the window: it detaches when running in the background,
// else quits.
func (m Model) leave() tea.Cmd {
	if m.detach == nil {
		return tea.Quit
	}
	d := m.detach
	return func() tea.Msg {
		d()
		return nil
	}
}

// overlayOpen reports screens that cover the chat list and messages.
func (m Model) overlayOpen() bool {
	return m.showHelp || m.emo != nil || m.reactors != nil || m.sched != nil || m.act != nil || m.info != nil || m.global != nil || m.fwd != nil || m.stk != nil || m.pic != nil || m.view != nil || m.qr != ""
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.emo != nil:
		return m.mouseEmoji(msg)
	case m.reactors != nil:
		return m.mouseReactors(msg)
	case m.sched != nil:
		return m.mouseScheduled(msg)
	case m.act != nil:
		return m.mouseActivity(msg)
	case m.overlayOpen():
		return m, nil
	}
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && m.picker &&
		m.mode == modeVisual && msg.Y == m.height-1 {
		return m.clickPicker(msg.X) // the quick-reaction bar
	}
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && m.notifyBadgeAt(msg.X, msg.Y) {
		return m.cycleNotifyMode()
	}
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && m.scheduledBadgeAt(msg.X, msg.Y) {
		return m, m.openScheduled()
	}
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && m.privateBadgeAt(msg.X, msg.Y) {
		return m.setPrivateReading(false)
	}
	overList := m.screen == screenList || msg.X < m.sidebarW
	if m.split != nil && m.screen == screenChat && msg.X > m.sidebarW+m.rightWidth() {
		// the chat beside: a click makes it the one you write in
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.Y >= headerRows && msg.Y < m.mainHeight() {
			return m.swapSplit()
		}
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		dir := 1
		if msg.Button == tea.MouseButtonWheelUp {
			dir = -1
		}
		if overList {
			m.scrollList(dir)
		} else if m.current != nil {
			if dir < 0 {
				m.vp.LineUp(wheelLines)
			} else {
				m.vp.LineDown(wheelLines)
			}
		}
		return m, nil
	case tea.MouseButtonRight:
		if msg.Action == tea.MouseActionPress && !overList {
			return m.rightClickMessage(msg.X, msg.Y)
		}
		return m, nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if !overList {
			// a click on a link in the messages opens it
			if url := m.linkAt(msg.X, msg.Y); url != "" {
				return m, func() tea.Msg { return actionDoneMsg{ok: "Opened " + url, err: openURL(url)} }
			}
			return m.clickMessage(msg.X, msg.Y)
		}
		// entries start below the list header, fullItemHeight rows each
		row := msg.Y - headerRows
		if row < 0 {
			return m, nil
		}
		i := m.listOffset + row/fullItemHeight
		if i >= m.listLen() {
			return m, nil
		}
		if m.mode == modeVisual || m.mode == modeInsert {
			m.mode = modeNormal
			m.compose.Blur()
		}
		m.cursor = i
		return m, m.openSelected()
	}
	return m, nil
}

// scrollList moves the chat list one entry, keeping the cursor on screen.
func (m *Model) scrollList(dir int) {
	n := m.listLen()
	rows := m.listRows()
	m.listOffset = min(max(m.listOffset+dir, 0), max(n-rows, 0))
	if m.cursor < m.listOffset {
		m.cursor = m.listOffset
	}
	if m.cursor >= m.listOffset+rows {
		m.cursor = m.listOffset + rows - 1
	}
}

// clickMessage handles a click on a message: its picture opens the viewer,
// its quote jumps to the message it replies to, and a second click on the
// same message replies to it.
func (m Model) clickMessage(x, y int) (tea.Model, tea.Cmd) {
	sp, line, ok := m.messageAt(x, y)
	if !ok || m.picker || m.confirm != nil ||
		(m.mode != modeNormal && m.mode != modeInsert && m.mode != modeVisual) {
		return m, nil
	}
	msg := m.msgs[sp.idx]
	switch {
	case sp.media.has(line):
		m.lastClickID = ""
		return m, m.viewMedia(msg)
	case sp.quote.has(line):
		m.lastClickID = ""
		return m.jumpToQuoted(msg)
	case sp.reacts.has(line):
		m.lastClickID = ""
		col := x - (m.sidebarW + 1)
		return m.openReactors(msg, chipAt(m.msgLines[line], msg.Reactions, col))
	}
	now := clickNow()
	if m.lastClickID == msg.Id && now.Sub(m.lastClickAt) <= doubleClickTime {
		m.lastClickID = ""
		if m.editing != nil {
			m.notice, m.noticeErr = "finish the edit first (enter saves, esc cancels)", true
			return m, nil
		}
		return m.startReply(msg)
	}
	m.lastClickID, m.lastClickAt = msg.Id, now
	return m, nil
}

// messageAt returns the message drawn at screen cell (x, y) and the content
// line clicked; clicks in the empty space beside a bubble miss.
func (m Model) messageAt(x, y int) (sp msgSpan, line int, ok bool) {
	if m.screen != screenChat || m.current == nil {
		return sp, 0, false
	}
	row := y - headerRows
	col := x - (m.sidebarW + 1) // sidebar + divider
	if row < 0 || row >= m.vp.Height || col < 0 {
		return sp, 0, false
	}
	line = m.vp.YOffset + row
	if line >= len(m.msgLines) {
		return sp, 0, false
	}
	if from, to := inkColumns(m.msgLines[line]); col < from || col >= to {
		return sp, 0, false
	}
	for _, s := range m.msgSpans {
		if line >= s.start && line <= s.end && s.idx < len(m.msgs) && m.msgs[s.idx].Id == s.id {
			return s, line, true
		}
	}
	return sp, 0, false
}

// inkColumns is the span of display columns from the first to just past
// the last non-blank cell of a rendered line.
func inkColumns(line string) (from, to int) {
	from, pos := -1, 0
	for _, r := range ansi.Strip(line) {
		w := ansi.StringWidth(string(r))
		if r != ' ' && w > 0 {
			if from < 0 {
				from = pos
			}
			to = pos + w
		}
		pos += w
	}
	if from < 0 {
		return 0, 0
	}
	return from, to
}

// quoteJumpMsg brings the history around a quoted message that wasn't
// loaded.
type quoteJumpMsg struct {
	chat, id string
	msgs     []messages.Message
	err      error
}

// jumpToQuoted selects the message msg replies to, loading older history
// when it isn't on screen yet.
func (m Model) jumpToQuoted(msg messages.Message) (tea.Model, tea.Cmd) {
	for i, x := range m.msgs {
		if x.Id == msg.QuotedID {
			m.selectJumped(i)
			return m, nil
		}
	}
	if m.globalSearcher == nil {
		m.notice, m.noticeErr = "the replied-to message isn't loaded", true
		return m, nil
	}
	m.notice, m.noticeErr = "Finding the replied-to message…", false
	return m, m.loadAndSelect(m.current.JID, msg.QuotedID)
}

// loadAndSelect loads a chat's history around a message, then selects it
// (see applyQuoteJump).
func (m Model) loadAndSelect(chat, id string) tea.Cmd {
	gs := m.globalSearcher
	if gs == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := gs.LoadAround(ctx, chat, id)
		return quoteJumpMsg{chat: chat, id: id, msgs: msgs, err: err}
	}
}

// applyQuoteJump shows the loaded history with the quoted message selected,
// keeping the newer messages already loaded below it.
func (m Model) applyQuoteJump(r quoteJumpMsg) (tea.Model, tea.Cmd) {
	if m.current == nil || r.chat != m.current.JID {
		return m, nil // left the chat meanwhile
	}
	idx := -1
	for i, x := range r.msgs {
		if x.Id == r.id {
			idx = i
		}
	}
	if r.err != nil || idx < 0 {
		m.notice, m.noticeErr = "the replied-to message isn't on this device", true
		return m, nil
	}
	msgs := r.msgs
	last := msgs[len(msgs)-1].Timestamp
	for _, x := range m.msgs {
		if x.Timestamp > last {
			msgs = append(msgs, x)
		}
	}
	m.msgs, m.keepOlder = msgs, true
	m.notice = ""
	m.findMatches()
	m.selectJumped(idx)
	return m, nil
}

// olderThan returns the loaded messages from before a new screen, when a
// jump to a quoted message loaded them (otherwise none: the screen
// replaces what was loaded).
func (m Model) olderThan(screen []messages.Message) []messages.Message {
	if !m.keepOlder || len(screen) == 0 {
		return nil
	}
	var out []messages.Message
	for _, x := range m.msgs {
		if x.Timestamp < screen[0].Timestamp {
			out = append(out, x)
		}
	}
	return out
}

// selectJumped selects message i in visual mode, in the middle of the view.
func (m *Model) selectJumped(i int) {
	if m.mode == modeInsert {
		m.compose.Blur()
	}
	m.mode, m.sel, m.picker = modeVisual, i, false
	m.refreshMessages(false)
	for _, sp := range m.msgSpans {
		if sp.idx == i {
			h := sp.end - sp.start + 1
			m.vp.SetYOffset(max(sp.start-max(m.vp.Height-h, 0)/2, 0))
			return
		}
	}
}

// osc8 matches a hyperlink start ("\x1b]8;params;url" + ST) or end (empty url).
var osc8Seq = regexp.MustCompile("^\x1b\\]8;[^;]*;([^\x1b\x07]*)(?:\x1b\\\\|\x07)")

// linkAt returns the URL of the link drawn at screen cell (x, y) in the
// message pane, or "".
func (m Model) linkAt(x, y int) string {
	if m.screen != screenChat || m.current == nil {
		return ""
	}
	row := y - headerRows
	col := x - (m.sidebarW + 1) // sidebar + divider
	if row < 0 || row >= m.vp.Height || col < 0 {
		return ""
	}
	i := m.vp.YOffset + row
	if i >= len(m.msgLines) {
		return ""
	}
	return urlAtColumn(m.msgLines[i], col)
}

// urlAtColumn walks a rendered line and returns the hyperlink covering the
// given display column.
func urlAtColumn(line string, col int) string {
	url, pos := "", 0
	for len(line) > 0 {
		if loc := osc8Seq.FindStringSubmatchIndex(line); loc != nil {
			url = line[loc[2]:loc[3]]
			line = line[loc[1]:]
			continue
		}
		if line[0] == 0x1b { // other escape codes take no space
			n := ansiSeqLen(line)
			line = line[n:]
			continue
		}
		r, size := utf8.DecodeRuneInString(line)
		w := ansi.StringWidth(string(r))
		if col >= pos && col < pos+w {
			return url
		}
		pos += w
		line = line[size:]
	}
	return ""
}

// ansiSeqLen is the length of the escape sequence at the start of s.
func ansiSeqLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[': // CSI: ends with a byte in 0x40..0x7e
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
	case ']', '_', 'P': // OSC/APC/DCS: end with BEL or ESC \
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
	}
	return 2
}
