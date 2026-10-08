package ui

import (
	"context"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/emoji"
	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Reactions: the quick bar (r in visual mode, or right-click a message),
// the emoji grid for any other emoji, and the list of who reacted (w, or
// click the reactions under a bubble).

// ---------- quick bar ----------

// openPicker shows the quick-reaction bar for the selected message.
func (m *Model) openPicker() tea.Cmd {
	m.picker = true
	m.cmdline.Prompt = "react: "
	m.cmdline.SetValue("")
	return m.cmdline.Focus()
}

func (m *Model) closePicker() {
	m.picker = false
	m.cmdline.Blur()
	m.cmdline.Prompt = ":"
}

// react sends emoji as your reaction to target ("" removes yours).
func (m Model) react(target messages.Message, emoji string) (tea.Model, tea.Cmd) {
	m.closePicker()
	m.emo = nil
	if m.actions == nil {
		return m, nil
	}
	a := m.actions
	label := "Reacted " + emoji
	if emoji == "" {
		label = "Reaction removed"
	}
	return m, m.action(label, func(ctx context.Context) (string, error) {
		return "", a.SendReaction(ctx, target, emoji)
	})
}

func (m Model) handlePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	sel, _ := m.selected()
	empty := m.cmdline.Value() == ""
	switch {
	case key == "esc":
		m.closePicker()
		return m, nil
	case empty && len(key) == 1 && key >= "1" && key <= "6":
		return m.react(sel, quickReactions[key[0]-'1'])
	case empty && key == "x":
		return m.react(sel, "")
	case empty && (key == "tab" || key == "+" || key == "/"):
		m.closePicker()
		m.openEmojiGrid(sel, "")
		return m, nil
	case key == "enter":
		v := strings.TrimSpace(m.cmdline.Value())
		switch {
		case v == "":
			return m, nil
		case isWords(v): // a name: look it up
			m.closePicker()
			m.openEmojiGrid(sel, v)
			return m, nil
		}
		return m.react(sel, v) // an emoji typed or pasted
	case empty && msg.Type == tea.KeyRunes && !msg.Paste && isWords(string(msg.Runes)):
		// typing a name searches all emoji
		m.closePicker()
		m.openEmojiGrid(sel, string(msg.Runes))
		return m, nil
	}
	var cmd tea.Cmd
	m.cmdline, cmd = m.cmdline.Update(msg)
	return m, cmd
}

// isWords reports text made of letters, digits, spaces and dashes: an
// emoji's name rather than an emoji.
func isWords(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-') {
			return false
		}
	}
	return s != ""
}

// pickerChip is one clickable part of the quick bar.
type pickerChip struct {
	label string // as drawn
	emoji string // to react with
	kind  int    // chipReact, chipRemove, chipMore
}

const (
	chipReact = iota
	chipRemove
	chipMore
)

// pickerGap separates the quick bar's chips.
const pickerGap = "  "

func (m Model) pickerChips() []pickerChip {
	var chips []pickerChip
	for i, e := range quickReactions {
		chips = append(chips, pickerChip{label: styleFilter.Render(string(rune('1'+i))) + " " + e, emoji: e})
	}
	return append(chips,
		pickerChip{label: styleFilter.Render("x") + styleDim.Render(" remove"), kind: chipRemove},
		pickerChip{label: styleFilter.Render("+") + styleDim.Render(" more"), kind: chipMore})
}

func (m Model) renderPicker() string {
	var parts []string
	for _, c := range m.pickerChips() {
		parts = append(parts, c.label)
	}
	return strings.Join(parts, pickerGap) + styleDim.Render("  ·  or type a name: ") + m.cmdline.View()
}

// clickPicker acts on the quick-bar chip at column x.
func (m Model) clickPicker(x int) (tea.Model, tea.Cmd) {
	sel, ok := m.selected()
	if !ok {
		return m, nil
	}
	pos := 0
	for _, c := range m.pickerChips() {
		w := lipgloss.Width(c.label)
		if x >= pos && x < pos+w {
			switch c.kind {
			case chipRemove:
				return m.react(sel, "")
			case chipMore:
				m.closePicker()
				m.openEmojiGrid(sel, "")
				return m, nil
			}
			return m.react(sel, c.emoji)
		}
		pos += w + lipgloss.Width(pickerGap)
	}
	return m, nil
}

// rightClickMessage selects the message under the pointer and opens the
// quick-reaction bar for it.
func (m Model) rightClickMessage(x, y int) (tea.Model, tea.Cmd) {
	sp, _, ok := m.messageAt(x, y)
	if !ok || m.confirm != nil || (m.mode != modeNormal && m.mode != modeInsert && m.mode != modeVisual) {
		return m, nil
	}
	if m.mode == modeInsert {
		m.compose.Blur()
	}
	// the first right-click selects it (ready for r, f, y, d, i…); a
	// second one on the same message opens the reactions
	again := m.mode == modeVisual && m.sel == sp.idx && m.rangeFrom == noRange
	m.mode, m.sel = modeVisual, sp.idx
	m.focus = paneMessages
	m.refreshMessages(false)
	if again && !m.picker {
		return m, m.openPicker()
	}
	m.picker = false
	return m, nil
}

// ---------- emoji grid ----------

// emojiPicker is the full-screen grid of every emoji, searchable by name.
type emojiPicker struct {
	target messages.Message
	query  string
	items  []emoji.Emoji
	cursor int
	top    int // first row shown
}

// emojiCell is the width of one emoji in the grid.
const emojiCell = 4

// Grid layout: title, search, blank, then the grid from this row (and
// two lines below it).
const emojiGridTop = 3

func (m *Model) openEmojiGrid(target messages.Message, query string) {
	m.emo = &emojiPicker{target: target}
	m.setEmojiQuery(query)
}

func (m *Model) setEmojiQuery(q string) {
	e := m.emo
	e.query, e.items, e.cursor, e.top = q, emoji.Search(q), 0, 0
}

// emojiGeom is how many emoji fit in a row, and how many rows show.
func (m Model) emojiGeom() (cols, rows int) {
	return max((m.width-4)/emojiCell, 1), max(m.mainHeight()-emojiGridTop-3, 1)
}

// moveEmoji moves the highlight, scrolling to keep it in view.
func (m *Model) moveEmoji(delta int) {
	e := m.emo
	if len(e.items) == 0 {
		return
	}
	e.cursor = min(max(e.cursor+delta, 0), len(e.items)-1)
	cols, rows := m.emojiGeom()
	row := e.cursor / cols
	if row < e.top {
		e.top = row
	}
	if row >= e.top+rows {
		e.top = row - rows + 1
	}
}

func (m Model) handleEmoji(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.emo
	cols, rows := m.emojiGeom()
	switch msg.String() {
	case "esc":
		m.emo = nil
		return m, nil
	case "enter":
		if len(e.items) > 0 {
			return m.react(e.target, e.items[e.cursor].Char)
		}
		if q := strings.TrimSpace(e.query); q != "" && !isWords(q) {
			return m.react(e.target, q) // pasted an emoji the list doesn't have
		}
		return m, nil
	case "left":
		m.moveEmoji(-1)
	case "right":
		m.moveEmoji(1)
	case "up", "ctrl+p":
		m.moveEmoji(-cols)
	case "down", "ctrl+n":
		m.moveEmoji(cols)
	case "pgup":
		m.moveEmoji(-cols * rows)
	case "pgdown":
		m.moveEmoji(cols * rows)
	case "home":
		m.moveEmoji(-len(e.items))
	case "end":
		m.moveEmoji(len(e.items))
	case "backspace":
		if r := []rune(e.query); len(r) > 0 {
			m.setEmojiQuery(string(r[:len(r)-1]))
		}
	case "ctrl+u":
		m.setEmojiQuery("")
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.setEmojiQuery(e.query + string(msg.Runes))
		}
	}
	return m, nil
}

func (m Model) mouseEmoji(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	e := m.emo
	cols, rows := m.emojiGeom()
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		dir := 1
		if msg.Button == tea.MouseButtonWheelUp {
			dir = -1
		}
		last := max((len(e.items)-1)/cols-rows+1, 0)
		e.top = min(max(e.top+dir, 0), last)
		e.cursor = min(max(e.cursor, e.top*cols), min((e.top+rows)*cols, len(e.items))-1)
		e.cursor = max(e.cursor, 0)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		r, c := msg.Y-emojiGridTop, (msg.X-2)/emojiCell
		if msg.X < 2 || r < 0 || r >= rows || c >= cols {
			return m, nil
		}
		if i := (e.top+r)*cols + c; i < len(e.items) {
			return m.react(e.target, e.items[i].Char)
		}
	case tea.MouseButtonRight:
		m.emo = nil
	}
	return m, nil
}

func (m Model) renderEmojiGrid(width, height int) string {
	e := m.emo
	cols, rows := m.emojiGeom()
	title := styleTitle.Render("React to "+m.senderName(e.target)) + styleDim.Render(": "+
		ansi.Truncate(strings.ReplaceAll(prettyTags(e.target.Text), "\n", " "), max(width-30, 10), "…"))
	search := lipgloss.NewStyle().Foreground(colorWarm).Render("search: ") + e.query + styleMuted.Render("▏")
	if e.query == "" {
		search += styleMuted.Render("type a name: heart, fire, laugh…")
	}
	lines := []string{title, search, ""}
	sel := lipgloss.NewStyle().Background(pal.HighlightMed)
	for r := e.top; r < e.top+rows; r++ {
		var row strings.Builder
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= len(e.items) {
				break
			}
			cell := " " + e.items[i].Char
			cell += strings.Repeat(" ", max(emojiCell-lipgloss.Width(cell), 0))
			if i == e.cursor {
				cell = sel.Render(cell)
			}
			row.WriteString(cell)
		}
		lines = append(lines, row.String())
	}
	foot := styleErr.Render("no emoji match")
	if len(e.items) > 0 {
		it := e.items[e.cursor]
		foot = it.Char + "  " + styleBase.Render(it.Name) + styleMuted.Render("  ·  "+it.Group)
	}
	lines = append(lines, "", foot,
		styleMuted.Render("arrows move · enter or click react · type to search · backspace · esc back"))
	return lipgloss.NewStyle().PaddingLeft(2).Width(width).Height(height).MaxHeight(height).
		Render(strings.Join(lines, "\n"))
}

// ---------- who reacted ----------

// reactorsView lists who reacted to a message.
type reactorsView struct {
	msgID  string
	cursor int
	names  map[string]string // sender JID -> name, filled in as they load
}

type reactorNamesMsg struct {
	msgID string
	names map[string]string
}

// reactorRow is one person's reaction.
type reactorRow struct {
	emoji, name string
	mine        bool
}

// Rows of the list start below the title, the message and a blank line.
const reactorsTop = 3

// openReactors shows who reacted to msg, starting at emoji (if any).
func (m Model) openReactors(msg messages.Message, emoji string) (tea.Model, tea.Cmd) {
	m.reactors = &reactorsView{msgID: msg.Id, names: map[string]string{}}
	for i, r := range m.reactorRows() {
		if r.emoji == emoji {
			m.reactors.cursor = i
			break
		}
	}
	// names not on screen come from the contacts
	var unknown []string
	for _, r := range msg.Reactions {
		if r.Sender != "" {
			if name := m.localName(r.Sender); name != "" {
				m.reactors.names[r.Sender] = name
			} else {
				unknown = append(unknown, r.Sender)
			}
		}
	}
	if len(unknown) == 0 || m.actions == nil {
		return m, nil
	}
	a, id := m.actions, msg.Id
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		names := map[string]string{}
		for _, jid := range unknown {
			names[jid] = a.ChatName(ctx, jid)
		}
		return reactorNamesMsg{msgID: id, names: names}
	}
}

func (m Model) applyReactorNames(r reactorNamesMsg) (tea.Model, tea.Cmd) {
	if m.reactors != nil && m.reactors.msgID == r.msgID {
		for k, v := range r.names {
			m.reactors.names[k] = v
		}
	}
	return m, nil
}

// localName finds a name for a sender among the loaded messages and the
// group's members.
func (m Model) localName(jid string) string {
	user := strings.Split(jid, "@")[0]
	for _, x := range m.msgs {
		if !x.FromMe && strings.Split(x.ContactId, "@")[0] == user && x.ContactShort != "" {
			return x.ContactShort
		}
	}
	if m.current != nil {
		for _, mem := range m.members[m.current.JID] {
			if strings.Split(mem.JID, "@")[0] == user && mem.Name != "" {
				return mem.Name
			}
		}
		if !isGroup(m.current.JID) && strings.Split(m.current.JID, "@")[0] == user {
			return chatName(m.current)
		}
	}
	return ""
}

// reactedMessage is the message the list is about, as loaded now (its
// reactions change as people react).
func (m Model) reactedMessage() (messages.Message, bool) {
	for _, x := range m.msgs {
		if m.reactors != nil && x.Id == m.reactors.msgID {
			return x, true
		}
	}
	return messages.Message{}, false
}

// reactorRows lists the reactions by emoji (as under the bubble), yours
// first within each.
func (m Model) reactorRows() []reactorRow {
	msg, ok := m.reactedMessage()
	if !ok {
		return nil
	}
	var rows []reactorRow
	for _, c := range reactionChips(msg.Reactions) {
		if c.mine {
			rows = append(rows, reactorRow{emoji: c.emoji, name: "You", mine: true})
		}
		for _, r := range msg.Reactions {
			if r.Emoji != c.emoji || r.Sender == "" {
				continue
			}
			name := m.reactors.names[r.Sender]
			if name == "" {
				name = "+" + strings.Split(r.Sender, "@")[0]
			}
			rows = append(rows, reactorRow{emoji: c.emoji, name: name})
		}
	}
	return rows
}

func (m Model) handleReactors(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.reactorRows()
	v := m.reactors
	switch msg.String() {
	case "esc", "q", "w":
		m.reactors = nil
	case "j", "down":
		v.cursor = min(v.cursor+1, max(len(rows)-1, 0))
	case "k", "up":
		v.cursor = max(v.cursor-1, 0)
	case "x", "enter", "d":
		if v.cursor < len(rows) && rows[v.cursor].mine {
			return m.removeMine()
		}
		m.notice, m.noticeErr = "you can only remove your own reaction", true
	case "r":
		// react from here: the quick bar on this message
		target, ok := m.reactedMessage()
		m.reactors = nil
		if !ok {
			return m, nil
		}
		for i, x := range m.msgs {
			if x.Id == target.Id {
				m.mode, m.sel = modeVisual, i
			}
		}
		m.refreshMessages(false)
		return m, m.openPicker()
	}
	return m, nil
}

// removeMine takes back your reaction to the listed message.
func (m Model) removeMine() (tea.Model, tea.Cmd) {
	target, ok := m.reactedMessage()
	if !ok {
		return m, nil
	}
	next, cmd := m.react(target, "")
	nm := next.(Model)
	if nm.reactors != nil {
		nm.reactors.cursor = 0
	}
	return nm, cmd
}

func (m Model) mouseReactors(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Button == tea.MouseButtonRight:
		m.reactors = nil
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		rows := m.reactorRows()
		i := msg.Y - reactorsTop
		if i < 0 || i >= len(rows) {
			return m, nil
		}
		m.reactors.cursor = i
		if rows[i].mine { // click yours to take it back
			return m.removeMine()
		}
	}
	return m, nil
}

func (m Model) renderReactors(width, height int) string {
	msg, _ := m.reactedMessage()
	rows := m.reactorRows()
	text := strings.ReplaceAll(prettyTags(msg.Text), "\n", " ")
	lines := []string{
		styleTitle.Render("Reactions") + styleDim.Render(" · "+m.senderName(msg)),
		styleDim.Render(ansi.Truncate(text, max(width-6, 10), "…")),
		"",
	}
	if len(rows) == 0 {
		lines = append(lines, styleMuted.Render("No reactions yet."))
	}
	cur := min(m.reactors.cursor, max(len(rows)-1, 0))
	for i, r := range rows {
		line := " " + r.emoji + "   " + styleBase.Render(r.name)
		if r.mine {
			line += styleMuted.Render("   x or click to remove")
		}
		if i == cur {
			line = lipgloss.NewStyle().Background(pal.HighlightMed).Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", styleMuted.Render("j/k move · x remove yours · r react · esc close"))
	return lipgloss.NewStyle().PaddingLeft(2).Width(width).Height(height).MaxHeight(height).
		Render(strings.Join(lines, "\n"))
}

// chipAt is the emoji of the reaction chip at column col of a rendered
// reaction line, or "".
func chipAt(line string, rs []messages.Reaction, col int) string {
	chips := reactionChips(rs)
	if len(chips) == 0 {
		return ""
	}
	plain := ansi.Strip(line)
	i := strings.Index(plain, chips[0].label)
	if i < 0 {
		return ""
	}
	pos := ansi.StringWidth(plain[:i])
	for _, c := range chips {
		w := ansi.StringWidth(c.label)
		if col >= pos && col < pos+w {
			return c.emoji
		}
		pos += w + len(reactionGap)
	}
	return ""
}
