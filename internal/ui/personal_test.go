package ui

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/personal"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

func personalStore(t *testing.T) *personal.Store {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s, err := personal.OpenDB(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type personalEnv struct {
	m     Model
	s     *personal.Store
	cmds  chan messages.Command
	notes *fakeNotifier
	clip  fakeClip
}

func personalModel(t *testing.T) *personalEnv {
	t.Helper()
	e := &personalEnv{s: personalStore(t), cmds: make(chan messages.Command, 50), notes: &fakeNotifier{}, clip: fakeClip{wrote: &copied{}}}
	chats := []*messages.Conversation{
		{JID: "111@s.whatsapp.net", Name: "Arjun", LastMsgTime: time.Now().Unix() - 60},
		{JID: groupJID, Name: "Hostel", LastMsgTime: time.Now().Unix() - 120, IsPinned: true},
	}
	m := New(e.cmds, chats, Options{SidebarWidth: 34, Images: termimg.ModeOff, Personal: e.s,
		Notifier: e.notes, Notifications: "all", Clipboard: e.clip, Actions: &fakeActions{}})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: 36})
	m = next.(Model)
	next, _ = m.Update(PersonalChangedMsg{})
	e.m = next.(Model)
	return e
}

// typeIn types text into the box (alt+enter for new lines) and presses
// enter.
func (e *personalEnv) typeIn(t *testing.T, text string) {
	t.Helper()
	m, _ := keys(t, e.m, "i")
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
			m = next.(Model)
		}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(line)})
		m = next.(Model)
	}
	m, _ = keys(t, m, "enter", "esc")
	e.m = m
}

func (e *personalEnv) key(t *testing.T, ks ...string) {
	t.Helper()
	e.m, _ = keys(t, e.m, ks...)
}

func (e *personalEnv) view() string { return stripANSI(e.m.View()) }

func (e *personalEnv) open(t *testing.T, name string) {
	t.Helper()
	m, _ := keys(t, e.m, "q")
	for i := 0; i < m.listLen(); i++ {
		if c, _ := m.itemAt(i); c != nil && strings.Contains(c.Name, name) {
			m.cursor = i
			m, _ = keys(t, m, "enter")
			e.m = m
			return
		}
	}
	t.Fatalf("no %q in the chat list", name)
}

func (e *personalEnv) rowTexts() []string {
	var out []string
	for _, r := range e.m.pv.rows {
		if r.isItem() {
			out = append(out, r.item.Text)
		} else {
			out = append(out, "["+r.header+"]")
		}
	}
	return out
}

func (e *personalEnv) selectRow(t *testing.T, text string) {
	t.Helper()
	for i, r := range e.m.pv.rows {
		if r.isItem() && r.item.Text == text {
			e.m.pv.sel = i
			return
		}
	}
	t.Fatalf("no row %q in %v", text, e.rowTexts())
}

func TestTodayPinnedOnTopWithLists(t *testing.T) {
	e := personalModel(t)
	var names []string
	for i := 0; i < e.m.listLen(); i++ {
		c, _ := e.m.itemAt(i)
		names = append(names, chatName(c))
	}
	if names[0] != "📋 Today" {
		t.Fatalf("Today should be first: %v", names)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"📥 Inbox", "📝 Notes", "🔖 Saved", "Arjun", "Hostel"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%s missing: %v", want, names)
		}
	}
	if lines := strings.Count(e.m.View(), "\n") + 1; lines != 36 {
		t.Fatalf("view has %d lines", lines)
	}
}

func TestTodayAddTaskWithTime(t *testing.T) {
	e := personalModel(t)
	e.open(t, "Today")
	if !strings.Contains(e.view(), "Nothing due") {
		t.Fatalf("empty Today:\n%s", e.view())
	}
	e.typeIn(t, "call mom in 2h #family !")
	e.typeIn(t, "water plants")
	// both are due today (undated things typed in Today are for today)
	rows := e.rowTexts()
	if strings.Join(rows, ",") != "[TODAY],call mom,water plants" && strings.Join(rows, ",") != "[TODAY],water plants,call mom" {
		t.Fatalf("today rows: %v", rows)
	}
	v := e.view()
	for _, want := range []string{"☐ call mom !", "#family", "⏰", "Inbox"} {
		if !strings.Contains(v, want) {
			t.Fatalf("%q missing:\n%s", want, v)
		}
	}
	// the badge counts them
	c, _ := e.m.itemAt(0)
	if c.Unread != 2 {
		t.Fatalf("Today badge %d, want 2", c.Unread)
	}
	// x ticks it off: it goes to the folded "done today"
	e.selectRow(t, "water plants")
	e.key(t, "x")
	if strings.Join(e.rowTexts(), ",") != "[TODAY],call mom,[DONE TODAY]" {
		t.Fatalf("after x: %v", e.rowTexts())
	}
	e.key(t, "u")
	if len(e.rowTexts()) != 3 || e.rowTexts()[2] == "[DONE TODAY]" {
		t.Fatalf("undo: %v", e.rowTexts())
	}
}

func TestListTasksChecklistReorderIndent(t *testing.T) {
	e := personalModel(t)
	e.key(t, ":", "l", "i", "s", "t", " ", "S", "h", "o", "p", "enter")
	if !e.m.inPersonal() || e.m.pv.list.Name != "Shop" {
		t.Fatal(":list Shop should make and open it")
	}
	e.typeIn(t, "eggs")
	e.typeIn(t, "milk")
	e.typeIn(t, "bread")
	e.typeIn(t, "Party:\n- cake\n- [x] candles\n- balloons")
	if got := strings.Join(e.rowTexts(), ","); got != "[SOMEDAY],eggs,milk,bread,Party,cake,candles,balloons" {
		t.Fatalf("rows: %s", got)
	}
	if !strings.Contains(e.view(), "▾ 1/3") {
		t.Fatalf("checklist progress missing:\n%s", e.view())
	}
	// enter folds the checklist
	e.selectRow(t, "Party")
	e.key(t, "enter")
	if strings.Contains(strings.Join(e.rowTexts(), ","), "cake") {
		t.Fatal("not folded")
	}
	e.key(t, "enter")
	// bread to the top
	e.selectRow(t, "bread")
	e.key(t, "K", "K")
	if got := strings.Join(e.rowTexts()[:4], ","); got != "[SOMEDAY],bread,eggs,milk" {
		t.Fatalf("after K K: %s", got)
	}
	// milk under eggs (>) and back (<)
	e.selectRow(t, "milk")
	e.key(t, ">")
	eggs := e.m.pv.rows[2]
	if eggs.item.Text != "eggs" || eggs.kids != 1 {
		t.Fatalf("indent: %+v", e.rowTexts())
	}
	e.selectRow(t, "milk")
	e.key(t, "<")
	e.selectRow(t, "milk")
	if e.m.pv.rows[e.m.pv.sel].depth != 0 {
		t.Fatal("outdent")
	}
	// t sets a date: it moves to Upcoming
	e.selectRow(t, "eggs")
	e.key(t, "t")
	if e.m.mode != modeCommand || e.m.cmdline.Value() != "due " {
		t.Fatalf("t: mode %d %q", e.m.mode, e.m.cmdline.Value())
	}
	e.key(t, "t", "o", "m", "o", "r", "r", "o", "w", " ", "9", "a", "m", "enter")
	if got := strings.Join(e.rowTexts()[:2], ","); got != "[UPCOMING],eggs" {
		t.Fatalf("after t: %v", e.rowTexts())
	}
	if !strings.Contains(e.view(), "tomorrow 09:00") {
		t.Fatalf("due label:\n%s", e.view())
	}
	// e edits the text, keeping the date
	e.selectRow(t, "eggs")
	e.key(t, "e")
	if e.m.compose.Value() != "eggs" {
		t.Fatalf("edit text %q", e.m.compose.Value())
	}
	e.m.compose.SetValue("free-range eggs #organic")
	e.key(t, "enter")
	e.selectRow(t, "free-range eggs")
	if it, _ := e.m.selectedItem(); it.Due == 0 || strings.Join(it.Tags, ",") != "organic" {
		t.Fatalf("edit lost the date or tags: %+v", it)
	}
	// ! marks it important, d deletes, u brings it back
	e.key(t, "!")
	if it, _ := e.m.selectedItem(); !it.Important {
		t.Fatal("!")
	}
	e.key(t, "d")
	if strings.Contains(strings.Join(e.rowTexts(), ","), "free-range") {
		t.Fatal("not deleted")
	}
	e.key(t, "u")
	if !strings.Contains(strings.Join(e.rowTexts(), ","), "free-range") {
		t.Fatal("undo delete")
	}
}

func TestDoneSectionAndClear(t *testing.T) {
	e := personalModel(t)
	e.open(t, "Inbox")
	e.typeIn(t, "one")
	e.typeIn(t, "two")
	e.selectRow(t, "one")
	e.key(t, "x")
	if got := strings.Join(e.rowTexts(), ","); got != "[SOMEDAY],two,[DONE]" {
		t.Fatalf("done folded: %s", got)
	}
	// enter on DONE shows it
	e.m.pv.sel = 2
	e.key(t, "enter")
	if got := strings.Join(e.rowTexts(), ","); got != "[SOMEDAY],two,[DONE],one" {
		t.Fatalf("done shown: %s", got)
	}
	if !strings.Contains(e.view(), "☑ one") {
		t.Fatal("done mark")
	}
	e.key(t, "c")
	if strings.Contains(strings.Join(e.rowTexts(), ","), "one") {
		t.Fatal("c should clear done")
	}
}

func TestNotesPages(t *testing.T) {
	e := personalModel(t)
	e.open(t, "Notes")
	e.typeIn(t, "Wifi\nThe password is on the fridge\n- [ ] change it")
	if got := strings.Join(e.rowTexts(), ","); got != "Wifi" {
		t.Fatalf("pages: %s", got)
	}
	e.key(t, "enter")
	v := e.view()
	if e.m.pv.page == nil || !strings.Contains(v, "The password is on the fridge") || !strings.Contains(v, "☐ change it") {
		t.Fatalf("page view:\n%s", v)
	}
	// e edits the whole page; enter is a new line, esc saves
	e.key(t, "e")
	if !e.m.pv.editPage || !strings.HasPrefix(e.m.compose.Value(), "Wifi\n") {
		t.Fatalf("edit page: %q", e.m.compose.Value())
	}
	e.m.compose.SetValue("Wi-Fi\nhome: hunter2")
	e.key(t, "enter")
	if !strings.Contains(e.m.compose.Value(), "\n") || e.m.mode != modeInsert {
		t.Fatal("enter should add a line in a page")
	}
	e.key(t, "esc")
	if e.m.pv.page == nil || e.m.pv.page.Text != "Wi-Fi" || !strings.Contains(e.m.pv.page.Body, "hunter2") {
		t.Fatalf("saved page: %+v", e.m.pv.page)
	}
	e.key(t, "esc")
	if e.m.pv.page != nil {
		t.Fatal("esc should go back to the pages")
	}
}

func TestMessageToTaskAndSaved(t *testing.T) {
	e := personalModel(t)
	e.open(t, "Hostel")
	next, _ := e.m.Update(screenMsg(groupMsgs()))
	e.m = next.(Model)
	// b saves "dinner at 8?"
	e.key(t, "v", "k", "k", "b")
	saved, _ := e.s.ListOfKind(personal.KindSaved, personal.SavedName)
	items, _ := e.s.Items(saved.ID)
	if len(items) != 1 || items[0].Text != "dinner at 8?" || items[0].SrcSender != "Arjun" || items[0].SrcMsg != "m1" {
		t.Fatalf("saved: %+v", items)
	}
	// T makes a task from it: the :task line, filled in
	e.key(t, "v", "k", "k", "T")
	if e.m.mode != modeCommand || e.m.cmdline.Value() != "task dinner at 8?" {
		t.Fatalf("T: %q", e.m.cmdline.Value())
	}
	e.key(t, "enter")
	inbox, _ := e.s.ListOfKind(personal.KindTasks, personal.InboxName)
	tasks, _ := e.s.Items(inbox.ID)
	if len(tasks) != 1 || tasks[0].Text != "dinner" || !tasks[0].DueTime || tasks[0].SrcSender != "Arjun" {
		t.Fatalf("task from message: %+v", tasks)
	}
	// in Saved, enter goes back to the message
	e.open(t, "Saved")
	e.key(t, "enter")
	if e.m.current == nil || e.m.current.JID != groupJID {
		t.Fatalf("enter on a saved message should open its chat, got %v", e.m.current)
	}
}

func TestSendAndShareList(t *testing.T) {
	e := personalModel(t)
	e.open(t, "Inbox")
	e.typeIn(t, "eggs")
	e.typeIn(t, "milk")
	e.key(t, "f")
	if e.m.qo == nil || e.m.qo.onPick == nil {
		t.Fatal("f should pick a chat")
	}
	for _, it := range e.m.qo.items {
		if isPersonal(it.chat.JID) {
			t.Fatal("lists aren't chats to send to")
		}
	}
	e.key(t, "a", "r", "j")
	var cmds []tea.Cmd
	e.m, cmds = keys(t, e.m, "enter")
	run(cmds)
	c := <-e.cmds
	if c.Name != "send" || c.Params[0] != "111@s.whatsapp.net" || !strings.Contains(c.Params[1], "☐ eggs") {
		t.Fatalf("sent %+v", c)
	}
	// share with the group: "done: milk" there ticks milk off here
	e.key(t, ":", "s", "h", "a", "r", "e", "enter")
	e.key(t, "h", "o", "s")
	e.m, cmds = keys(t, e.m, "enter")
	run(cmds)
	<-e.cmds
	next, _ := e.m.Update(incomingMsg{msg: messages.Message{Id: "x", ChatId: groupJID, ContactShort: "Priya",
		Text: "done: milk", Timestamp: uint64(time.Now().Unix())}, chatName: "Hostel"})
	e.m = next.(Model)
	if !strings.Contains(e.m.notice, "Priya ticked off “milk”") {
		t.Fatalf("notice %q", e.m.notice)
	}
	inbox, _ := e.s.ListOfKind(personal.KindTasks, personal.InboxName)
	items, _ := e.s.Items(inbox.ID)
	for _, it := range items {
		if it.Text == "milk" && !it.Done {
			t.Fatal("milk not ticked off")
		}
	}
}

func TestReminders(t *testing.T) {
	e := personalModel(t)
	inbox, _ := e.s.ListOfKind(personal.KindTasks, personal.InboxName)
	e.s.AddItem(personal.Item{ListID: inbox.ID, Text: "stretch", Due: time.Now().Add(-time.Minute).Unix(), DueTime: true})
	next, cmd := e.m.Update(personalTickMsg{})
	e.m = next.(Model)
	for _, msg := range drainMsgs(cmd) {
		_ = msg
	}
	if len(e.notes.popups) != 1 || !strings.Contains(e.notes.popups[0], "stretch") {
		t.Fatalf("popups: %v", e.notes.popups)
	}
	// once only
	e.notes.reset()
	_, cmd = e.m.Update(personalTickMsg{})
	drainMsgs(cmd)
	if len(e.notes.popups) != 0 {
		t.Fatal("reminded twice")
	}
}

// drainMsgs runs a command's batch (not ticks) and returns the messages.
func drainMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if b, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, drainMsgs(c)...)
			}
			return out
		}
		return []tea.Msg{msg}
	case <-time.After(200 * time.Millisecond):
		return nil // a tick
	}
}

func TestListOrganising(t *testing.T) {
	e := personalModel(t)
	e.key(t, ":", "l", "i", "s", "t", " ", "W", "o", "r", "k", "enter")
	e.key(t, ":", "i", "c", "o", "n", " ", "💼", "enter")
	e.key(t, ":", "r", "e", "n", "a", "m", "e", " ", "J", "o", "b", "enter")
	l, ok := e.s.ListByName("Job")
	if !ok || l.Icon != "💼" {
		t.Fatalf("rename/icon: %+v %v", l, ok)
	}
	// in the chat list: P pins it (under Today), e archives, d deletes
	e.key(t, "q")
	for i := 0; i < e.m.listLen(); i++ {
		if c, _ := e.m.itemAt(i); c != nil && c.Name == "💼 Job" {
			e.m.cursor = i
		}
	}
	e.key(t, "P")
	if c, _ := e.m.itemAt(1); c == nil || c.Name != "💼 Job" {
		t.Fatalf("pinned list should come right after Today, got %v", c)
	}
	e.key(t, "e")
	if l, _ := e.s.ListByName("Job"); !l.Archived {
		t.Fatal("e should archive the list")
	}
	e.key(t, ":", "u", "n", "d", "o", "enter")
	if l, _ := e.s.ListByName("Job"); l.Archived {
		t.Fatal("undo archive")
	}
	// Today can't be archived
	e.m.cursor = 0
	e.key(t, "e")
	if !e.m.noticeErr {
		t.Fatal("Today shouldn't archive")
	}
	// m moves a task to another list
	e.open(t, "Inbox")
	e.typeIn(t, "update CV")
	e.selectRow(t, "update CV")
	e.key(t, "m")
	if e.m.qo == nil {
		t.Fatal("m should pick a list")
	}
	e.key(t, "j", "o", "b", "enter")
	job, _ := e.s.ListByName("Job")
	if items, _ := e.s.Items(job.ID); len(items) != 1 || items[0].Text != "update CV" {
		t.Fatalf("moved: %+v", items)
	}
}

func TestPaletteFindsListsAndItems(t *testing.T) {
	e := personalModel(t)
	e.key(t, ":", "t", "a", "s", "k", " ", "r", "e", "n", "e", "w", " ", "p", "a", "s", "s", "p", "o", "r", "t", "enter")
	// ctrl+p finds a list like a chat
	e.m, _ = press(t, e.m, tea.KeyCtrlP)
	e.key(t, "i", "n", "b")
	if e.m.qo.items[0].chat == nil || e.m.qo.items[0].chat.Name != "📥 Inbox" {
		t.Fatalf("ctrl+p inb: %+v", e.m.qo.items[0])
	}
	e.key(t, "esc")
	// @ finds what's in them
	e.m, _ = press(t, e.m, tea.KeyCtrlP)
	e.key(t, "@", "p", "a", "s", "s")
	if len(e.m.qo.items) != 1 || e.m.qo.items[0].pitem == nil {
		t.Fatalf("@pass: %+v", e.m.qo.items)
	}
	e.key(t, "enter")
	if !e.m.inPersonal() || e.m.pv.list.Name != "Inbox" {
		t.Fatal("@ result should open its list")
	}
	if it, ok := e.m.selectedItem(); !ok || it.Text != "renew passport" {
		t.Fatalf("selected %+v", it)
	}
}

func TestCommandsInAList(t *testing.T) {
	e := personalModel(t)
	e.open(t, "Inbox")
	e.typeIn(t, "pay rent")
	e.m, _ = press(t, e.m, tea.KeyF1)
	ids := map[string]bool{}
	for _, it := range e.m.qo.items {
		ids[it.cmd.id] = true
	}
	for _, want := range []string{"p.done", "p.due", "p.move", "p.send", "p.rename", "p.share"} {
		if !ids[want] {
			t.Errorf("%s missing in a list", want)
		}
	}
	for _, not := range []string{"find", "chat.mute", "info", "write", "done", "chat.archive"} {
		if ids[not] {
			t.Errorf("chat command %s shown in a list", not)
		}
	}
	e.key(t, strings.Split("set when", "")...)
	e.key(t, "enter")
	if e.m.mode != modeCommand || e.m.cmdline.Value() != "due " {
		t.Fatalf("set when: %q", e.m.cmdline.Value())
	}
}
