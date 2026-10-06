package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type indexActions struct {
	fakeActions
	off map[string]bool
}

func (a *indexActions) SetChatIndexed(jid string, on bool) error {
	if on {
		delete(a.off, jid)
	} else {
		a.off[jid] = true
	}
	return nil
}

func (a *indexActions) NotIndexedChats() []string {
	var out []string
	for j := range a.off {
		out = append(out, j)
	}
	return out
}

func TestNoIndexPerChat(t *testing.T) {
	a := &indexActions{off: map[string]bool{"91111@s.whatsapp.net": true}} // Arjun was off already
	chats := []*messages.Conversation{
		{JID: groupJID, Name: "Hostel", LastMsgTime: 300},
		{JID: "91111@s.whatsapp.net", Name: "Arjun", LastMsgTime: 200},
	}
	m := New(make(chan messages.Command, 20), chats, Options{SidebarWidth: 30, Images: termimg.ModeOff, Actions: a})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(Model)
	if m.chatIndexed("91111@s.whatsapp.net") || !m.chatIndexed(groupJID) {
		t.Fatal("not loaded at start")
	}
	// in Hostel: F1 offers turning it off
	m, _ = keys(t, m, "enter")
	m, _ = press(t, m, tea.KeyF1)
	m, _ = keys(t, m, strings.Split("don't index", "")...)
	if len(m.qo.items) == 0 || m.qo.items[0].cmd.id != "noindex" {
		t.Fatalf("palette: %+v", m.qo.items)
	}
	m, _ = keys(t, m, "enter")
	if !a.off[groupJID] || !strings.Contains(stripANSI(m.View()), "not indexed for search") {
		t.Fatalf("off: %v\n%s", a.off, stripANSI(m.View()))
	}
	// :index turns it back on
	m, _ = keys(t, m, ":", "i", "n", "d", "e", "x", "enter")
	if a.off[groupJID] {
		t.Fatal(":index")
	}
	// the list of chats left out: enter puts one back
	m, _ = keys(t, m, ":", "n", "o", "i", "n", "d", "e", "x", " ", "l", "i", "s", "t", "enter")
	if m.qo == nil || len(m.qo.items) != 1 || !strings.Contains(m.qo.items[0].action.label, "Arjun") {
		t.Fatalf("list: %+v", m.qo)
	}
	m, _ = keys(t, m, "enter")
	if len(a.off) != 0 || m.qo != nil {
		t.Fatalf("index again from the list: %v", a.off)
	}
}
