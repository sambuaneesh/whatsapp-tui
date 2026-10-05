package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/skratchdot/open-golang/open"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

func mouseModel(t *testing.T, chats int) Model {
	t.Helper()
	var cs []*messages.Conversation
	for i := 0; i < chats; i++ {
		cs = append(cs, &messages.Conversation{JID: fmt.Sprintf("%d@s.whatsapp.net", 100+i), Name: fmt.Sprintf("Chat %02d", i),
			LastMsgTime: int64(1000 - i)})
	}
	m := New(make(chan messages.Command, 20), cs, Options{SidebarWidth: 38, Images: termimg.ModeOff})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	return next.(Model)
}

func click(t *testing.T, m Model, x, y int) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return next.(Model), cmd
}

func wheel(t *testing.T, m Model, x, y int, up bool) Model {
	t.Helper()
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
	return next.(Model)
}

func TestClickOpensChat(t *testing.T) {
	m := mouseModel(t, 30)
	// rows 0-1 are the header; each chat is 3 rows: the third chat is at 8-10
	m, _ = click(t, m, 20, 9)
	if m.screen != screenChat || m.current == nil || m.current.Name != "Chat 02" {
		t.Fatalf("opened %v", m.current)
	}
	// clicking in the sidebar opens another; clicking the header does nothing
	m, _ = click(t, m, 10, 3)
	if m.current.Name != "Chat 00" {
		t.Fatalf("sidebar click opened %s", m.current.Name)
	}
	m, _ = click(t, m, 10, 0)
	if m.current.Name != "Chat 00" {
		t.Fatal("header click changed the chat")
	}
	// clicks in the message area don't open anything
	m, _ = click(t, m, 80, 9)
	if m.current.Name != "Chat 00" {
		t.Fatal("message-area click changed the chat")
	}
}

func TestWheelScrollsListAndMessages(t *testing.T) {
	m := mouseModel(t, 30)
	for i := 0; i < 3; i++ {
		m = wheel(t, m, 20, 10, false)
	}
	if m.listOffset != 3 {
		t.Fatalf("list offset %d, want 3", m.listOffset)
	}
	// after scrolling, a click maps to the chat actually shown there
	m, _ = click(t, m, 20, 3)
	if m.current == nil || m.current.Name != "Chat 03" {
		t.Fatalf("clicked %v", m.current)
	}
	var msgs []messages.Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs, messages.Message{Id: fmt.Sprint(i), ChatId: m.current.JID, Timestamp: uint64(1700000000 + i), Text: fmt.Sprint("msg ", i)})
	}
	next, _ := m.Update(screenMsg(msgs))
	m = next.(Model)
	bottom := m.vp.YOffset
	m = wheel(t, m, 80, 10, true)
	if m.vp.YOffset != bottom-wheelLines {
		t.Fatalf("messages scrolled %d -> %d", bottom, m.vp.YOffset)
	}
	// wheel over the sidebar scrolls the list, not the messages
	off := m.vp.YOffset
	m = wheel(t, m, 10, 10, false)
	if m.vp.YOffset != off || m.listOffset != 4 {
		t.Fatalf("sidebar wheel: vp %d->%d, list %d", off, m.vp.YOffset, m.listOffset)
	}
}

func TestMouseIgnoredUnderOverlays(t *testing.T) {
	m := mouseModel(t, 5)
	m, _ = keys(t, m, "?")
	m, _ = click(t, m, 20, 3)
	if m.screen != screenList || !m.showHelp {
		t.Fatal("click went through the help screen")
	}
}

func TestClickOpensLinks(t *testing.T) {
	var opened []string
	openURL = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openURL = open.Start }()

	long := "https://www.linkedin.com/jobs/view/4470560160/?refId=abcdefghijkl&trackingId=xyz"
	m := mouseModel(t, 3)
	m, _ = keys(t, m, "enter")
	next, _ := m.Update(screenMsg{
		{Id: "1", ChatId: m.current.JID, ContactId: "x", Timestamp: 1700000000, Text: "short https://example.com/a here"},
		{Id: "2", ChatId: m.current.JID, ContactId: "x", Timestamp: 1700000060, Text: "job: " + long},
	})
	m = next.(Model)

	// find where text is drawn on screen (column in cells)
	find := func(needle string) (int, int) {
		for y, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			if i := strings.Index(line, needle); i >= 0 {
				return ansi.StringWidth(line[:i]), y
			}
		}
		t.Fatalf("%q not on screen", needle)
		return 0, 0
	}
	for _, tc := range []struct{ needle, want string }{
		{"example.com", "https://example.com/a"},
		{"https://www.link", long}, // first line of the wrapped link
		{"refId", long},            // a continuation line
	} {
		opened = nil
		x, y := find(tc.needle)
		m2, cmd := click(t, m, x+2, y)
		drain(t, m2, cmd)
		if len(opened) != 1 || opened[0] != tc.want {
			t.Fatalf("click on %q opened %v, want %s", tc.needle, opened, tc.want)
		}
	}
	// plain text next to a link opens nothing
	opened = nil
	x, y := find("short")
	m2, cmd := click(t, m, x+1, y)
	drain(t, m2, cmd)
	if len(opened) != 0 {
		t.Fatalf("click on plain text opened %v", opened)
	}
}

func TestURLAtColumn(t *testing.T) {
	line := "ab\x1b[1m\x1b]8;;https://x.io\x1b\\link\x1b]8;;\x1b\\\x1b[0m end ✨ \x1b]8;;https://y.io\x07yy\x1b]8;;\x07"
	for col, want := range map[int]string{0: "", 2: "https://x.io", 5: "https://x.io", 6: "", 11: "", 13: "", 14: "https://y.io", 15: "https://y.io", 16: ""} {
		if got := urlAtColumn(line, col); got != want {
			t.Errorf("col %d: %q, want %q", col, got, want)
		}
	}
}

// findLast returns the screen cell where needle is last drawn.
func findLast(t *testing.T, m Model, needle string) (int, int) {
	t.Helper()
	x, y := -1, -1
	for i, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if j := strings.LastIndex(line, needle); j >= 0 {
			x, y = ansi.StringWidth(line[:j]), i
		}
	}
	if y < 0 {
		t.Fatalf("%q not on screen", needle)
	}
	return x, y
}

// fakeClock makes clicks happen at controlled times.
func fakeClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Unix(1700000000, 0)
	clickNow = func() time.Time { return now }
	t.Cleanup(func() { clickNow = time.Now })
	return &now
}

func TestDoubleClickReplies(t *testing.T) {
	now := fakeClock(t)
	m := visualModel(t, &fakeActions{}, fakeClip{})
	x, y := findLast(t, m, "yes!")

	m, _ = click(t, m, x+1, y)
	if m.replyTo != nil {
		t.Fatal("a single click started a reply")
	}
	*now = now.Add(time.Second) // too slow for a double click
	m, _ = click(t, m, x+1, y)
	if m.replyTo != nil {
		t.Fatal("two slow clicks started a reply")
	}
	*now = now.Add(200 * time.Millisecond)
	m, _ = click(t, m, x+1, y)
	if m.replyTo == nil || m.replyTo.Id != "m2" || m.mode != modeInsert {
		t.Fatalf("double click: reply to %v, mode %v", m.replyTo, m.mode)
	}

	// a double click on the empty space beside a bubble does nothing
	m = visualModel(t, &fakeActions{}, fakeClip{})
	m, _ = click(t, m, m.width-3, y)
	*now = now.Add(100 * time.Millisecond)
	m, _ = click(t, m, m.width-3, y)
	if m.replyTo != nil {
		t.Fatal("double click beside the bubble started a reply")
	}
}

func TestClickQuoteSelectsOriginal(t *testing.T) {
	m := visualModel(t, &fakeActions{}, fakeClip{})
	x, y := findLast(t, m, "dinner at 8?") // the quote in "count me in"
	m, _ = click(t, m, x+2, y)
	if sel, _ := m.selected(); m.mode != modeVisual || sel.Id != "m1" {
		t.Fatalf("selected %s in mode %v", sel.Id, m.mode)
	}
}

func TestClickQuoteLoadsOlderHistory(t *testing.T) {
	const chat = "333@s.whatsapp.net"
	var all []messages.Message
	for i := 0; i < 300; i++ {
		all = append(all, messages.Message{Id: fmt.Sprint("h", i), ChatId: chat, ContactId: chat,
			Timestamp: uint64(1700000000 + i*60), Text: fmt.Sprint("history ", i)})
	}
	all[299].Text, all[299].QuotedID, all[299].QuotedText = "replying", "h10", "history 10"
	f := &fakeGlobal{chats: map[string][]messages.Message{chat: all}}
	m := New(make(chan messages.Command, 20), []*messages.Conversation{{JID: chat, Name: "Meera", LastMsgTime: 1}},
		Options{SidebarWidth: 38, Images: termimg.ModeOff, GlobalSearcher: f})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	next, _ = m.Update(screenMsg(all[250:]))
	m = next.(Model)

	x, y := findLast(t, m, "history 10")
	m, cmd := click(t, m, x+2, y)
	m = drain(t, m, cmd)
	if sel, _ := m.selected(); m.mode != modeVisual || sel.Id != "h10" {
		t.Fatalf("selected %q in mode %v (%s)", sel.Id, m.mode, m.notice)
	}
	if !strings.Contains(ansi.Strip(m.View()), "history 10") {
		t.Fatal("the quoted message isn't on screen")
	}
	// a fresh screen of the newest messages keeps the loaded history
	next, _ = m.Update(screenMsg(all[250:]))
	m = next.(Model)
	if sel, _ := m.selected(); len(m.msgs) != 290 || sel.Id != "h10" {
		t.Fatalf("after a new screen: %d messages, selected %s", len(m.msgs), sel.Id)
	}

	// a quote whose message isn't stored says so
	all[299].QuotedID = "gone"
	next, _ = m.Update(screenMsg(all[250:]))
	m = next.(Model)
	m.vp.GotoBottom()
	x, y = findLast(t, m, "history 10")
	m, cmd = click(t, m, x+2, y)
	m = drain(t, m, cmd)
	if !m.noticeErr || !strings.Contains(m.notice, "isn't on this device") {
		t.Fatalf("notice %q", m.notice)
	}
}

func TestClickPictureOpensViewer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	writePNG(t, path, fill(1200, 800, color.RGBA{156, 207, 216, 255}))
	photo := mediaMsg(t, "p1", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Width: proto.Uint32(1200), Height: proto.Uint32(800)}}, "[IMAGE] sunset at the lake")
	m := viewerModel(t, &viewActions{path: path}, photo)

	// the caption opens nothing
	x, y := findLast(t, m, "sunset at the lake")
	m, _ = click(t, m, x+2, y)
	if m.view != nil {
		t.Fatal("clicking the caption opened the viewer")
	}
	var sp msgSpan
	for _, s := range m.msgSpans {
		if s.id == "p1" {
			sp = s
		}
	}
	line := sp.media.start + sp.media.n/2
	from, to := inkColumns(m.msgLines[line])
	m, cmd := click(t, m, m.sidebarW+1+(from+to)/2, headerRows+line-m.vp.YOffset)
	m = drain(t, m, cmd)
	if m.view == nil || m.view.loading || m.view.err != nil || m.view.msg.Id != "p1" {
		t.Fatalf("viewer: %+v", m.view)
	}
}

func TestInkColumns(t *testing.T) {
	for line, want := range map[string][2]int{
		"":                        {0, 0},
		"    ":                    {0, 0},
		"  \x1b[1m╭──╮\x1b[0m   ": {2, 6},
		"   hi ✨ ":                {3, 8},
	} {
		if from, to := inkColumns(line); from != want[0] || to != want[1] {
			t.Errorf("%q: %d..%d, want %v", line, from, to, want)
		}
	}
}

func TestMouseBackAfterPlayer(t *testing.T) {
	enables := func(cmd tea.Cmd) bool {
		queue := []tea.Cmd{cmd}
		for len(queue) > 0 {
			c := queue[0]
			queue = queue[1:]
			if c == nil {
				continue
			}
			switch msg := c().(type) {
			case tea.BatchMsg:
				queue = append(queue, msg...)
			default:
				if fmt.Sprintf("%T", msg) == fmt.Sprintf("%T", tea.EnableMouseCellMotion()) {
					return true
				}
			}
		}
		return false
	}
	m := New(make(chan messages.Command, 1), nil, Options{SidebarWidth: 38, Images: termimg.ModeOff, Mouse: true})
	for _, msg := range []tea.Msg{videoDoneMsg{}, pickedFilesMsg{}} {
		if _, cmd := m.Update(msg); !enables(cmd) {
			t.Fatalf("%T: mouse not turned back on", msg)
		}
	}
	m = New(make(chan messages.Command, 1), nil, Options{SidebarWidth: 38, Images: termimg.ModeOff})
	if _, cmd := m.Update(videoDoneMsg{}); enables(cmd) {
		t.Fatal("mouse turned on with mouse = false")
	}
}
