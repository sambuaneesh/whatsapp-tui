package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeReader map[string][]messages.Message

func (f fakeReader) ChatMessages(_ context.Context, jid string) ([]messages.Message, error) {
	return f[jid], nil
}

func TestSplitView(t *testing.T) {
	a, b := "a@s.whatsapp.net", "b@s.whatsapp.net"
	mk := func(chat, who string, n int) []messages.Message {
		var out []messages.Message
		for i := 0; i < n; i++ {
			out = append(out, messages.Message{Id: fmt.Sprint(chat, i), ChatId: chat, ContactId: chat, ContactShort: who,
				Timestamp: uint64(1700000000 + i*60), Text: fmt.Sprintf("%s says %d", who, i)})
		}
		return out
	}
	reader := fakeReader{a: mk(a, "Arjun", 5), b: mk(b, "Bina", 5)}
	chats := []*messages.Conversation{{JID: a, Name: "Arjun", LastMsgTime: 9}, {JID: b, Name: "Bina", LastMsgTime: 8}}
	m := New(make(chan messages.Command, 50), chats, Options{SidebarWidth: 30, Images: termimg.ModeOff, Chats: reader})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	next, _ = m.Update(screenMsg(reader[a]))
	m = next.(Model)
	full := m.rightWidth()

	// v on Bina in the sidebar: beside Arjun
	m, cmds := keys(t, m, "h", "j", "v")
	m = runAll(t, m, cmds...)
	if m.split == nil || m.current.Name != "Arjun" || m.rightWidth() >= full {
		t.Fatalf("split %v, current %s, width %d", m.split, m.current.Name, m.rightWidth())
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Arjun says 4") || !strings.Contains(v, "Bina says 4") || !strings.Contains(v, "W swap") {
		t.Fatalf("not side by side:\n%s", v)
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(line); w > m.width {
			t.Fatalf("line %d is %d wide (window %d)", i, w, m.width)
		}
	}
	// a new message in Bina's chat shows up beside
	reader[b] = append(reader[b], messages.Message{Id: "new", ChatId: b, ContactId: b, ContactShort: "Bina", Timestamp: 1800000000, Text: "fresh one"})
	chats[1].LastMsgTime = 99
	next, cmd := m.Update(chatListMsg(chats))
	m = runAll(t, next.(Model), cmd)
	if !strings.Contains(ansi.Strip(m.View()), "fresh one") {
		t.Fatal("split chat didn't update")
	}
	// W swaps: Bina on the left (where you write), Arjun beside
	m, cmds = keys(t, m, "l", "W")
	m = runAll(t, m, cmds...)
	if m.current.Name != "Bina" || m.split == nil || m.split.conv.Name != "Arjun" {
		t.Fatalf("after swap: %s / %v", m.current.Name, m.split)
	}
	// :only closes it
	m = typeCmd(t, m, "only")
	if m.split != nil || m.rightWidth() != full {
		t.Fatal(":only didn't close the split")
	}
}
