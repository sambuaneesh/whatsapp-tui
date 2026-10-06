package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

func fuzzyStr(q, s string) (int, []int, bool) {
	name, low := lowerRunes(s)
	return fuzzyMatch([]rune(q), low, name)
}

func TestFuzzyMatch(t *testing.T) {
	if _, _, ok := fuzzyStr("hrsh", "Hari Shankar"); !ok {
		t.Fatal("hrsh should match Hari Shankar")
	}
	if _, _, ok := fuzzyStr("xyz", "Hari Shankar"); ok {
		t.Fatal("xyz matched")
	}
	if _, _, ok := fuzzyStr("sh ha", "Hari Shankar"); ok {
		t.Fatal("letters out of order matched")
	}
	// word starts beat letters in the middle
	a, _, _ := fuzzyStr("hs", "Hari Shankar")
	b, _, _ := fuzzyStr("hs", "Thushar")
	if a <= b {
		t.Fatalf("word starts %d should beat inner letters %d", a, b)
	}
	// a word starting with the query is found as a word
	_, pos, _ := fuzzyStr("sam", "Usha Sam")
	if len(pos) != 3 || pos[0] != 5 {
		t.Fatalf("sam in Usha Sam at %v, want 5,6,7", pos)
	}
	// a prefix beats the same letters later
	p1, _, _ := fuzzyStr("mo", "Mom")
	p2, _, _ := fuzzyStr("mo", "Ammo")
	if p1 <= p2 {
		t.Fatalf("prefix %d should beat %d", p1, p2)
	}
	// accents and case: positions line up with the name's runes
	_, pos, ok := fuzzyStr("jo", "JOSÉ")
	if !ok || pos[0] != 0 || pos[1] != 1 {
		t.Fatalf("JOSÉ: %v %v", pos, ok)
	}
}

func paletteModel(t *testing.T) (Model, chan messages.Command) {
	t.Helper()
	ch := make(chan messages.Command, 50)
	chats := []*messages.Conversation{
		{JID: "111@s.whatsapp.net", Name: "Alice", LastMsgTime: 100},
		{JID: "222@g.us", Name: "Book Club", LastMsgTime: 300, Unread: 2},
		{JID: "333@s.whatsapp.net", Name: "Bob", LastMsgTime: 200},
		{JID: "444@s.whatsapp.net", Name: "Hari Shankar", LastMsgTime: 50, IsArchived: true},
		{JID: "919876500000@s.whatsapp.net", Name: "Harsha (contact)", LastMsgTime: 0},
	}
	reader := fakeReader{
		"111@s.whatsapp.net": {{Id: "a1", ChatId: "111@s.whatsapp.net", Text: "from alice", Timestamp: 100}},
		"333@s.whatsapp.net": {{Id: "b1", ChatId: "333@s.whatsapp.net", Text: "from bob", Timestamp: 200}},
	}
	m := New(ch, chats, Options{SidebarWidth: 30, Images: termimg.ModeOff, Chats: reader})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return next.(Model), ch
}

func ctrlP(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = press(t, m, tea.KeyCtrlP)
	return m
}

func lastCommand(ch chan messages.Command, name string) (messages.Command, bool) {
	var got messages.Command
	found := false
	for {
		select {
		case c := <-ch:
			if c.Name == name {
				got, found = c, true
			}
		default:
			return got, found
		}
	}
}

func TestQuickOpenFuzzyOpensAtOnce(t *testing.T) {
	m, ch := paletteModel(t)
	m = ctrlP(t, m)
	if m.qo == nil {
		t.Fatal("ctrl+p didn't open the palette")
	}
	v := stripANSI(m.View())
	if !strings.Contains(v, "Go to chat") || !strings.Contains(v, "Book Club") {
		t.Fatalf("palette not drawn:\n%s", v)
	}
	if lines := strings.Count(m.View(), "\n") + 1; lines != 30 {
		t.Fatalf("view has %d lines, want 30", lines)
	}
	// fuzzy, archived chats and contacts too
	m, _ = keys(t, m, "h", "r", "s", "h")
	if len(m.qo.items) != 2 || m.qo.items[0].chat.Name != "Hari Shankar" {
		var names []string
		for _, it := range m.qo.items {
			names = append(names, it.chat.Name)
		}
		t.Fatalf("hrsh found %v", names)
	}
	m, cmds := keys(t, m, "enter")
	run(cmds)
	if m.qo != nil || m.screen != screenChat || m.current.Name != "Hari Shankar" {
		t.Fatalf("enter: palette %v screen %d current %v", m.qo, m.screen, m.current)
	}
	if c, ok := lastCommand(ch, "select"); !ok || c.Params[0] != "444@s.whatsapp.net" {
		t.Fatalf("select not sent: %+v", c)
	}
	if !m.archive {
		t.Fatal("the sidebar should switch to the archive to show the chat")
	}
}

func TestQuickOpenRecentFirstAndBackToPrevious(t *testing.T) {
	m, _ := paletteModel(t)
	open := func(name string) {
		t.Helper()
		m = ctrlP(t, m)
		m, _ = keys(t, m, strings.Split(strings.ToLower(name), "")...)
		m, _ = keys(t, m, "enter")
		if m.current == nil || m.current.Name != name {
			t.Fatalf("didn't open %s: %v", name, m.current)
		}
	}
	open("Alice")
	open("Bob")
	// empty query: recent chats first; enter goes back to the previous one
	m = ctrlP(t, m)
	if m.qo.items[0].chat.Name != "Bob" || m.qo.items[1].chat.Name != "Alice" || m.qo.cursor != 1 {
		t.Fatalf("recent order: %s, %s; cursor %d", m.qo.items[0].chat.Name, m.qo.items[1].chat.Name, m.qo.cursor)
	}
	m, _ = keys(t, m, "enter")
	if m.current.Name != "Alice" {
		t.Fatalf("ctrl+p enter should go back to Alice, got %s", m.current.Name)
	}
	// esc closes without changing anything
	m = ctrlP(t, m)
	m, _ = keys(t, m, "esc")
	if m.qo != nil || m.current.Name != "Alice" {
		t.Fatal("esc should just close")
	}
}

func TestQuickOpenPhoneNumberAndNoMatch(t *testing.T) {
	m, _ := paletteModel(t)
	m = ctrlP(t, m)
	m, _ = keys(t, m, "9", "8", "7", "6", "5")
	if len(m.qo.items) != 1 || m.qo.items[0].cand == nil || !m.qo.items[0].cand.contact {
		t.Fatalf("number search: %+v", m.qo.items)
	}
	m, _ = keys(t, m, "q", "q")
	if len(m.qo.items) != 0 || !strings.Contains(stripANSI(m.View()), "no chat or contact matches") {
		t.Fatal("no-match message missing")
	}
	// backspace widens again (the narrowed cache mustn't stick)
	m, _ = keys(t, m, "backspace", "backspace")
	if len(m.qo.items) != 1 {
		t.Fatalf("after backspace: %d items", len(m.qo.items))
	}
}

func TestCommandPalette(t *testing.T) {
	m, _ := paletteModel(t)
	m, _ = press(t, m, tea.KeyF1)
	if m.qo == nil || m.qo.mode() != '>' {
		t.Fatal("F1 should open the command palette")
	}
	// chat-only commands hide in the list
	for _, it := range m.qo.items {
		if it.cmd.when(m) == false || strings.HasPrefix(it.title, "Message:") {
			t.Fatalf("%q shown outside a chat", it.title)
		}
	}
	m, _ = keys(t, m, strings.Split("only unread", "")...)
	if len(m.qo.items) == 0 || m.qo.items[0].cmd.id != "unread" {
		t.Fatalf("'unread' found %+v", m.qo.items)
	}
	m, _ = keys(t, m, "enter")
	if !m.unreadOnly || m.qo != nil {
		t.Fatal("running the command should toggle unread-only and close")
	}
	// recently used comes first next time
	m, _ = press(t, m, tea.KeyF1)
	if m.qo.items[0].cmd.id != "unread" {
		t.Fatalf("recent command not first: %s", m.qo.items[0].title)
	}
	// backspace over ">" goes back to chats
	m, _ = keys(t, m, "backspace")
	if m.qo.mode() != 0 || len(m.qo.items) == 0 || m.qo.items[0].chat == nil {
		t.Fatal("backspace should go back to chats")
	}
	// ">" typed in Quick Open switches to commands
	m, _ = keys(t, m, ">", "h", "e", "l", "p")
	if m.qo.items[0].cmd.id != "help" {
		t.Fatalf("help: %s", m.qo.items[0].title)
	}
	m, _ = keys(t, m, "enter")
	if !m.showHelp {
		t.Fatal("help didn't open")
	}
}

func TestCommandNeedingInputPrefillsCommandLine(t *testing.T) {
	m, _ := paletteModel(t)
	m, _ = keys(t, m, "j", "enter") // a chat
	m, _ = press(t, m, tea.KeyF1)
	m, _ = keys(t, m, strings.Split("send later", "")...)
	m, _ = keys(t, m, "enter")
	if m.mode != modeCommand || m.cmdline.Value() != "later " {
		t.Fatalf("mode %d, cmdline %q", m.mode, m.cmdline.Value())
	}
}

func TestPaletteOpensBeside(t *testing.T) {
	m, _ := paletteModel(t)
	m = ctrlP(t, m)
	m, _ = keys(t, m, "b", "o", "b", "enter")
	m = ctrlP(t, m)
	m, _ = keys(t, m, "a", "l", "i")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = next.(Model)
	if m.split == nil || m.split.conv.Name != "Alice" || m.current.Name != "Bob" {
		t.Fatalf("alt+enter: split %v current %v", m.split, m.current)
	}
	if msg := cmd(); msg != nil {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
}

func TestPaletteSearchMessages(t *testing.T) {
	m, _ := paletteModel(t)
	m.globalSearcher = &fakeGlobal{}
	m = ctrlP(t, m)
	m, _ = keys(t, m, "#", "p", "i", "z", "z", "a")
	if !strings.Contains(stripANSI(m.View()), "search all chats for “pizza”") {
		t.Fatal("# mode hint missing")
	}
	m, _ = keys(t, m, "enter")
	if m.global == nil || m.global.query != "pizza" {
		t.Fatalf("global search: %+v", m.global)
	}
}

func TestVSCodeKeys(t *testing.T) {
	m, _ := paletteModel(t)
	m.globalSearcher = &fakeGlobal{}
	// F14 is ctrl+shift+f (mapped in kitty.conf)
	m, _ = press(t, m, tea.KeyF14)
	if m.global == nil {
		t.Fatal("F14 should search all chats")
	}
	m, _ = keys(t, m, "esc", "esc")
	m.global = nil
	m.mode = modeNormal
	// F13 is ctrl+shift+p
	m, _ = press(t, m, tea.KeyF13)
	if m.qo == nil || m.qo.mode() != '>' {
		t.Fatal("F13 should open commands")
	}
	m, _ = keys(t, m, "esc")
	// ctrl+f in a chat finds in it
	m, _ = keys(t, m, "enter")
	m, _ = press(t, m, tea.KeyCtrlF)
	if m.mode != modeChatSearch {
		t.Fatalf("ctrl+f: mode %d", m.mode)
	}
}

func TestPaletteMouse(t *testing.T) {
	m, _ := paletteModel(t)
	m = ctrlP(t, m)
	x, y, _, _ := m.paletteBox()
	// third result
	next, _ := m.Update(tea.MouseMsg{X: x + 5, Y: y + 3 + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.qo != nil || m.current == nil {
		t.Fatal("clicking a result should open it")
	}
	m = ctrlP(t, m)
	next, _ = m.Update(tea.MouseMsg{X: 0, Y: 25, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if next.(Model).qo != nil {
		t.Fatal("clicking outside should close")
	}
}

func TestOpenChatDrawsPreloadedMessages(t *testing.T) {
	m, _ := paletteModel(t)
	// the list highlight rests on Alice: she's loaded in the background
	m, cmds := keys(t, m, "j", "j")
	if m.selectedChat().Name != "Alice" {
		t.Fatalf("selected %v", m.selectedChat())
	}
	var cmd tea.Cmd
	for _, c := range cmds {
		if c != nil {
			cmd = c
		}
	}
	if cmd == nil {
		t.Fatal("no preload started")
	}
	next, load := m.Update(preloadTick{jid: "111@s.whatsapp.net"})
	m = next.(Model)
	if load == nil {
		t.Fatal("tick didn't load")
	}
	next, _ = m.Update(load())
	m = next.(Model)
	// opening draws them before the backend answers
	m, _ = keys(t, m, "enter")
	if len(m.msgs) != 1 || !strings.Contains(stripANSI(m.View()), "from alice") {
		t.Fatalf("preloaded messages not shown: %d", len(m.msgs))
	}
	// leaving keeps them; coming back is instant too
	m, _ = keys(t, m, "q", "k", "enter")
	m = ctrlP(t, m)
	m, _ = keys(t, m, "enter") // back to Alice
	if m.current.Name != "Alice" || len(m.msgs) != 1 {
		t.Fatalf("back to Alice: %v, %d messages", m.current, len(m.msgs))
	}
}
