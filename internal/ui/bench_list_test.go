package ui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// A list the size of a real account: 2,045 chats, 6,126 more contacts.
func bigList(b *testing.B) Model {
	b.Helper()
	var chats []*messages.Conversation
	for i := 0; i < 2045; i++ {
		chats = append(chats, &messages.Conversation{JID: fmt.Sprintf("91%08d@s.whatsapp.net", i),
			Name: fmt.Sprintf("Person %d Kumar", i), LastMsgTime: int64(100000 - i), Preview: "hello there"})
	}
	all := append([]*messages.Conversation(nil), chats...)
	for i := 0; i < 6126; i++ {
		all = append(all, &messages.Conversation{JID: fmt.Sprintf("92%08d@s.whatsapp.net", i), Name: fmt.Sprintf("Contact %d Reddy", i)})
	}
	m := New(make(chan messages.Command, 100), all, Options{SidebarWidth: 38, Images: termimg.ModeOff})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 50})
	return next.(Model)
}

// Typing in the chat filter: one keystroke, then the frame.
func BenchmarkListFilterKeystroke(b *testing.B) {
	m := bigList(b)
	m, _ = keysB2(m, "/")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := "r"
		if i%2 == 1 {
			k = "backspace"
		}
		m, _ = keysB2(m, k)
		_ = m.View()
	}
}

func BenchmarkListFrame(b *testing.B) {
	m := bigList(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func keysB2(m Model, k string) (Model, tea.Cmd) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	if k == "backspace" {
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}
