package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/personal"
)

// Your own space, among your chats: 📋 Today (everything due, from every
// list) is pinned on top, and each list (tasks, notes, saved messages)
// is a chat of its own. Open one and type in the box to add to it; the
// usual keys work on what's in it (x done, e edit, d delete, u undo…).
// Nothing here goes to WhatsApp unless you send it (f).

const (
	personalSuffix = "@personal"
	todayJID       = "today" + personalSuffix
)

func isPersonal(jid string) bool { return strings.HasSuffix(jid, personalSuffix) }

func listJID(id int64) string { return "list" + strconv.FormatInt(id, 10) + personalSuffix }

// listIDOf is the list a personal chat shows (0: Today).
func listIDOf(jid string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(jid, "list"), personalSuffix), 10, 64)
	return n
}

// PersonalChangedMsg tells the UI the personal store changed outside it
// (the API, the CLI, the Markdown mirror).
type PersonalChangedMsg struct{}

type personalTickMsg struct{}

// personalTick checks reminders and Today's counts twice a minute.
func personalTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return personalTickMsg{} })
}

// personalView is the open list.
type personalView struct {
	listID    int64 // 0: Today
	list      personal.List
	rows      []prow
	sel       int
	collapsed map[int64]bool // checklists folded
	showDone  bool
	page      *personal.Item // the note page being read
	editID    int64          // the box edits this item (0: adds)
	editPage  bool           // the box holds a whole note page
	lineOf    []int          // row → its first line on screen
}

// prow is one row: a section title, or an item.
type prow struct {
	header     string
	doneHeader bool
	count      int
	item       personal.Item
	depth      int
	list       string // in Today: the item's list
	kids, done int    // a checklist's size and how much is done
	folded     bool
}

func (r prow) isItem() bool { return r.header == "" }

// ---------- the chats in the list ----------

// refreshPersonal rebuilds the personal chats (after a change, or as time
// passes) and puts them in the chat list.
func (m *Model) refreshPersonal() {
	if m.personal == nil {
		return
	}
	m.pconvs = m.personalConvs(time.Now())
	m.setChats(m.waChats)
	if m.pv != nil && m.current != nil && isPersonal(m.current.JID) {
		m.reloadPersonal()
	}
}

func (m Model) personalConvs(now time.Time) []*messages.Conversation {
	s := m.personal
	lists, err := s.Lists()
	if err != nil {
		return nil
	}
	start, end := dayBounds(now)
	due, _ := s.Dated(end)
	overdue, today := 0, 0
	for _, it := range due {
		if isOverdue(it, now, start) {
			overdue++
		} else {
			today++
		}
	}
	preview := "nothing due today"
	switch {
	case overdue > 0 && today > 0:
		preview = fmt.Sprintf("%d overdue · %d today", overdue, today)
	case overdue > 0:
		preview = fmt.Sprintf("%d overdue", overdue)
	case today > 0:
		preview = fmt.Sprintf("%d to do today", today)
	}
	if len(due) > 0 {
		preview += " · " + due[0].Text
	}
	out := []*messages.Conversation{{
		JID: todayJID, Name: "📋 Today", LastMsgTime: now.Unix(), Preview: preview,
		Unread: uint16(min(overdue+today, 9999)), IsPinned: true,
	}}
	for _, l := range lists {
		out = append(out, &messages.Conversation{
			JID: listJID(l.ID), Name: listTitle(l), LastMsgTime: max(l.Updated, 1), Preview: m.listPreview(l, now),
			IsPinned: l.Pinned, IsArchived: l.Archived,
		})
	}
	return out
}

func listTitle(l personal.List) string {
	if l.Icon == "" {
		return l.Name
	}
	return l.Icon + " " + l.Name
}

// listPreview is the line under a list in the chat list.
func (m Model) listPreview(l personal.List, now time.Time) string {
	items, err := m.personal.Items(l.ID)
	if err != nil {
		return ""
	}
	switch l.Kind {
	case personal.KindNotes:
		pages := 0
		var last personal.Item
		for _, it := range items {
			if it.ParentID == 0 {
				pages++
				if it.Updated > last.Updated {
					last = it
				}
			}
		}
		if pages == 0 {
			return "no pages yet · type a title to start one"
		}
		return fmt.Sprintf("%d pages · %s", pages, last.Text)
	case personal.KindSaved:
		if len(items) == 0 {
			return "b on a message (in v) saves it here"
		}
		return fmt.Sprintf("%d saved · %s", len(items), items[len(items)-1].Text)
	}
	open, done := 0, 0
	var next *personal.Item
	for i, it := range items {
		if it.ParentID != 0 {
			continue
		}
		if it.Done {
			done++
			continue
		}
		open++
		if it.Due > 0 && (next == nil || it.Due < next.Due) {
			next = &items[i]
		}
	}
	if open == 0 && done == 0 {
		return "empty · type a task to add it"
	}
	s := fmt.Sprintf("%d to do", open)
	if done > 0 {
		s += fmt.Sprintf(" · %d done", done)
	}
	if next != nil {
		s += " · next: " + next.Text + " (" + dueLabel(*next, now) + ")"
	}
	if l.ShareChat != "" {
		s = "↗ shared · " + s
	}
	return s
}

// ---------- opening a list ----------

func (m *Model) openPersonal(c *messages.Conversation) tea.Cmd {
	save := m.stashDraft()
	m.screen, m.focus = screenChat, paneMessages
	m.current = c
	m.msgs, m.pins, m.pinIdx, m.search, m.replyTo, m.editing = nil, nil, 0, nil, nil, nil
	m.attachments, m.mention, m.chosen = nil, nil, nil
	if m.mode == modeVisual {
		m.mode = modeNormal
	}
	if m.split != nil {
		m.split = nil
	}
	m.pv = &personalView{listID: listIDOf(c.JID), collapsed: map[int64]bool{}}
	if m.recent != nil {
		m.recent.chats = pushRecent(m.recent.chats, c.JID)
	}
	m.compose.SetValue("")
	m.compose.SetHeight(1)
	m.reloadPersonal()
	m.resize()
	m.refreshMessages(false)
	return save
}

// inPersonal reports whether a personal list is open.
func (m Model) inPersonal() bool {
	return m.pv != nil && m.current != nil && isPersonal(m.current.JID) && m.screen == screenChat
}

func (m Model) personalPlaceholder() string {
	if m.pv == nil {
		return composePlaceholder
	}
	switch {
	case m.pv.editPage:
		return "the page: first line is its title · enter new line · esc saves"
	case m.pv.editID != 0:
		return "edit it · enter saves · esc cancels"
	case m.pv.listID == 0:
		return "i to add: call mom 6pm · pay rent fri ! · shopping: eggs · #tags"
	case m.pv.list.Kind == personal.KindNotes:
		return "i to start a page: its title (alt+enter for more lines)"
	case m.pv.list.Kind == personal.KindSaved:
		return "i to jot something down · b on a message (in v) saves it here"
	}
	return "i to add: buy milk tomorrow 7pm #home ! (several lines: a checklist)"
}

// reloadPersonal reads the open list again, keeping the selection.
func (m *Model) reloadPersonal() {
	pv := m.pv
	if pv == nil || m.personal == nil {
		return
	}
	var selID int64
	if pv.sel >= 0 && pv.sel < len(pv.rows) && pv.rows[pv.sel].isItem() {
		selID = pv.rows[pv.sel].item.ID
	}
	now := time.Now()
	if pv.listID == 0 {
		pv.rows = m.todayRows(now)
	} else {
		l, err := m.personal.List(pv.listID)
		if err != nil {
			m.notice, m.noticeErr = "that list is gone", true
			pv.rows = nil
			return
		}
		pv.list = l
		items, _ := m.personal.Items(l.ID)
		switch l.Kind {
		case personal.KindNotes, personal.KindSaved:
			pv.rows = flatRows(items, l.Kind)
		default:
			pv.rows = m.taskRows(items, now)
		}
	}
	if pv.page != nil {
		if it, err := m.personal.Item(pv.page.ID); err == nil {
			pv.page = &it
		} else {
			pv.page = nil
		}
	}
	m.compose.Placeholder = m.personalPlaceholder()
	pv.sel = min(max(pv.sel, 0), max(len(pv.rows)-1, 0))
	for i, r := range pv.rows {
		if r.isItem() && r.item.ID == selID && selID != 0 {
			pv.sel = i
			break
		}
	}
	m.selectableRow(1)
}

// selectableRow moves the selection off section titles (only Done's can
// be selected, to open it), in direction dir.
func (m *Model) selectableRow(dir int) {
	pv := m.pv
	for i := pv.sel; i >= 0 && i < len(pv.rows); i += dir {
		if pv.rows[i].isItem() || pv.rows[i].doneHeader {
			pv.sel = i
			return
		}
	}
	for i := pv.sel; i >= 0 && i < len(pv.rows); i -= dir {
		if pv.rows[i].isItem() || pv.rows[i].doneHeader {
			pv.sel = i
			return
		}
	}
}

func dayBounds(now time.Time) (start, end time.Time) {
	y, mo, d := now.Date()
	start = time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	return start, start.AddDate(0, 0, 1).Add(-time.Second)
}

func isOverdue(it personal.Item, now, start time.Time) bool {
	if it.Due == 0 || it.Done {
		return false
	}
	if it.DueTime {
		return it.Due < now.Unix()
	}
	return it.Due < start.Unix()
}

// taskRows lays out a task list: Overdue, Today, Upcoming, Someday, Done.
func (m Model) taskRows(items []personal.Item, now time.Time) []prow {
	pv := m.pv
	start, end := dayBounds(now)
	kids := map[int64][]personal.Item{}
	var top []personal.Item
	for _, it := range personal.Tree(items) {
		if it.ParentID == 0 {
			top = append(top, it)
		} else {
			kids[it.ParentID] = append(kids[it.ParentID], it)
		}
	}
	sections := []struct {
		name  string
		in    func(personal.Item) bool
		byDue bool
	}{
		{"OVERDUE", func(it personal.Item) bool { return isOverdue(it, now, start) }, true},
		{"TODAY", func(it personal.Item) bool { return it.Due > 0 && !isOverdue(it, now, start) && it.Due <= end.Unix() }, true},
		{"UPCOMING", func(it personal.Item) bool { return it.Due > end.Unix() }, true},
		{"SOMEDAY", func(it personal.Item) bool { return it.Due == 0 }, false},
	}
	var rows []prow
	for _, sec := range sections {
		var in []personal.Item
		for _, it := range top {
			if !it.Done && sec.in(it) {
				in = append(in, it)
			}
		}
		if len(in) == 0 {
			continue
		}
		if sec.byDue {
			sort.SliceStable(in, func(a, b int) bool { return in[a].Due < in[b].Due })
		}
		rows = append(rows, prow{header: sec.name, count: len(in)})
		for _, it := range in {
			rows = append(rows, m.itemRows(it, kids[it.ID], "")...)
		}
	}
	var done []personal.Item
	for _, it := range top {
		if it.Done {
			done = append(done, it)
		}
	}
	if len(done) > 0 {
		sort.SliceStable(done, func(a, b int) bool { return done[a].DoneAt > done[b].DoneAt })
		rows = append(rows, prow{header: "DONE", doneHeader: true, count: len(done), folded: !pv.showDone})
		if pv.showDone {
			for _, it := range done {
				rows = append(rows, m.itemRows(it, kids[it.ID], "")...)
			}
		}
	}
	return rows
}

// itemRows is an item and (unless folded) its checklist.
func (m Model) itemRows(it personal.Item, kids []personal.Item, list string) []prow {
	r := prow{item: it, list: list, kids: len(kids), folded: m.pv.collapsed[it.ID]}
	for _, k := range kids {
		if k.Done {
			r.done++
		}
	}
	rows := []prow{r}
	if !r.folded {
		for _, k := range kids {
			rows = append(rows, prow{item: k, depth: 1, list: list})
		}
	}
	return rows
}

// todayRows gathers what's due from every list.
func (m Model) todayRows(now time.Time) []prow {
	s := m.personal
	start, end := dayBounds(now)
	names := map[int64]string{}
	if lists, err := s.Lists(); err == nil {
		for _, l := range lists {
			names[l.ID] = l.Name
		}
	}
	week, _ := s.Dated(end.AddDate(0, 0, 7))
	var overdue, today, upcoming []personal.Item
	for _, it := range week {
		switch {
		case isOverdue(it, now, start):
			overdue = append(overdue, it)
		case it.Due <= end.Unix():
			today = append(today, it)
		default:
			upcoming = append(upcoming, it)
		}
	}
	var rows []prow
	add := func(name string, items []personal.Item) {
		if len(items) == 0 {
			return
		}
		rows = append(rows, prow{header: name, count: len(items)})
		for _, it := range items {
			kids, _ := s.Children(it.ID)
			rows = append(rows, m.itemRows(it, kids, names[it.ListID])...)
		}
	}
	add("OVERDUE", overdue)
	add("TODAY", today)
	add("NEXT 7 DAYS", upcoming)
	if done, _ := s.DoneSince(start); len(done) > 0 {
		rows = append(rows, prow{header: "DONE TODAY", doneHeader: true, count: len(done), folded: !m.pv.showDone})
		if m.pv.showDone {
			for _, it := range done {
				rows = append(rows, prow{item: it, list: names[it.ListID]})
			}
		}
	}
	return rows
}

// flatRows lists note pages (newest change first) or saved messages
// (newest first).
func flatRows(items []personal.Item, kind personal.Kind) []prow {
	var top []personal.Item
	for _, it := range items {
		if it.ParentID == 0 {
			top = append(top, it)
		}
	}
	sort.SliceStable(top, func(a, b int) bool {
		if kind == personal.KindNotes {
			return top[a].Updated > top[b].Updated
		}
		return top[a].Created > top[b].Created
	})
	rows := make([]prow, 0, len(top))
	for _, it := range top {
		rows = append(rows, prow{item: it})
	}
	return rows
}

// ---------- drawing ----------

// dueLabel says when: "17:00", "tomorrow", "Fri 10:00", "12 Oct".
func dueLabel(it personal.Item, now time.Time) string {
	if it.Due == 0 {
		return ""
	}
	t := time.Unix(it.Due, 0)
	start, _ := dayBounds(now)
	days := int(t.Sub(start).Hours() / 24)
	if t.Before(start) {
		days = -int(start.Sub(t).Hours()/24) - 1
	}
	day := t.Format("2 Jan")
	switch {
	case days == 0:
		day = "today"
	case days == 1:
		day = "tomorrow"
	case days == -1:
		day = "yesterday"
	case days > 1 && days < 7:
		day = t.Format("Mon")
	case t.Year() != now.Year():
		day = t.Format("2 Jan 2006")
	}
	if !it.DueTime {
		return day
	}
	if days == 0 {
		return t.Format("15:04")
	}
	return day + " " + t.Format("15:04")
}

// personalLines draws the open list (or note page) at width.
func (m *Model) personalLines(width int) []string {
	pv := m.pv
	if pv.page != nil {
		return m.pageLines(*pv.page, width)
	}
	now := time.Now()
	start, _ := dayBounds(now)
	var lines []string
	pv.lineOf = make([]int, len(pv.rows))
	if len(pv.rows) == 0 {
		lines = append(lines, "", styleMuted.Render("  "+emptyPersonalText(pv)))
	}
	for i, r := range pv.rows {
		pv.lineOf[i] = len(lines)
		sel := i == pv.sel && m.focus == paneMessages
		if !r.isItem() {
			if len(lines) > 0 {
				lines = append(lines, "")
				pv.lineOf[i]++
			}
			title := fmt.Sprintf(" %s  %d", r.header, r.count)
			st := lipgloss.NewStyle().Foreground(pal.Iris).Bold(true)
			if r.header == "OVERDUE" {
				st = st.Foreground(pal.Love)
			}
			if r.doneHeader {
				st = lipgloss.NewStyle().Foreground(pal.Muted).Bold(true)
				if r.folded {
					title = " ▸" + title + "  (enter shows)"
				} else {
					title = " ▾" + title
				}
			}
			line := st.Render(title)
			if sel {
				line = paint(lipgloss.NewStyle(), true).Render(padLine(ansi.Strip(title), width))
			}
			lines = append(lines, line)
			continue
		}
		lines = append(lines, m.itemLine(r, width, sel, now, start))
	}
	return lines
}

func emptyPersonalText(pv *personalView) string {
	switch {
	case pv.listID == 0:
		return "Nothing due. Tasks with a date, from any list, show up here. i adds one."
	case pv.list.Kind == personal.KindNotes:
		return "No pages yet. i, then a title (and alt+enter for the text)."
	case pv.list.Kind == personal.KindSaved:
		return "Nothing saved yet. In a chat: v, pick a message, b."
	}
	return "Empty. i, then type a task: \"call mom 6pm\", \"pay rent fri !\", \"buy milk #home\"."
}

// itemLine draws one item.
func (m Model) itemLine(r prow, width int, sel bool, now, start time.Time) string {
	it := r.item
	fill := paint(lipgloss.NewStyle(), sel)
	marker := fill.Render("  ")
	if sel {
		marker = paint(styleAccent, true).Render("▌ ")
	}
	indent := fill.Render(strings.Repeat("    ", r.depth))
	kind := m.pv.list.Kind
	if m.pv.listID == 0 {
		kind = personal.KindTasks
	}
	var left, right string
	switch kind {
	case personal.KindNotes:
		left = marker + fill.Render("📄 ") + paint(styleNameBold, sel).Render(it.Text)
		if first := firstLine(it.Body); first != "" {
			left += paint(styleDim, sel).Render("  " + first)
		}
		right = paint(styleMuted, sel).Render(listTime(it.Updated, now) + " ")
	case personal.KindSaved:
		left = marker + fill.Render("🔖 ")
		if it.SrcSender != "" {
			left += paint(lipgloss.NewStyle().Foreground(pal.Iris), sel).Render(it.SrcSender + ": ")
		}
		left += paint(styleBase, sel).Render(strings.ReplaceAll(it.Text, "\n", " "))
		where := it.Body // the chat's name
		if where != "" {
			where += " · "
		}
		right = paint(styleMuted, sel).Render(where + listTime(it.Created, now) + " ")
	default:
		box := "☐ "
		textSt := styleBase
		if it.Done {
			box = "☑ "
			textSt = styleMuted.Strikethrough(true)
		}
		boxSt := styleDim
		switch {
		case it.Done:
			boxSt = lipgloss.NewStyle().Foreground(pal.Pine)
		case it.Important:
			boxSt = lipgloss.NewStyle().Foreground(pal.Love)
		}
		left = marker + indent + paint(boxSt, sel).Render(box) + paint(textSt, sel).Render(it.Text)
		if it.Important && !it.Done {
			left += paint(lipgloss.NewStyle().Foreground(pal.Love).Bold(true), sel).Render(" !")
		}
		if r.kids > 0 {
			arrow := "▾"
			if r.folded {
				arrow = "▸"
			}
			left += paint(styleDim, sel).Render(fmt.Sprintf("  %s %d/%d", arrow, r.done, r.kids))
		}
		for _, t := range it.Tags {
			left += paint(lipgloss.NewStyle().Foreground(pal.Foam), sel).Render(" #" + t)
		}
		if it.Body != "" {
			left += paint(styleMuted, sel).Render("  ✎")
		}
		var parts []string
		if it.SrcSender != "" {
			parts = append(parts, paint(lipgloss.NewStyle().Foreground(pal.Iris), sel).Render("↩ "+it.SrcSender))
		}
		if r.list != "" {
			parts = append(parts, paint(styleMuted, sel).Render(r.list))
		}
		if d := dueLabel(it, now); d != "" && !it.Done {
			st := lipgloss.NewStyle().Foreground(pal.Gold)
			if isOverdue(it, now, start) {
				st = lipgloss.NewStyle().Foreground(pal.Love)
			}
			if it.DueTime {
				d = "⏰ " + d
			}
			parts = append(parts, paint(st, sel).Render(d))
		}
		right = strings.Join(parts, fill.Render("  ")) + fill.Render(" ")
	}
	return fitRow(left, right, width, fill)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// pageLines draws a note page: its title, then its text (Markdown light:
// # headings, - lists, - [ ] boxes, *bold* _italic_ `code`).
func (m Model) pageLines(it personal.Item, width int) []string {
	inner := max(width-4, 10)
	lines := []string{
		"",
		"  " + lipgloss.NewStyle().Foreground(pal.Rose).Bold(true).Render(it.Text),
		"  " + styleMuted.Render("edited "+time.Unix(it.Updated, 0).Format("Mon 2 Jan 15:04")+" · e edit · esc back"),
		"",
	}
	if strings.TrimSpace(it.Body) == "" {
		return append(lines, "  "+styleMuted.Render("Empty page. e writes in it."))
	}
	inCode := false
	for _, raw := range strings.Split(it.Body, "\n") {
		trim := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(trim, "```"):
			inCode = !inCode
			continue
		case inCode:
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(pal.Foam).Render(ansi.Truncate(raw, inner, "…")))
			continue
		case strings.HasPrefix(trim, "#"):
			h := strings.TrimSpace(strings.TrimLeft(trim, "#"))
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(pal.Iris).Bold(true).Render(h))
			continue
		case strings.HasPrefix(trim, "- [ ] "), strings.HasPrefix(trim, "- [x] "), strings.HasPrefix(trim, "- [X] "):
			box, st := "☐ ", styleBase
			if trim[3] != ' ' {
				box, st = "☑ ", styleMuted.Strikethrough(true)
			}
			lines = append(lines, "  "+styleDim.Render(box)+st.Render(trim[6:]))
			continue
		case strings.HasPrefix(trim, "- "), strings.HasPrefix(trim, "* "):
			raw = "• " + trim[2:]
		}
		for _, l := range formatText(raw, inner, styleBase, nil) {
			lines = append(lines, "  "+l)
		}
	}
	return lines
}

// scrollToPersonal keeps the selected row on screen.
func (m *Model) scrollToPersonal() {
	pv := m.pv
	if pv == nil || pv.page != nil || pv.sel >= len(pv.lineOf) {
		return
	}
	line := pv.lineOf[pv.sel]
	switch {
	case line < m.vp.YOffset+1:
		m.vp.SetYOffset(line - 1)
	case line >= m.vp.YOffset+m.vp.Height-1:
		m.vp.SetYOffset(line - m.vp.Height + 2)
	}
}

// ---------- keys ----------

func (m Model) selectedRow() (prow, bool) {
	pv := m.pv
	if pv == nil || pv.sel < 0 || pv.sel >= len(pv.rows) {
		return prow{}, false
	}
	return pv.rows[pv.sel], true
}

func (m Model) selectedItem() (personal.Item, bool) {
	r, ok := m.selectedRow()
	if !ok || !r.isItem() {
		return personal.Item{}, false
	}
	return r.item, true
}

// redraw re-reads the list and redraws it, keeping the selection in view.
func (m *Model) redrawPersonal() {
	m.reloadPersonal()
	m.refreshMessages(false)
	m.scrollToPersonal()
}

// afterChange refreshes everything a change touches (the list, Today's
// count, the chat list order).
func (m *Model) afterChange(err error, ok string) {
	if err != nil {
		m.notice, m.noticeErr = err.Error(), true
	} else if ok != "" {
		m.notice, m.noticeErr = ok, false
	}
	m.refreshPersonal()
	m.refreshMessages(false)
	m.scrollToPersonal()
}

func (m Model) handlePersonalKey(key string) (tea.Model, tea.Cmd) {
	pv := m.pv
	s := m.personal
	if pv.page != nil {
		switch key {
		case "esc", "q", "backspace", "h", "left":
			pv.page = nil
			m.redrawPersonal()
		case "e", "i", "enter":
			return m.editPage(*pv.page)
		case "j", "down":
			m.vp.LineDown(1)
		case "k", "up":
			m.vp.LineUp(1)
		case "ctrl+d", "pgdown":
			m.vp.LineDown(m.vp.Height / 2)
		case "ctrl+u", "pgup":
			m.vp.LineUp(m.vp.Height / 2)
		case "d":
			it := *pv.page
			pv.page = nil
			m.afterChange(s.Delete(it.ID), "Deleted "+it.Text+" · u undoes")
		case "u":
			return m.undoPersonal()
		case "y":
			return m, m.copyText(pv.page.Text+"\n\n"+pv.page.Body, "Copied the page")
		case "f":
			text := pv.page.Text + "\n\n" + pv.page.Body
			return m.sendToChat(text, "Send this page to…")
		}
		return m, nil
	}
	r, ok := m.selectedRow()
	it := r.item
	isItem := ok && r.isItem()
	switch key {
	case "j", "down":
		pv.sel = min(pv.sel+1, len(pv.rows)-1)
		m.selectableRow(1)
		m.refreshMessages(false)
		m.scrollToPersonal()
	case "k", "up":
		pv.sel = max(pv.sel-1, 0)
		m.selectableRow(-1)
		m.refreshMessages(false)
		m.scrollToPersonal()
	case "ctrl+d", "pgdown":
		pv.sel = min(pv.sel+max(m.vp.Height/2, 1), len(pv.rows)-1)
		m.selectableRow(1)
		m.refreshMessages(false)
		m.scrollToPersonal()
	case "ctrl+u", "pgup":
		pv.sel = max(pv.sel-max(m.vp.Height/2, 1), 0)
		m.selectableRow(-1)
		m.refreshMessages(false)
		m.scrollToPersonal()
	case "G":
		pv.sel = len(pv.rows) - 1
		m.selectableRow(-1)
		m.refreshMessages(false)
		m.scrollToPersonal()
	case "i", "a", "o":
		pv.editID = 0
		m.mode = modeInsert
		m.compose.Placeholder = m.personalPlaceholder()
		return m, m.compose.Focus()
	case "x", " ", "space":
		if ok && r.doneHeader {
			pv.showDone = !pv.showDone
			m.redrawPersonal()
			return m, nil
		}
		if !isItem || !m.isTaskList() {
			return m, nil
		}
		msg := "Done: " + it.Text
		if it.Done {
			msg = "Not done: " + it.Text
		}
		m.afterChange(s.SetDone(it.ID, !it.Done), msg+" · u undoes")
	case "enter", "l", "right":
		switch {
		case ok && r.doneHeader:
			pv.showDone = !pv.showDone
			m.redrawPersonal()
		case !isItem:
		case r.kids > 0 && key == "enter":
			pv.collapsed[it.ID] = !pv.collapsed[it.ID]
			m.redrawPersonal()
		case it.SrcMsg != "":
			return m.jumpToSource(it)
		case m.pv.list.Kind == personal.KindNotes:
			pv.page = &it
			m.refreshMessages(false)
			m.vp.GotoTop()
		case key == "enter":
			return m.startPersonalEdit(it)
		}
	case "e":
		if isItem {
			if m.pv.list.Kind == personal.KindNotes {
				return m.editPage(it)
			}
			return m.startPersonalEdit(it)
		}
	case "N":
		// a task's notes
		if isItem && m.isTaskList() {
			return m.editPage(it)
		}
	case "d":
		if isItem {
			m.afterChange(s.Delete(it.ID), "Deleted "+it.Text+" · u undoes")
		}
	case "u":
		return m.undoPersonal()
	case "t":
		if isItem && m.isTaskList() {
			return prefill("due ")(m)
		}
	case "!":
		if isItem && m.isTaskList() {
			it.Important = !it.Important
			label := "Important: " + it.Text
			if !it.Important {
				label = "Not important: " + it.Text
			}
			m.afterChange(s.Update(it), label)
		}
	case "J", "K":
		if isItem {
			return m.reorderPersonal(r, key == "J")
		}
	case ">":
		if isItem && m.pv.listID != 0 {
			m.afterChange(s.Indent(it.ID), "Moved into the checklist above")
		}
	case "<":
		if isItem && it.ParentID != 0 {
			m.afterChange(s.Outdent(it.ID), "Moved out of the checklist")
		}
	case "m":
		if isItem {
			return m.pickList("Move “"+short(it.Text)+"” to…", func(m Model, l personal.List) (tea.Model, tea.Cmd) {
				it.ListID, it.ParentID = l.ID, 0
				m.afterChange(s.Update(it), "Moved to "+l.Name)
				return m, nil
			})
		}
	case "y":
		if isItem {
			return m, m.copyText(itemAsText(it), "Copied")
		}
	case "Y":
		return m, m.copyText(m.listAsText(), "Copied the list")
	case "f":
		return m.sendToChat(m.listAsText(), "Send "+m.listName()+" to…")
	case "c":
		if m.pv.listID != 0 && m.isTaskList() {
			n, err := s.ClearDone(m.pv.listID)
			m.afterChange(err, fmt.Sprintf("Cleared %d done · u undoes", n))
		}
	case "h", "left", "tab", "ctrl+h":
		m.focus = paneList
		m.clampCursor()
		m.refreshMessages(false)
	case "backspace", "q":
		return m, m.back()
	case "/":
		m.openPalette("@")
	}
	return m, nil
}

func (m Model) isTaskList() bool {
	return m.pv != nil && (m.pv.listID == 0 || m.pv.list.Kind == personal.KindTasks)
}

func (m Model) listName() string {
	if m.pv == nil || m.pv.listID == 0 {
		return "Today"
	}
	return m.pv.list.Name
}

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 30 {
		return string(r[:29]) + "…"
	}
	return s
}

func (m Model) undoPersonal() (tea.Model, tea.Cmd) {
	label, err := m.personal.Undo()
	if err == nil {
		label = "Undid: " + label
	}
	m.afterChange(err, label)
	return m, nil
}

// reorderPersonal moves an item past the next (or previous) one in its
// section; dated tasks go by their time instead.
func (m Model) reorderPersonal(r prow, down bool) (tea.Model, tea.Cmd) {
	pv := m.pv
	if pv.listID == 0 {
		m.notice, m.noticeErr = "Today goes by time; t changes when", true
		return m, nil
	}
	if r.item.Due > 0 && r.depth == 0 {
		m.notice, m.noticeErr = "dated tasks go by their time; t changes it", true
		return m, nil
	}
	step := 1
	if !down {
		step = -1
	}
	for i := pv.sel + step; i >= 0 && i < len(pv.rows); i += step {
		o := pv.rows[i]
		if !o.isItem() {
			break // not past the section
		}
		if o.depth != r.depth || o.item.ParentID != r.item.ParentID {
			continue
		}
		m.afterChange(m.personal.MoveNextTo(r.item.ID, o.item.ID, down), "")
		return m, nil
	}
	return m, nil
}

// ---------- adding and editing ----------

// startPersonalEdit puts an item in the box to change it.
func (m Model) startPersonalEdit(it personal.Item) (tea.Model, tea.Cmd) {
	m.pv.editID = it.ID
	m.compose.SetValue(itemEditText(it))
	m.compose.CursorEnd()
	m.mode = modeInsert
	m.compose.Placeholder = m.personalPlaceholder()
	m.fitCompose()
	m.notice, m.noticeErr = "editing · a new date in the text changes it (or t) · enter saves · esc cancels", false
	return m, m.compose.Focus()
}

// itemEditText is an item's text with its tags and "!", for editing (the
// date stays unless you type a new one).
func itemEditText(it personal.Item) string {
	s := it.Text
	for _, t := range it.Tags {
		s += " #" + t
	}
	if it.Important {
		s += " !"
	}
	return s
}

// editPage opens a note page (or a task's notes) in the box, whole.
func (m Model) editPage(it personal.Item) (tea.Model, tea.Cmd) {
	m.pv.editID, m.pv.editPage = it.ID, true
	text := it.Text
	if it.Body != "" || m.pv.list.Kind == personal.KindNotes {
		text += "\n" + it.Body
	}
	m.compose.SetValue(text)
	m.mode = modeInsert
	m.compose.Placeholder = m.personalPlaceholder()
	m.growCompose()
	m.fitCompose()
	m.notice, m.noticeErr = "first line: the title · enter new line · esc (or ctrl+s) saves", false
	return m, m.compose.Focus()
}

// personalInsertKey handles the box's keys while writing in a list: enter
// adds (or saves), and a page takes new lines until esc.
func (m Model) personalInsertKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	pv := m.pv
	switch msg.String() {
	case "esc", "ctrl+s":
		if pv.editPage {
			return m.savePage()
		}
		if pv.editID != 0 {
			pv.editID = 0
			m.compose.SetValue("")
			m.fitCompose()
			m.notice = ""
		}
		m.mode = modeNormal
		m.compose.Blur()
		m.compose.Placeholder = m.personalPlaceholder()
		return m, nil, true
	case "enter":
		if pv.editPage {
			m.growCompose()
			m.compose.InsertString("\n")
			m.fitCompose()
			return m, nil, true
		}
		next, cmd := m.submitPersonal(strings.TrimSpace(m.compose.Value()))
		return next, cmd, true
	}
	return m, nil, false
}

func (m Model) savePage() (tea.Model, tea.Cmd, bool) {
	pv := m.pv
	text := strings.TrimRight(m.compose.Value(), " \n")
	title, body, _ := strings.Cut(text, "\n")
	pv.editPage = false
	m.mode = modeNormal
	m.compose.Blur()
	m.compose.SetValue("")
	m.compose.SetHeight(1)
	m.resize()
	if strings.TrimSpace(title) == "" {
		pv.editID = 0
		m.notice, m.noticeErr = "a page needs a title (its first line): not saved", true
		return m, nil, true
	}
	var err error
	var saved personal.Item
	if pv.editID != 0 {
		var it personal.Item
		if it, err = m.personal.Item(pv.editID); err == nil {
			it.Text, it.Body = strings.TrimSpace(title), strings.Trim(body, "\n")
			err = m.personal.Update(it)
			saved = it
		}
	} else {
		saved, err = m.personal.AddItem(personal.Item{ListID: pv.listID, Text: strings.TrimSpace(title), Body: strings.Trim(body, "\n")})
	}
	pv.editID = 0
	if err == nil && pv.list.Kind == personal.KindNotes {
		pv.page = &saved
	}
	m.afterChange(err, "Saved "+strings.TrimSpace(title))
	if pv.page != nil {
		m.vp.GotoTop()
	}
	return m, nil, true
}

// submitPersonal adds what was typed (or saves the edit).
func (m Model) submitPersonal(text string) (tea.Model, tea.Cmd) {
	pv := m.pv
	if text == "" {
		return m, nil
	}
	m.compose.SetValue("")
	m.fitCompose()
	if pv.editID != 0 {
		it, err := m.personal.Item(pv.editID)
		pv.editID = 0
		if err != nil {
			m.afterChange(err, "")
			return m, nil
		}
		p := personal.ParseTask(text, time.Now(), nil)
		it.Text, it.Tags, it.Important = p.Text, p.Tags, p.Important
		if !p.Due.IsZero() {
			it.Due, it.DueTime = p.Due.Unix(), p.DueTime
		}
		m.mode = modeNormal
		m.compose.Blur()
		m.afterChange(m.personal.Update(it), "Saved")
		return m, nil
	}
	if pv.listID != 0 && pv.list.Kind == personal.KindNotes {
		title, body, _ := strings.Cut(text, "\n")
		it, err := m.personal.AddItem(personal.Item{ListID: pv.listID, Text: title, Body: body})
		m.afterChange(err, "Added the page "+title+" · enter opens it")
		_ = it
		return m, nil
	}
	if pv.listID != 0 && pv.list.Kind == personal.KindSaved {
		m.afterChange(m.addSavedNote(text), "Saved")
		return m, nil
	}
	added, err := m.addTask(text, pv.listID, pv.listID == 0)
	m.afterChange(err, added)
	return m, nil
}

// addTask adds a typed task (or checklist) to list (0: the Inbox, or the
// list it names: "shopping: eggs"). dueToday gives an undated task today.
func (m Model) addTask(text string, listID int64, dueToday bool) (string, error) {
	var from personal.Item
	if m.pendingSrc != nil {
		from.SrcChat, from.SrcMsg, from.SrcSender = m.pendingSrc.chat, m.pendingSrc.msg, m.pendingSrc.sender
		m.pendingSrc = nil
	}
	it, kids, err := m.personal.AddTyped(text, listID, dueToday, from)
	if err != nil {
		return "", err
	}
	where := ""
	if m.pv == nil || m.pv.listID != it.ListID {
		if l, err := m.personal.List(it.ListID); err == nil {
			where = " to " + l.Name
		}
	}
	if kids > 0 {
		return fmt.Sprintf("Added the checklist %s (%d items)%s", it.Text, kids, where), nil
	}
	msg := "Added " + it.Text + where
	if it.Due > 0 {
		msg += " · " + dueLabel(it, time.Now())
	}
	return msg, nil
}

func (m Model) addSavedNote(text string) error {
	_, err := m.personal.AddItem(personal.Item{ListID: m.pv.listID, Text: text})
	return err
}

// setDue sets the selected task's date from words ("tomorrow 9am"; ""
// clears it).
func (m Model) setDue(words string) (tea.Model, tea.Cmd) {
	it, ok := m.selectedItem()
	if !ok || !m.inPersonal() {
		m.notice, m.noticeErr = ":due works on a task in a list", true
		return m, nil
	}
	words = strings.TrimSpace(words)
	if words == "" || words == "none" || words == "clear" {
		it.Due, it.DueTime = 0, false
		m.afterChange(m.personal.Update(it), "No date for "+it.Text)
		return m, nil
	}
	t, hasTime, ok := personal.FindDate(words, time.Now())
	if !ok {
		m.notice, m.noticeErr = "not a date I understand: try 9am, tomorrow 6pm, fri, 12 oct, by the 12th, a week from friday, in 2h", true
		return m, nil
	}
	it.Due, it.DueTime = t.Unix(), hasTime
	m.afterChange(m.personal.Update(it), it.Text+": "+dueLabel(it, time.Now()))
	return m, nil
}

// ---------- reminders ----------

// remindDue notifies tasks whose time has come.
func (m Model) remindDue() tea.Cmd {
	if m.personal == nil {
		return nil
	}
	due, err := m.personal.DueReminders(time.Now())
	if err != nil || len(due) == 0 {
		return nil
	}
	n := m.notifier
	mode := m.notifyMode
	var lines []string
	for _, it := range due {
		m.personal.MarkReminded(it.ID)
		lines = append(lines, it.Text)
	}
	m.notifyLogf("task reminders: %s", strings.Join(lines, "; "))
	if n == nil || mode == "off" || mode == "" {
		return nil
	}
	title := "📋 " + lines[0]
	body := "due now · open 📋 Today to tick it off"
	if len(lines) > 1 {
		title = fmt.Sprintf("📋 %d tasks due", len(lines))
		body = strings.Join(lines, "\n")
	}
	return func() tea.Msg {
		var err error
		if mode == "all" || mode == "popup" {
			err = n.Popup(title, body, true)
		}
		if mode == "all" || mode == "sound" {
			if serr := n.Sound(); err == nil {
				err = serr
			}
		}
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("reminder: %w", err)}
		}
		return nil
	}
}

// ---------- text, sending and sharing ----------

func itemAsText(it personal.Item) string {
	box := "☐"
	if it.Done {
		box = "☑"
	}
	s := box + " " + it.Text
	if it.Due > 0 && !it.Done {
		s += " (" + dueLabel(it, time.Now()) + ")"
	}
	return s
}

// listAsText is the open list as plain text, for copying or sending.
func (m Model) listAsText() string {
	pv := m.pv
	var b strings.Builder
	b.WriteString("📋 " + m.listName() + "\n")
	if pv.listID != 0 && pv.list.Kind != personal.KindTasks {
		for _, r := range pv.rows {
			b.WriteString("• " + r.item.Text + "\n")
		}
		return strings.TrimRight(b.String(), "\n")
	}
	var items []personal.Item
	if pv.listID == 0 {
		for _, r := range pv.rows {
			if r.isItem() {
				items = append(items, r.item)
			}
		}
	} else {
		items, _ = m.personal.Items(pv.listID)
		items = personal.Tree(items)
	}
	for _, it := range items {
		if it.ParentID != 0 {
			b.WriteString("    ")
		}
		b.WriteString(itemAsText(it) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) copyText(text, ok string) tea.Cmd {
	clip := m.clip
	if clip == nil {
		return nil
	}
	return func() tea.Msg { return actionDoneMsg{ok: ok, err: clip.WriteText(text)} }
}

// sendToChat picks a chat (in the palette) and sends text there.
func (m Model) sendToChat(text, title string) (tea.Model, tea.Cmd) {
	m.openChatPicker(title, func(c *messages.Conversation) bool { return !isPersonal(c.JID) },
		func(m Model, c *messages.Conversation) (tea.Model, tea.Cmd) {
			m.notice, m.noticeErr = "Sent to "+chatName(c), false
			return m, m.dispatch("send", c.JID, text)
		})
	return m, nil
}

// shareList sends the open list to a chat and keeps them linked: there,
// "done: eggs" (or "✅ eggs") ticks eggs off.
func (m Model) shareList() (tea.Model, tea.Cmd) {
	if !m.inPersonal() || m.pv.listID == 0 || m.pv.list.Kind != personal.KindTasks {
		m.notice, m.noticeErr = "share works on a task list", true
		return m, nil
	}
	text := m.listAsText() + "\n\n(reply \"done: <item>\" to tick one off)"
	m.openChatPicker("Share "+m.pv.list.Name+" with…", func(c *messages.Conversation) bool { return !isPersonal(c.JID) },
		func(m Model, c *messages.Conversation) (tea.Model, tea.Cmd) {
			l := m.pv.list
			l.ShareChat = c.JID
			m.afterChange(m.personal.UpdateList(l), "Shared with "+chatName(c)+": \"done: <item>\" there ticks it off here")
			return m, m.dispatch("send", c.JID, text)
		})
	return m, nil
}

// tickOffShared ticks off items of lists shared with the message's chat
// when it says "done: <item>" (or "✅ <item>", "☑ <item>").
func (m Model) tickOffShared(msg messages.Message) (string, bool) {
	if m.personal == nil || msg.FromMe {
		return "", false
	}
	text := strings.TrimSpace(msg.Text)
	var what string
	for _, p := range []string{"done:", "done ", "✅", "☑", "✔"} {
		if rest, ok := strings.CutPrefix(strings.ToLower(text), p); ok {
			what = strings.TrimSpace(rest)
			break
		}
	}
	if what == "" {
		return "", false
	}
	lists, err := m.personal.Lists()
	if err != nil {
		return "", false
	}
	for _, l := range lists {
		if l.ShareChat != msg.ChatId {
			continue
		}
		items, _ := m.personal.Items(l.ID)
		for _, it := range items {
			if !it.Done && strings.Contains(strings.ToLower(it.Text), what) {
				if m.personal.SetDone(it.ID, true) == nil {
					who := msg.ContactShort
					if who == "" {
						who = "someone"
					}
					return fmt.Sprintf("%s ticked off “%s” in %s", who, it.Text, l.Name), true
				}
			}
		}
	}
	return "", false
}

// ---------- from messages: tasks and saved ----------

type msgSource struct{ chat, msg, sender string }

// taskFromMessage starts a task from the selected message: the :task line,
// filled in with it (the local model, when there is one, words it as a
// task with its date).
func (m Model) taskFromMessage(sel messages.Message) (tea.Model, tea.Cmd) {
	if m.personal == nil {
		return m, nil
	}
	sender := "You"
	if !sel.FromMe {
		sender = sel.ContactShort
		if sender == "" {
			sender = sel.ContactName
		}
	}
	m.pendingSrc = &msgSource{chat: sel.ChatId, msg: sel.Id, sender: sender}
	text := strings.Join(strings.Fields(plainText(prettyTags(sel.Text))), " ")
	if r := []rune(text); len(r) > 100 {
		text = string(r[:100])
	}
	m.exitVisual()
	next, cmd := prefill("task " + text)(m)
	nm := next.(Model)
	if ai := nm.aiTaskFromMessage(sel); ai != nil {
		nm.notice, nm.noticeErr = "", false
		return nm, tea.Batch(cmd, ai)
	}
	return nm, cmd
}

// saveMessage keeps the selected message in 🔖 Saved.
func (m Model) saveMessage(sel messages.Message) (tea.Model, tea.Cmd) {
	if m.personal == nil {
		return m, nil
	}
	saved, err := m.personal.ListOfKind(personal.KindSaved, personal.SavedName)
	if err != nil {
		m.notice, m.noticeErr = err.Error(), true
		return m, nil
	}
	sender := "You"
	if !sel.FromMe {
		sender = sel.ContactShort
		if sender == "" {
			sender = sel.ContactName
		}
	}
	text := strings.TrimSpace(plainText(prettyTags(sel.Text)))
	if text == "" {
		text = "(" + sel.MediaType + ")"
	}
	chat := ""
	if m.current != nil {
		chat = chatName(m.current)
	}
	_, err = m.personal.AddItem(personal.Item{ListID: saved.ID, Text: text, Body: chat,
		SrcChat: sel.ChatId, SrcMsg: sel.Id, SrcSender: sender})
	m.exitVisual()
	if err != nil {
		m.notice, m.noticeErr = err.Error(), true
		return m, nil
	}
	m.notice, m.noticeErr = "Saved to 🔖 Saved", false
	m.refreshPersonal()
	return m, nil
}

// jumpToSource opens the chat an item came from, at its message.
func (m Model) jumpToSource(it personal.Item) (tea.Model, tea.Cmd) {
	var c *messages.Conversation
	for _, x := range append(append([]*messages.Conversation(nil), m.chats...), m.allChats...) {
		if x.JID == it.SrcChat {
			c = x
			break
		}
	}
	if c == nil {
		m.notice, m.noticeErr = "that chat isn't in your list any more", true
		return m, nil
	}
	m.showChatInList(c)
	open := m.openChat(c)
	return m, tea.Batch(open, m.loadAndSelect(c.JID, it.SrcMsg))
}

// ---------- lists: making and organising ----------

// pickList picks one of your lists in the palette.
func (m Model) pickList(title string, then func(m Model, l personal.List) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	m.openChatPicker(title, func(c *messages.Conversation) bool { return isPersonal(c.JID) && c.JID != todayJID },
		func(m Model, c *messages.Conversation) (tea.Model, tea.Cmd) {
			l, err := m.personal.List(listIDOf(c.JID))
			if err != nil {
				m.notice, m.noticeErr = err.Error(), true
				return m, nil
			}
			return then(m, l)
		})
	return m, nil
}

// theList is the list a list command acts on: the open one, else the one
// selected in the chat list.
func (m Model) theList() (personal.List, bool) {
	if m.personal == nil {
		return personal.List{}, false
	}
	c := m.theChat()
	if c == nil || !isPersonal(c.JID) || c.JID == todayJID {
		return personal.List{}, false
	}
	l, err := m.personal.List(listIDOf(c.JID))
	return l, err == nil
}

// openListByName opens a list, making it if there's none by that name.
func (m Model) openListByName(name string, kind personal.Kind) (tea.Model, tea.Cmd) {
	name = strings.TrimSpace(name)
	if name == "" {
		return m, nil
	}
	l, ok := m.personal.ListByName(name)
	if !ok {
		var err error
		if l, err = m.personal.CreateList(name, kind, ""); err != nil {
			m.notice, m.noticeErr = err.Error(), true
			return m, nil
		}
		m.notice, m.noticeErr = "Made the list "+listTitle(l)+" · type to add to it", false
	}
	m.refreshPersonal()
	for _, c := range m.chats {
		if c.JID == listJID(l.ID) {
			m.showChatInList(c)
			return m, m.openPersonal(c)
		}
	}
	return m, nil
}

// personalCommand runs the list and task ex commands; ok is false when
// line isn't one.
func (m Model) personalCommand(fields []string) (tea.Model, tea.Cmd, bool) {
	if m.personal == nil || len(fields) == 0 {
		return m, nil, false
	}
	arg := strings.TrimSpace(strings.Join(fields[1:], " "))
	need := func(what string) (tea.Model, tea.Cmd, bool) {
		m.notice, m.noticeErr = what, true
		return m, nil, true
	}
	switch fields[0] {
	case "task", "todo", "t":
		if arg == "" {
			return need(":task <what> [when] [#tag] [!] (list: … puts it in a list)")
		}
		listID := int64(0)
		if m.inPersonal() && m.isTaskList() {
			listID = m.pv.listID
		}
		msg, err := m.addTask(arg, listID, m.inPersonal() && m.pv.listID == 0)
		m.afterChange(err, msg)
		return m, nil, true
	case "today":
		for _, c := range m.chats {
			if c.JID == todayJID {
				m.showChatInList(c)
				return m, m.openPersonal(c), true
			}
		}
	case "list", "newlist":
		if arg == "" {
			return need(":list <name> opens a list (making it if needed)")
		}
		next, cmd := m.openListByName(arg, personal.KindTasks)
		return next, cmd, true
	case "notebook", "notes":
		if arg == "" {
			arg = personal.NotesName
		}
		next, cmd := m.openListByName(arg, personal.KindNotes)
		return next, cmd, true
	case "saved":
		next, cmd := m.openListByName(personal.SavedName, personal.KindSaved)
		return next, cmd, true
	case "note":
		if arg == "" {
			return need(":note <title> adds a page to Notes")
		}
		nb, err := m.personal.ListOfKind(personal.KindNotes, personal.NotesName)
		if err == nil {
			_, err = m.personal.AddItem(personal.Item{ListID: nb.ID, Text: arg})
		}
		m.afterChange(err, "Added the page "+arg+" to "+nb.Name)
		return m, nil, true
	case "due":
		next, cmd := m.setDue(arg)
		return next, cmd, true
	case "rename":
		l, ok := m.theList()
		if !ok || arg == "" {
			return need(":rename <name> renames the list")
		}
		old := l.Name
		l.Name = arg
		m.afterChange(m.personal.UpdateList(l), "Renamed "+old+" to "+arg)
		return m, nil, true
	case "icon":
		l, ok := m.theList()
		if !ok || arg == "" {
			return need(":icon <emoji> sets the list's icon")
		}
		l.Icon = arg
		m.afterChange(m.personal.UpdateList(l), "New icon for "+l.Name)
		return m, nil, true
	case "deletelist":
		l, ok := m.theList()
		if !ok {
			return need(":deletelist deletes the open list (u in it, or :undo, brings it back)")
		}
		if m.inPersonal() && m.pv.listID == l.ID {
			m.back()
		}
		m.afterChange(m.personal.DeleteList(l.ID), "Deleted the list "+l.Name+" · :undo brings it back")
		return m, nil, true
	case "plan":
		next, cmd := m.planDay()
		return next, cmd, true
	case "mirror", "obsidian":
		next, cmd := m.setMirror(arg)
		return next, cmd, true
	case "undo":
		next, cmd := m.undoPersonal()
		return next, cmd, true
	case "cleardone":
		l, ok := m.theList()
		if !ok {
			return need(":cleardone works on a task list")
		}
		n, err := m.personal.ClearDone(l.ID)
		m.afterChange(err, fmt.Sprintf("Cleared %d done · u undoes", n))
		return m, nil, true
	case "share":
		next, cmd := m.shareList()
		return next, cmd, true
	case "unshare":
		l, ok := m.theList()
		if !ok || l.ShareChat == "" {
			return need("this list isn't shared")
		}
		l.ShareChat = ""
		m.afterChange(m.personal.UpdateList(l), "Stopped sharing "+l.Name)
		return m, nil, true
	}
	return m, nil, false
}

// listChatKey handles chat list keys on a personal chat: e archives, P
// pins, d deletes (Today can't).
func (m Model) listChatKey(c *messages.Conversation, key string) (tea.Model, tea.Cmd, bool) {
	if c == nil || !isPersonal(c.JID) || m.personal == nil {
		return m, nil, false
	}
	if c.JID == todayJID {
		switch key {
		case "e", "P", "d", "U", "v":
			m.notice, m.noticeErr = "📋 Today stays on top; it gathers what's due from your lists", true
			return m, nil, true
		}
		return m, nil, false
	}
	l, err := m.personal.List(listIDOf(c.JID))
	if err != nil {
		return m, nil, false
	}
	switch key {
	case "e":
		l.Archived = !l.Archived
		label := "Archived " + l.Name
		if !l.Archived {
			label = "Moved " + l.Name + " back"
		}
		m.afterChange(m.personal.UpdateList(l), label)
		return m, nil, true
	case "P":
		l.Pinned = !l.Pinned
		label := "Pinned " + l.Name
		if !l.Pinned {
			label = "Unpinned " + l.Name
		}
		m.afterChange(m.personal.UpdateList(l), label)
		return m, nil, true
	case "d":
		m.afterChange(m.personal.DeleteList(l.ID), "Deleted the list "+l.Name+" · :undo brings it back")
		return m, nil, true
	case "U", "v", "K", "V":
		m.notice, m.noticeErr = "that's for chats; lists have e archive, P pin, d delete", true
		return m, nil, true
	}
	return m, nil, false
}

// personalClick handles a click in an open list: the box ticks, the rest
// selects (a second click on the selected one opens or edits it).
func (m Model) personalClick(x, y int) (tea.Model, tea.Cmd) {
	pv := m.pv
	line := m.vp.YOffset + y - headerRows - m.pinRows()
	for i := range pv.rows {
		if pv.lineOf == nil || i >= len(pv.lineOf) || pv.lineOf[i] != line {
			continue
		}
		col := x - (m.sidebarW + 1)
		wasSel := pv.sel == i
		pv.sel = i
		m.focus = paneMessages
		r := pv.rows[i]
		boxAt := 2 + 4*r.depth
		switch {
		case r.doneHeader || (r.isItem() && m.isTaskList() && col >= boxAt && col < boxAt+2):
			return m.handlePersonalKey("x")
		case wasSel:
			return m.handlePersonalKey("enter")
		}
		m.refreshMessages(false)
		return m, nil
	}
	return m, nil
}

// openPersonalItem opens an item's list with it selected (a note page
// opens to read).
func (m Model) openPersonalItem(it personal.Item) (tea.Model, tea.Cmd) {
	var c *messages.Conversation
	for _, x := range m.chats {
		if x.JID == listJID(it.ListID) {
			c = x
		}
	}
	if c == nil {
		m.refreshPersonal()
		for _, x := range m.chats {
			if x.JID == listJID(it.ListID) {
				c = x
			}
		}
	}
	if c == nil {
		m.notice, m.noticeErr = "its list is gone", true
		return m, nil
	}
	m.showChatInList(c)
	cmd := m.openPersonal(c)
	pv := m.pv
	if it.Done && pv.list.Kind == personal.KindTasks {
		pv.showDone = true
		m.reloadPersonal()
	}
	top := it.ID
	if it.ParentID != 0 {
		top = it.ParentID
		delete(pv.collapsed, top)
		m.reloadPersonal()
	}
	for i, r := range pv.rows {
		if r.isItem() && r.item.ID == it.ID {
			pv.sel = i
		}
	}
	if pv.list.Kind == personal.KindNotes && it.ParentID == 0 {
		x := it
		pv.page = &x
	}
	m.refreshMessages(false)
	m.scrollToPersonal()
	return m, cmd
}

// personalKind is what the header says about the open list.
func (m Model) personalKind() string {
	pv := m.pv
	switch {
	case pv.listID == 0:
		return "due from all your lists"
	case pv.page != nil:
		return "page"
	case pv.list.Kind == personal.KindNotes:
		return "notes"
	case pv.list.Kind == personal.KindSaved:
		return "saved messages"
	case pv.list.ShareChat != "":
		return "tasks · shared"
	}
	return "tasks"
}

func (m Model) personalHint() string {
	pv := m.pv
	switch {
	case pv.page != nil:
		return "e edit · j/k scroll · y copy · f send to a chat · d delete · esc back"
	case pv.listID != 0 && pv.list.Kind == personal.KindNotes:
		return "i new page · enter read · e edit · d delete · u undo · m move · / find · F1 all"
	case pv.listID != 0 && pv.list.Kind == personal.KindSaved:
		return "enter go to the message · d delete · u undo · y copy · i jot · F1 all"
	}
	return "i add · x done · e edit · t when · ! important · d delete · u undo · J/K move · >/< checklist · m list · f send · F1 all"
}

// Mirrorer keeps a Markdown folder in step with your lists
// (*personal.Mirror).
type Mirrorer interface {
	Dir() string
	SetDir(dir string) error
}

// setMirror shows, sets or turns off (":mirror off") the Markdown folder.
func (m Model) setMirror(arg string) (tea.Model, tea.Cmd) {
	if m.mirror == nil {
		m.notice, m.noticeErr = "the Markdown mirror isn't available", true
		return m, nil
	}
	switch arg {
	case "":
		if d := m.mirror.Dir(); d != "" {
			m.notice, m.noticeErr = "Your lists and notes are mirrored to "+tildePath(d)+" (:mirror off stops)", false
		} else {
			m.notice, m.noticeErr = "Not mirrored: :mirror <folder> keeps a Markdown copy there (an Obsidian vault folder)", false
		}
		return m, nil
	case "off":
		arg = ""
	default:
		arg = config.ExpandPath(arg)
	}
	if err := m.mirror.SetDir(arg); err != nil {
		m.notice, m.noticeErr = "mirror: "+err.Error(), true
		return m, nil
	}
	if err := config.SetObsidianDir(arg); err != nil {
		m.notice, m.noticeErr = err.Error(), true
		return m, nil
	}
	if arg == "" {
		m.notice, m.noticeErr = "Stopped mirroring (the files stay where they are)", false
	} else {
		m.notice, m.noticeErr = "Mirroring your lists and notes to "+tildePath(arg)+": edit them there too", false
	}
	return m, nil
}
