package ui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// pinActions is fakeActions that also pins messages and chats.
type pinActions struct {
	fakeActions
	pmu     sync.Mutex
	pinned  map[string]time.Duration // msgID → length (0: unpinned)
	pinMsgs []messages.Message
	chats   []string // "jid|pin" or "jid|mute <d>"
}

func (a *pinActions) PinMessage(_ context.Context, m messages.Message, d time.Duration) error {
	a.pmu.Lock()
	defer a.pmu.Unlock()
	if a.pinned == nil {
		a.pinned = map[string]time.Duration{}
	}
	a.pinned[m.Id] = d
	return nil
}

func (a *pinActions) PinnedMessages(context.Context, string) ([]messages.Message, error) {
	return a.pinMsgs, nil
}

func (a *pinActions) PinChat(_ context.Context, jid string, pin bool) error {
	a.pmu.Lock()
	defer a.pmu.Unlock()
	a.chats = append(a.chats, jid+"|pin "+map[bool]string{true: "on", false: "off"}[pin])
	return nil
}

func (a *pinActions) MuteChat(_ context.Context, jid string, d time.Duration) error {
	a.pmu.Lock()
	defer a.pmu.Unlock()
	a.chats = append(a.chats, jid+"|mute "+d.String())
	return nil
}

func pinModel(t *testing.T, a *pinActions) Model {
	t.Helper()
	chats := []*messages.Conversation{
		{JID: groupJID, Name: "Hostel", LastMsgTime: 300},
		{JID: "91111@s.whatsapp.net", Name: "Arjun", LastMsgTime: 200},
	}
	m := New(make(chan messages.Command, 50), chats, Options{SidebarWidth: 38, Images: termimg.ModeOff, Actions: a, Deleter: &fakeDeleter{}})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	return m
}

// pinScreen delivers msgs as the backend would, then the pins it asks for.
func pinScreen(t *testing.T, m Model, msgs []messages.Message) Model {
	t.Helper()
	next, cmd := m.Update(screenMsg(msgs))
	return drain(t, next.(Model), cmd)
}

func TestPinSelectedMessage(t *testing.T) {
	a := &pinActions{}
	m := pinScreen(t, pinModel(t, a), groupMsgs())
	m, _ = keys(t, m, "v", "k") // "yes!"
	m, cmds := keys(t, m, "P")
	m = runAll(t, m, cmds...)
	if a.pinned["m2"] != 7*24*time.Hour || m.mode == modeVisual {
		t.Fatalf("P: pinned %v, mode %d", a.pinned, m.mode)
	}
	// a pinned message unpins with P
	msgs := groupMsgs()
	msgs[1].Pinned = true
	a.pinMsgs = []messages.Message{msgs[1]}
	m = pinScreen(t, m, msgs)
	if !strings.Contains(stripANSI(m.View()), "📌") {
		t.Fatal("pinned message not marked")
	}
	m, _ = keys(t, m, "v", "k")
	m, cmds = keys(t, m, "P")
	m = runAll(t, m, cmds...)
	if d, ok := a.pinned["m2"]; !ok || d != 0 {
		t.Fatalf("P on a pinned message should unpin: %v", a.pinned)
	}
}

func TestPinnedBarJumps(t *testing.T) {
	a := &pinActions{}
	msgs := groupMsgs()
	msgs[0].Pinned = true
	a.pinMsgs = []messages.Message{msgs[0]}
	m := pinModel(t, a)
	h := m.vp.Height
	m = pinScreen(t, m, msgs)
	v := stripANSI(m.View())
	if !strings.Contains(v, "📌 Arjun: dinner at 8?") {
		t.Fatalf("pinned bar missing:\n%s", v)
	}
	if m.vp.Height != h-1 {
		t.Fatalf("messages height %d, want %d (one row for the bar)", m.vp.Height, h-1)
	}
	if lines := strings.Count(m.View(), "\n") + 1; lines != 40 {
		t.Fatalf("view has %d lines", lines)
	}
	// clicking the bar selects the pinned message
	next, _ := m.Update(tea.MouseMsg{X: m.sidebarW + 10, Y: headerRows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	if s, ok := m.selected(); m.mode != modeVisual || !ok || s.Id != "m1" {
		t.Fatalf("bar click: mode %d sel %v", m.mode, s.Id)
	}
	// and from the palette
	m, _ = keys(t, m, "esc")
	m, _ = press(t, m, tea.KeyF1)
	m, _ = keys(t, m, strings.Split("pinned message", "")...)
	if m.qo.items[0].cmd.id != "pinned" {
		t.Fatalf("pinned command: %s", m.qo.items[0].title)
	}
}

func TestPaletteSelectedCommands(t *testing.T) {
	a := &pinActions{}
	m := pinScreen(t, pinModel(t, a), groupMsgs())
	m, _ = keys(t, m, "v", "k") // Priya's "yes!", with reactions
	m, _ = press(t, m, tea.KeyF1)
	// what's selected comes first, and only what applies to it
	if !strings.HasPrefix(m.qo.items[0].title, "Selected:") {
		t.Fatalf("first command %q, want the selection's", m.qo.items[0].title)
	}
	titles := map[string]bool{}
	for _, it := range m.qo.items {
		titles[it.cmd.id] = true
	}
	for _, id := range []string{"sel.reply", "sel.private", "sel.who", "sel.pin", "sel.delete", "sel.forward"} {
		if !titles[id] {
			t.Errorf("%s missing", id)
		}
	}
	for _, id := range []string{"sel.edit", "sel.retry", "sel.unpin", "sel.save", "range.copy"} {
		if titles[id] {
			t.Errorf("%s shouldn't show for someone else's text message", id)
		}
	}
	// pin for 24 hours from the palette
	m, _ = keys(t, m, strings.Split("pin 24", "")...)
	m, cmds := keys(t, m, "enter")
	m = runAll(t, m, cmds...)
	if a.pinned["m2"] != 24*time.Hour {
		t.Fatalf("pin 24h: %v", a.pinned)
	}
	// delete from the palette asks first
	m, _ = keys(t, m, "v")
	m, _ = press(t, m, tea.KeyF1)
	m, _ = keys(t, m, strings.Split("selected delete", "")...)
	m, _ = keys(t, m, "enter")
	if m.confirm == nil {
		t.Fatal("delete should ask")
	}
}

func TestPaletteSeveralSelected(t *testing.T) {
	a := &pinActions{}
	m := pinScreen(t, pinModel(t, a), groupMsgs())
	m, _ = keys(t, m, "v", "V", "k")
	m, _ = press(t, m, tea.KeyF1)
	ids := map[string]bool{}
	for _, it := range m.qo.items {
		ids[it.cmd.id] = true
	}
	if !ids["range.delete"] || !ids["range.copy"] || ids["sel.reply"] {
		t.Fatalf("range commands: %v", ids)
	}
	m, _ = keys(t, m, strings.Split("delete all", "")...)
	m, _ = keys(t, m, "enter")
	if m.confirm == nil {
		t.Fatal("deleting several should ask")
	}
}

func TestPinAndMuteChat(t *testing.T) {
	a := &pinActions{}
	m := pinModel(t, a)
	m, _ = keys(t, m, "q") // back to the list, on Hostel
	m, cmds := keys(t, m, "P")
	m = runAll(t, m, cmds...)
	m, cmds = keys(t, m, ":", "m", "u", "t", "e", " ", "8", "h", "enter")
	m = runAll(t, m, cmds...)
	m, cmds = keys(t, m, ":", "u", "n", "m", "u", "t", "e", "enter")
	m = runAll(t, m, cmds...)
	want := []string{groupJID + "|pin on", groupJID + "|mute 8h0m0s", groupJID + "|mute 0s"}
	if strings.Join(a.chats, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", a.chats, want)
	}
	m, _ = keys(t, m, ":", "m", "u", "t", "e", " ", "x", "enter")
	if !m.noticeErr {
		t.Fatal("a bad length should say what works")
	}
}

func TestPaletteArchiveUnarchive(t *testing.T) {
	f := &fakeTriage{}
	m := triageModel(t, f)
	m, _ = press(t, m, tea.KeyF1)
	m, _ = keys(t, m, strings.Split("chat archive", "")...)
	if m.qo.items[0].cmd.id != "chat.archive" {
		t.Fatalf("first: %s", m.qo.items[0].title)
	}
	m, cmds := keys(t, m, "enter")
	m = runAll(t, m, cmds...)
	if strings.Join(f.calls, ",") != "archive a@s.whatsapp.net" {
		t.Fatalf("calls %v (archive alone shouldn't mark it read)", f.calls)
	}
	// an archived chat offers unarchive instead
	m.chats[0].IsArchived = true
	m.listVer++
	m, _ = keys(t, m, "A") // the archive, on Arjun
	m, _ = press(t, m, tea.KeyF1)
	ids := map[string]bool{}
	for _, it := range m.qo.items {
		ids[it.cmd.id] = true
	}
	if !ids["chat.unarchive"] || ids["chat.archive"] {
		t.Fatalf("archived chat commands: %v", ids)
	}
}

type resyncTriage struct {
	fakeTriage
	n int
}

func (r *resyncTriage) ResyncChatSettings(context.Context) (int, error) {
	r.n++
	return 1, nil
}

func TestResync(t *testing.T) {
	f := &resyncTriage{}
	chats := []*messages.Conversation{{JID: "a@s.whatsapp.net", Name: "Arjun", LastMsgTime: 5}}
	m := New(make(chan messages.Command, 5), chats, Options{SidebarWidth: 30, Images: termimg.ModeOff, Triage: f})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m, cmds := keys(t, m, ":", "r", "e", "s", "y", "n", "c", "enter")
	m = runAll(t, m, cmds...)
	if f.n != 1 || !strings.Contains(m.notice, "match your phone again (1 chat fixed)") {
		t.Fatalf("resync: calls %d, notice %q", f.n, m.notice)
	}
}
