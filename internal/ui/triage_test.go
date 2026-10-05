package ui

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeTriage struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeTriage) ArchiveChat(_ context.Context, jid string, archive bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if archive {
		f.calls = append(f.calls, "archive "+jid)
	} else {
		f.calls = append(f.calls, "unarchive "+jid)
	}
	return nil
}

func (f *fakeTriage) MarkChatUnread(_ context.Context, jid string, unread bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if unread {
		f.calls = append(f.calls, "unread "+jid)
	} else {
		f.calls = append(f.calls, "read "+jid)
	}
	return nil
}

func triageModel(t *testing.T, f *fakeTriage) Model {
	t.Helper()
	chats := []*messages.Conversation{
		{JID: "a@s.whatsapp.net", Name: "Arjun", LastMsgTime: 500, Unread: 2},
		{JID: "b@s.whatsapp.net", Name: "Bina", LastMsgTime: 400},
		{JID: "c@s.whatsapp.net", Name: "Chitra", LastMsgTime: 300, Unread: 1},
		{JID: "d@s.whatsapp.net", Name: "Dev", LastMsgTime: 200},
	}
	m := New(make(chan messages.Command, 50), chats, Options{SidebarWidth: 38, Images: termimg.ModeOff, Triage: f})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return next.(Model)
}

// run executes commands and feeds their results back, like the program.
func runAll(t *testing.T, m Model, cmds ...tea.Cmd) Model {
	t.Helper()
	return drain(t, m, tea.Batch(cmds...))
}

func TestTriageInList(t *testing.T) {
	f := &fakeTriage{}
	m := triageModel(t, f)
	m, cmds := keys(t, m, "j", "e") // Bina: done
	m = runAll(t, m, cmds...)
	if strings.Join(f.calls, ",") != "read b@s.whatsapp.net,archive b@s.whatsapp.net" || !strings.Contains(m.notice, "Done with Bina") {
		t.Fatalf("calls %v, notice %q", f.calls, m.notice)
	}
	f.calls = nil
	m, cmds = keys(t, m, "U") // Bina is read: mark unread
	m = runAll(t, m, cmds...)
	if strings.Join(f.calls, ",") != "unread b@s.whatsapp.net" {
		t.Fatalf("U: %v", f.calls)
	}
	// J: the next unread after the cursor, Chitra
	m, cmds = keys(t, m, "J")
	if m.current == nil || m.current.Name != "Chitra" || m.screen != screenChat {
		t.Fatalf("J opened %v", m.current)
	}
	// an archived chat: e brings it back
	f.calls = nil
	m = triageModel(t, f)
	m.chats[1].IsArchived = true
	m.setChats(m.chats)
	m, _ = keys(t, m, "A")
	m, cmds = keys(t, m, "e")
	m = runAll(t, m, cmds...)
	if strings.Join(f.calls, ",") != "unarchive b@s.whatsapp.net" {
		t.Fatalf("archived e: %v", f.calls)
	}
}

func TestTriageInChat(t *testing.T) {
	f := &fakeTriage{}
	m := triageModel(t, f)
	m, _ = keys(t, m, "enter") // Arjun (unread)
	m, cmds := keys(t, m, "e")
	m = runAll(t, m, cmds...)
	if m.current.Name != "Chitra" || m.screen != screenChat {
		t.Fatalf("e didn't move on to the next unread: %v", m.current.Name)
	}
	if strings.Join(f.calls, ",") != "read a@s.whatsapp.net,archive a@s.whatsapp.net" {
		t.Fatalf("calls %v", f.calls)
	}
	// the last unread: back to the list, all caught up
	m.chats[0].Unread = 0
	m, cmds = keys(t, m, "e")
	m = runAll(t, m, cmds...)
	if m.screen != screenList || !strings.Contains(m.notice, "no more unread chats") {
		t.Fatalf("screen %v notice %q", m.screen, m.notice)
	}
	// U in a chat marks it unread and leaves it
	f.calls = nil
	m, _ = keys(t, m, "enter")
	m, cmds = keys(t, m, "U")
	m = runAll(t, m, cmds...)
	if m.screen != screenList || len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "unread ") {
		t.Fatalf("U in chat: screen %v calls %v", m.screen, f.calls)
	}
	// J with nothing unread
	for _, c := range m.chats {
		c.Unread = 0
	}
	m, _ = keys(t, m, "J")
	if !strings.Contains(m.notice, "all caught up") {
		t.Fatalf("notice %q", m.notice)
	}
}
