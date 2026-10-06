package ui

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/personal"
)

// The palette, after VS Code's: ctrl+p is Quick Open (go to any chat or
// contact by fuzzy name, recent chats first), F1 or ctrl+shift+p the
// Command Palette (">" in Quick Open), and "#" searches messages in all
// chats. It floats over the screen; enter opens, alt+enter opens beside.

const (
	maxPaletteRows    = 12
	maxPaletteResults = 200
	maxRecent         = 50
)

type paletteState struct {
	query  []rune
	cursor int
	offset int
	beside bool // a chat opens beside the current one (split view)

	// picking a chat or list for something (sending a list, moving a task)
	pickTitle string
	only      func(c *messages.Conversation) bool
	onPick    func(m Model, c *messages.Conversation) (tea.Model, tea.Cmd)
	actions   []palItem // a list of things to do (the local model's suggestions…)

	cands []palCand // chats and contacts, gathered when opened
	items []palItem
	// the last chat query and the candidates it matched: typing more
	// only searches those
	lastQ   string
	lastIdx []int
}

type palCand struct {
	c             *messages.Conversation
	name, lowered []rune
	contact       bool // no chat yet
	recent        int  // place in the recent chats (0 = not recent)
	digits        string
}

type palItem struct {
	action *palAction
	pitem  *personal.Item // "@": one of your tasks, notes or saved messages
	plist  string
	chat   *messages.Conversation
	cand   *palCand
	cmd    *command
	score  int
	pos    []int // matched runes, highlighted
	title  string
}

// recents remembers the chats you opened and the commands you ran, most
// recent first; Quick Open and the Command Palette list them first.
type recents struct{ chats, cmds []string }

func pushRecent(list []string, id string) []string {
	out := []string{id}
	for _, x := range list {
		if x != id && len(out) < maxRecent {
			out = append(out, x)
		}
	}
	return out
}

// openPalette opens the palette with a starting query ("" for chats, ">"
// for commands).
func (m *Model) openPalette(query string) {
	m.qo = &paletteState{query: []rune(query)}
	m.qo.cands = m.paletteCands()
	m.refilter()
}

func (m *Model) closePalette() { m.qo = nil }

// paletteCands are the chats and contacts to search, made once per
// change of the chat list (thousands of names, each lowercased).
func (m Model) paletteCands() []palCand {
	key := fmt.Sprintf("%d|%d|%p", m.listVer, len(m.allChats), m.allChats)
	if m.memo != nil && m.memo.palKey == key {
		return m.memo.palCands
	}
	seen := make(map[string]bool, len(m.allChats)+len(m.chats))
	out := make([]palCand, 0, len(m.allChats)+len(m.chats))
	add := func(c *messages.Conversation, contact bool) {
		if seen[c.JID] {
			return
		}
		seen[c.JID] = true
		name, low := lowerRunes(chatName(c))
		user, _, _ := strings.Cut(c.JID, "@")
		out = append(out, palCand{c: c, name: name, lowered: low, contact: contact, digits: user})
	}
	for _, c := range m.chats {
		add(c, false)
	}
	for _, c := range m.allChats {
		add(c, c.LastMsgTime == 0)
	}
	if m.memo != nil {
		m.memo.palKey, m.memo.palCands = key, out
	}
	return out
}

// recentRank is each recent chat's place (1 = most recent).
func (m Model) recentRank() map[string]int {
	r := map[string]int{}
	if m.recent != nil {
		for i, j := range m.recent.chats {
			r[j] = i + 1
		}
	}
	return r
}

// paletteMode is what the query asks for: chats, ">" commands, "#"
// messages.
func (p *paletteState) mode() byte {
	if p.onPick != nil || p.actions != nil {
		return 0
	}
	if len(p.query) > 0 && (p.query[0] == '>' || p.query[0] == '#' || p.query[0] == '@') {
		return byte(p.query[0])
	}
	return 0
}

// text is the query without its mode prefix, lowercased and trimmed.
func (p *paletteState) text() string {
	q := string(p.query)
	if p.mode() != 0 {
		q = q[1:]
	}
	return strings.ToLower(strings.TrimSpace(q))
}

func (m *Model) refilter() {
	p := m.qo
	if p == nil {
		return
	}
	p.cursor, p.offset = 0, 0
	switch p.mode() {
	case '>':
		p.items = m.matchCommands(p.text())
	case '#':
		p.items = nil
	case '@':
		p.items = m.matchPersonal(p.text())
	default:
		if p.actions != nil {
			p.items = matchActions(p.actions, p.text())
			for p.cursor < len(p.items)-1 && p.items[p.cursor].action.info && p.items[p.cursor].action.run == nil && hasRunnable(p.items) {
				p.cursor++
			}
			return
		}
		p.items = m.matchChats(p)
		if len(p.items) > 0 && p.text() == "" && m.current != nil && m.screen == screenChat &&
			p.items[0].chat != nil && p.items[0].chat.JID == m.current.JID && len(p.items) > 1 {
			p.cursor = 1 // like alt+tab: enter goes back to the previous chat
		}
	}
}

func (m Model) matchChats(p *paletteState) []palItem {
	q := p.text()
	if q == "" {
		p.lastQ, p.lastIdx = "", nil
		// recent chats, then the list as it's sorted; contacts only when
		// searched for
		rank := m.recentRank()
		var items []palItem
		for i := range p.cands {
			if c := &p.cands[i]; !c.contact && (p.only == nil || p.only(c.c)) {
				items = append(items, palItem{chat: c.c, cand: c, score: -rank[c.c.JID]})
			}
		}
		sort.SliceStable(items, func(a, b int) bool {
			ra, rb := -items[a].score, -items[b].score
			if (ra > 0) != (rb > 0) {
				return ra > 0
			}
			return ra < rb
		})
		for i := range items {
			items[i].score = 0
		}
		return items[:min(len(items), maxPaletteResults)]
	}
	qr := []rune(strings.Join(strings.Fields(q), " "))
	digits := ""
	if d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, q); len(d) >= 4 && len(d) == len(strings.NewReplacer(" ", "", "+", "").Replace(q)) {
		digits = d
	}
	// typing more only narrows what the shorter query found
	var idx []int
	// (not for numbers: a number matches only from its fourth digit)
	if p.lastQ != "" && strings.HasPrefix(q, p.lastQ) && p.lastIdx != nil && digits == "" {
		idx = p.lastIdx
	} else {
		idx = make([]int, len(p.cands))
		for i := range idx {
			idx[i] = i
		}
	}
	rank := m.recentRank()
	items := make([]palItem, 0, len(idx))
	kept := make([]int, 0, len(idx))
	for _, i := range idx {
		c := &p.cands[i]
		if p.only != nil && !p.only(c.c) {
			continue
		}
		score, ok := fuzzyScore(qr, c.lowered, c.name)
		if !ok && digits != "" && strings.Contains(c.digits, digits) {
			score, ok = scoreMatch*len(digits), true
		}
		if !ok {
			continue
		}
		kept = append(kept, i)
		if r := rank[c.c.JID]; r > 0 {
			score += max(12-r, 2)
		}
		if !c.contact {
			score += 6
		}
		items = append(items, palItem{chat: c.c, cand: c, score: score})
	}
	p.lastQ, p.lastIdx = q, kept
	slices.SortFunc(items, func(a, b palItem) int {
		if a.score != b.score {
			return b.score - a.score
		}
		if a.chat.LastMsgTime != b.chat.LastMsgTime {
			return cmp.Compare(b.chat.LastMsgTime, a.chat.LastMsgTime)
		}
		return strings.Compare(a.chat.JID, b.chat.JID)
	})
	return items[:min(len(items), maxPaletteResults)]
}

func (m Model) matchCommands(q string) []palItem {
	cmds := m.availableCommands()
	rank := map[string]int{}
	if m.recent != nil {
		for i, id := range m.recent.cmds {
			rank[id] = i + 1
		}
	}
	qr := []rune(q)
	var items []palItem
	for _, c := range cmds {
		name, low := lowerRunes(c.title)
		score, pos, ok := fuzzyMatch(qr, low, name)
		if !ok {
			continue
		}
		if q == "" && strings.HasPrefix(c.title, "Selected") {
			score += 500 // what you've selected comes first, then recent
		}
		if r := rank[c.id]; r > 0 {
			if q == "" {
				score += 1000 - r // recently used first, like VS Code
			} else {
				score += max(10-r, 2)
			}
		}
		items = append(items, palItem{cmd: c, score: score, pos: pos, title: c.title})
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].score > items[b].score })
	return items
}

// chosenChat is the highlighted chat, if a chat is highlighted.
func (p *paletteState) chosenChat() *messages.Conversation {
	if p == nil || p.cursor < 0 || p.cursor >= len(p.items) {
		return nil
	}
	return p.items[p.cursor].chat
}

func (p *paletteState) move(d int) {
	n := len(p.items)
	if n == 0 {
		return
	}
	p.cursor = (p.cursor + d%n + n) % n // wraps, like VS Code
}

func (p *paletteState) page(d int) {
	if len(p.items) == 0 {
		return
	}
	p.cursor = min(max(p.cursor+d, 0), len(p.items)-1)
}

func (m Model) handlePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.qo
	switch k := msg.String(); k {
	case "esc", "ctrl+c", "ctrl+g":
		m.closePalette()
		return m, nil
	case "down", "ctrl+n", "ctrl+j", "tab":
		p.move(1)
	case "ctrl+p":
		if p.mode() == '>' {
			p.move(-1)
		} else {
			p.move(1) // ctrl+p again: the next chat, as in VS Code
		}
	case "up", "ctrl+k", "shift+tab":
		p.move(-1)
	case "pgdown", "ctrl+d":
		p.page(maxPaletteRows)
	case "pgup":
		p.page(-maxPaletteRows)
	case "enter":
		return m.choosePalette(p.beside)
	case "alt+enter", "ctrl+\\", "ctrl+right":
		return m.choosePalette(true)
	case "f1", "f13":
		p.query = []rune(">")
		m.refilter()
	case "backspace", "ctrl+h":
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			m.refilter()
		}
	case "ctrl+w", "alt+backspace", "ctrl+backspace":
		s := strings.TrimRight(string(p.query), " ")
		if i := strings.LastIndexAny(s, " >#"); i >= 0 {
			s = s[:i+1]
		} else {
			s = ""
		}
		p.query = []rune(s)
		m.refilter()
	case "ctrl+u":
		if p.mode() != 0 {
			p.query = p.query[:1]
		} else {
			p.query = nil
		}
		m.refilter()
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			r := msg.Runes
			if msg.Type == tea.KeySpace {
				r = []rune{' '}
			}
			p.query = append(p.query, []rune(strings.ReplaceAll(string(r), "\n", " "))...)
			m.refilter()
		}
	}
	return m, nil
}

// choosePalette opens the highlighted chat (beside: in split view), runs
// the highlighted command, or searches messages.
func (m Model) choosePalette(beside bool) (tea.Model, tea.Cmd) {
	p := m.qo
	if p.mode() == '#' {
		q := strings.TrimSpace(string(p.query[1:]))
		m.closePalette()
		next, cmd := m.runCommand("search " + q)
		return next, cmd
	}
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return m, nil
	}
	it := p.items[p.cursor]
	if a := it.action; a != nil {
		if a.run == nil {
			return m, nil
		}
		if a.keep {
			nm, note := a.run(m)
			nm.notice, nm.noticeErr = note, false
			// done with it: it leaves the list
			kept := p.actions[:0:0]
			for _, x := range p.actions {
				if x.action != a {
					kept = append(kept, x)
				}
			}
			p.actions = kept
			if !hasRunnable(kept) {
				nm.closePalette()
				return nm, nil
			}
			nm.qo = p
			nm.refilter()
			return nm, nil
		}
		m.closePalette()
		nm, note := a.run(m)
		nm.notice, nm.noticeErr = note, false
		return nm, nil
	}
	m.closePalette()
	if p.onPick != nil && it.chat != nil {
		return p.onPick(m, it.chat)
	}
	if it.pitem != nil {
		return m.openPersonalItem(*it.pitem)
	}
	if it.cmd != nil {
		if m.recent != nil {
			m.recent.cmds = pushRecent(m.recent.cmds, it.cmd.id)
		}
		return it.cmd.run(m)
	}
	c := it.chat
	// a chat opens over whatever panel was showing
	m.showHelp, m.info, m.act, m.sched, m.pic = false, nil, nil, nil, nil
	if m.global != nil {
		m.closeGlobalSearch()
	}
	if beside && inChat(m) {
		return m, m.openSplit(c)
	}
	m.showChatInList(c)
	return m, m.openChat(c)
}

// showChatInList moves the list's cursor to c, switching between inbox
// and archive if needed, so the sidebar shows where you are.
func (m *Model) showChatInList(c *messages.Conversation) {
	if c.LastMsgTime > 0 && c.IsArchived != m.archive && m.filter == "" {
		m.toggleArchive()
	}
	for i := 0; i < m.listLen(); i++ {
		if x, _ := m.itemAt(i); x != nil && x.JID == c.JID {
			m.cursor = i
			m.clampCursor()
			return
		}
	}
}

// paletteBox is where the palette is drawn: x, y and its width (with the
// border), and how many result rows show.
func (m Model) paletteBox() (x, y, w, rows int) {
	w = min(max(m.width*6/10, 64), m.width-2, 110)
	x = (m.width - w) / 2
	y = 1
	rows = max(min(maxPaletteRows, m.mainHeight()-7), 1)
	return
}

// paletteLines draws the palette: border, query, results and hints.
func (m Model) paletteLines() []string {
	p := m.qo
	_, _, w, rows := m.paletteBox()
	inner := w - 2
	bg := lipgloss.NewStyle().Background(colorBarBg)
	sel := lipgloss.NewStyle().Background(colorSelBg)
	border := lipgloss.NewStyle().Foreground(colorBorderFocus).Background(colorBarBg)
	fill := func(s string, st lipgloss.Style) string {
		pad := inner - ansi.StringWidth(s)
		if pad < 0 {
			return ansi.Truncate(s, inner, "…")
		}
		return s + st.Render(strings.Repeat(" ", pad))
	}

	title := " Go to chat "
	placeholder := "type a name or number · > commands · # search messages"
	switch {
	case p.mode() == '>':
		title, placeholder = " Commands ", "type to find a command"
	case p.mode() == '#':
		title, placeholder = " Search messages ", "type what to find in all chats"
	case p.mode() == '@':
		title, placeholder = " Your tasks, notes and saved ", "type to find them"
	case p.pickTitle != "":
		title, placeholder = " "+p.pickTitle+" ", "type a name"
	case p.beside:
		title = " Open beside "
	}
	top := border.Render("╭─") + bg.Foreground(pal.Iris).Bold(true).Render(title) +
		border.Render(strings.Repeat("─", max(inner-1-ansi.StringWidth(title), 0))+"╮")
	side := border.Render("│")
	out := []string{top}

	prompt := bg.Foreground(pal.Rose).Bold(true).Render(" › ")
	text := bg.Foreground(pal.Text).Render(string(p.query))
	cursor := lipgloss.NewStyle().Background(pal.Rose).Render(" ")
	line := prompt + text + cursor
	if p.text() == "" {
		line += bg.Foreground(pal.Muted).Italic(true).Render(" " + placeholder)
	}
	out = append(out, side+fill(line, bg)+side)
	out = append(out, side+bg.Foreground(colorBorder).Render(strings.Repeat("─", inner))+side)

	// keep the cursor in view
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
	now := time.Now()
	shown := 0
	switch {
	case p.mode() == '#':
		q := strings.TrimSpace(string(p.query[1:]))
		msg := "type something to search for"
		if q != "" {
			msg = fmt.Sprintf("enter: search all chats for “%s”", q)
		}
		out = append(out, side+fill(bg.Foreground(pal.Subtle).Render("  "+msg), bg)+side)
		shown = 1
	case len(p.items) == 0:
		msg := "no chat or contact matches"
		switch {
		case p.mode() == '>':
			msg = "no command matches"
		case p.mode() == '@' && p.text() == "":
			msg = "type to search your tasks, notes and saved messages"
		case p.mode() == '@':
			msg = "nothing of yours matches"
		}
		out = append(out, side+fill(bg.Foreground(pal.Muted).Render("  "+msg), bg)+side)
		shown = 1
	default:
		for i := p.offset; i < len(p.items) && i < p.offset+rows; i++ {
			st := bg
			if i == p.cursor {
				st = sel
			}
			out = append(out, side+fill(m.paletteRow(p.items[i], inner, st, i == p.cursor, now), st)+side)
			shown++
		}
	}
	for ; shown < min(rows, max(len(p.items), 1)); shown++ {
		out = append(out, side+fill("", bg)+side)
	}

	hint := "↑↓ move · enter open · alt+enter beside · esc close"
	if p.onPick != nil {
		hint = "↑↓ move · enter picks · esc cancels"
	}
	if p.actions != nil {
		hint = "↑↓ move · enter does it · esc closes"
	}
	switch p.mode() {
	case '>':
		hint = "↑↓ move · enter run · backspace: back to chats · esc close"
	case '#':
		hint = "enter search · esc close"
	}
	if n := len(p.items); n > rows && p.mode() != '#' {
		hint = fmt.Sprintf("%d–%d of %d · ", p.offset+1, min(p.offset+rows, n), n) + hint
	}
	out = append(out, side+fill(bg.Foreground(pal.Muted).Render(" "+hint), bg)+side)
	out = append(out, border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return out
}

// paletteRow draws one result: the name with the matched letters lit,
// and what it is on the right.
func (m Model) paletteRow(it palItem, width int, st lipgloss.Style, chosen bool, now time.Time) string {
	marker := st.Render("  ")
	if chosen {
		marker = st.Foreground(pal.Rose).Render("▌ ")
	}
	nameSt := st.Foreground(pal.Text)
	if a := it.action; a != nil {
		lst := nameSt
		if a.info {
			lst = st.Foreground(pal.Subtle)
		}
		if a.run != nil && !a.keep {
			lst = st.Foreground(pal.Foam).Bold(true)
		}
		return fitRow(marker+lst.Render(a.label), st.Foreground(pal.Muted).Render(a.right+" "), width, st)
	}
	if it.pitem != nil {
		x := it.pitem
		icon := "☐ "
		switch {
		case x.Done:
			icon = "☑ "
		case x.SrcMsg != "" && x.Due == 0:
			icon = "🔖 "
		case x.Body != "" && x.Due == 0:
			icon = "📄 "
		}
		left := marker + st.Render(icon) + nameSt.Render(x.Text)
		right := it.plist
		if d := dueLabel(*x, now); d != "" && !x.Done {
			right = d + " · " + right
		}
		return fitRow(left, st.Foreground(pal.Muted).Render(right+" "), width, st)
	}
	if it.cmd != nil {
		group, rest, ok := strings.Cut(it.title, ": ")
		left := marker
		off := 0
		if ok {
			left += st.Foreground(pal.Subtle).Render(group + ": ")
			off = len([]rune(group)) + 2
		} else {
			rest = it.title
		}
		left += highlightRunes([]rune(rest), shiftPos(it.pos, off), nameSt, st.Foreground(pal.Gold).Bold(true))
		right := st.Foreground(pal.Muted).Render(it.cmd.keys + " ")
		return fitRow(left, right, width, st)
	}
	c := it.chat
	if c.Unread > 0 {
		nameSt = nameSt.Bold(true)
	}
	var pos []int
	if q := []rune(strings.Join(strings.Fields(m.qo.text()), " ")); len(q) > 0 && it.cand != nil {
		_, pos, _ = fuzzyMatch(q, it.cand.lowered, it.cand.name)
	}
	left := marker + highlightRunes([]rune(chatName(c)), pos, nameSt, st.Foreground(pal.Gold).Bold(true))
	var tags []string
	switch {
	case it.cand != nil && it.cand.contact:
		tags = append(tags, "contact")
	case c.IsArchived:
		tags = append(tags, "archived")
	}
	if isGroup(c.JID) {
		tags = append(tags, "group")
	}
	if len(tags) > 0 {
		left += st.Foreground(pal.Muted).Render("  " + strings.Join(tags, " · "))
	}
	right := st.Foreground(pal.Subtle).Render(listTime(c.LastMsgTime, now) + " ")
	if c.Unread > 0 {
		right = styleBadge.Render(fmt.Sprint(c.Unread)) + st.Render(" ") + right
	}
	if m.current != nil && c.JID == m.current.JID && m.screen == screenChat {
		right = st.Foreground(pal.Rose).Render("open ") + right
	}
	return fitRow(left, right, width, st)
}

func shiftPos(pos []int, off int) []int {
	out := make([]int, 0, len(pos))
	for _, p := range pos {
		if p-off >= 0 {
			out = append(out, p-off)
		}
	}
	return out
}

// highlightRunes renders s with the runes at pos in hi, the rest in base.
func highlightRunes(s []rune, pos []int, base, hi lipgloss.Style) string {
	if len(pos) == 0 {
		return base.Render(string(s))
	}
	lit := make(map[int]bool, len(pos))
	for _, p := range pos {
		lit[p] = true
	}
	var b strings.Builder
	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || lit[i] != lit[start] {
			st := base
			if lit[start] {
				st = hi
			}
			b.WriteString(st.Render(string(s[start:i])))
			start = i
		}
	}
	return b.String()
}

// overlayPalette draws the palette over the frame's main area.
func (m Model) overlayPalette(main string) string {
	x, y, w, _ := m.paletteBox()
	lines := strings.Split(main, "\n")
	for i, l := range m.paletteLines() {
		row := y + i
		if row >= len(lines) {
			break
		}
		base := lines[row]
		left := ansi.Truncate(base, x, "")
		if lw := ansi.StringWidth(left); lw < x {
			left += strings.Repeat(" ", x-lw)
		}
		right := ansi.TruncateLeft(base, x+w, "")
		lines[row] = left + "\x1b[0m" + l + "\x1b[0m" + right
	}
	return strings.Join(lines, "\n")
}

// paletteClick handles a mouse click while the palette is open: a result
// opens, outside closes it.
func (m Model) paletteClick(mx, my int) (tea.Model, tea.Cmd) {
	x, y, w, rows := m.paletteBox()
	p := m.qo
	first := y + 3 // border, query, rule
	if mx < x || mx >= x+w || my < y || my > first+rows+1 {
		m.closePalette()
		return m, nil
	}
	if i := p.offset + my - first; my >= first && i >= 0 && i < len(p.items) && i < p.offset+rows {
		p.cursor = i
		return m.choosePalette(p.beside)
	}
	return m, nil
}

// globalKey handles the VS Code keys that work everywhere (outside
// typing): ctrl+p, F1 / ctrl+shift+p, ctrl+shift+f, ctrl+f, F3.
// ctrl+shift+p and ctrl+shift+f reach a terminal app only as F13 and F14
// (see the kitty.conf lines in USAGE.md).
func (m Model) globalKey(key string) (tea.Model, tea.Cmd, bool) {
	typing := m.mode == modeFilter || m.mode == modeChatSearch || m.mode == modeGlobalSearch ||
		m.mode == modeCommand || m.confirm != nil
	// panels with keys of their own (ctrl+p moves up in them)
	busy := typing || m.global != nil || m.fwd != nil || m.emo != nil || m.reactors != nil || m.stk != nil || m.view != nil
	switch key {
	case "ctrl+p":
		if busy || (m.mode == modeInsert && m.mention != nil) {
			return m, nil, false // ctrl+p means "previous" there
		}
		m.openPalette("")
		return m, nil, true
	case "f1", "f13":
		if typing || m.fwd != nil || m.emo != nil || m.reactors != nil || m.stk != nil || m.view != nil {
			return m, nil, false
		}
		m.openPalette(">")
		return m, nil, true
	case "f14":
		if m.confirm != nil {
			return m, nil, false
		}
		if m.global != nil {
			m.closeGlobalSearch()
		}
		m.mode = modeNormal
		return m, m.openGlobalSearch(), true
	case "ctrl+f":
		if typing || m.mode == modeInsert || m.overlayOpen() {
			return m, nil, false
		}
		if m.inPersonal() {
			m.openPalette("@") // find in your lists
			return m, nil, true
		}
		if inChat(m) {
			m.focus = paneMessages
			return m, m.startSearch(), true
		}
		m.openPalette("")
		return m, nil, true
	case "f3", "shift+f3":
		if !inChat(m) || m.search == nil || typing {
			return m, nil, false
		}
		if key == "f3" {
			m.nextMatch(-1)
		} else {
			m.nextMatch(1)
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) mousePalette(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		return m.paletteClick(msg.X, msg.Y)
	case msg.Button == tea.MouseButtonWheelDown:
		m.qo.move(1)
	case msg.Button == tea.MouseButtonWheelUp:
		m.qo.move(-1)
	}
	return m, nil
}

// openPicker opens the palette to pick a chat (or list) for something.
func (m *Model) openChatPicker(title string, only func(c *messages.Conversation) bool,
	onPick func(m Model, c *messages.Conversation) (tea.Model, tea.Cmd)) {
	m.qo = &paletteState{pickTitle: title, only: only, onPick: onPick}
	m.qo.cands = m.paletteCands()
	m.refilter()
}

// matchPersonal finds your tasks, notes and saved messages ("@" in ctrl+p).
func (m Model) matchPersonal(q string) []palItem {
	if m.personal == nil || q == "" {
		return nil
	}
	found, err := m.personal.Search(q)
	if err != nil {
		return nil
	}
	names := map[int64]string{}
	if lists, err := m.personal.Lists(); err == nil {
		for _, l := range lists {
			names[l.ID] = listTitle(l)
		}
	}
	items := make([]palItem, 0, len(found))
	for i := range found {
		x := found[i]
		items = append(items, palItem{pitem: &x, plist: names[x.ListID]})
	}
	return items
}

// palAction is a row that does something when chosen (keep: the list
// stays open, without it), or just says something (info, no run).
type palAction struct {
	label, right string
	info, keep   bool
	run          func(m Model) (Model, string) // returns a notice
}

// openActions opens the palette on a list of things to do.
func (m *Model) openActions(title string, items []palItem) {
	m.qo = &paletteState{pickTitle: title, actions: items}
	m.refilter()
}

func hasRunnable(items []palItem) bool {
	for _, it := range items {
		if it.action != nil && it.action.run != nil {
			return true
		}
	}
	return false
}

func matchActions(items []palItem, q string) []palItem {
	if q == "" {
		return items
	}
	qr := []rune(q)
	var out []palItem
	for _, it := range items {
		name, low := lowerRunes(it.action.label)
		if _, ok := fuzzyScore(qr, low, name); ok {
			out = append(out, it)
		}
	}
	return out
}
