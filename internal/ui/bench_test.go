package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// Benchmarks for how fast the chat screen reacts. Run with
//
//	go test ./internal/ui -run XXX -bench . -benchmem

// benchChat is a busy group chat of n messages: several senders, links,
// replies, reactions and a mention, at a typical window size.
func benchChat(b *testing.B, n int) Model {
	b.Helper()
	m := New(make(chan messages.Command, 100), []*messages.Conversation{{JID: groupJID, Name: "Hostel", LastMsgTime: 1}},
		Options{SidebarWidth: 38, Images: termimg.ModeOff})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 50})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(screenMsg(benchMessages(n)))
	return next.(Model)
}

func benchMessages(n int) []messages.Message {
	msgs := make([]messages.Message, n)
	for i := range msgs {
		x := messages.Message{Id: fmt.Sprint("m", i), ChatId: groupJID, ContactId: fmt.Sprintf("91%d@s.whatsapp.net", i%7),
			ContactShort: fmt.Sprint("Person ", i%7), Timestamp: uint64(1700000000 + i*300), FromMe: i%5 == 0,
			Text: strings.Repeat("some *message* text with a link https://example.com/x and _words_ ", 1+i%4)}
		if i%9 == 0 {
			x.Reactions = []messages.Reaction{{Sender: "a", Emoji: "👍"}, {Sender: "", Emoji: "❤️"}}
		}
		if i%11 == 0 && i > 0 {
			x.QuotedID, x.QuotedText = fmt.Sprint("m", i-1), "the message before"
		}
		msgs[i] = x
	}
	return msgs
}

// A full redraw of the messages (after a resize, say).
func BenchmarkRefresh400(b *testing.B) {
	m := benchChat(b, 400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.refreshMessages(false)
	}
}

// One frame: what Bubble Tea calls after every update.
func BenchmarkView(b *testing.B) {
	m := benchChat(b, 400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// j in visual mode, then the frame.
func BenchmarkVisualMove(b *testing.B) {
	m := benchChat(b, 400)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(Model)
	up, down := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := up
		if i%2 == 1 {
			k = down
		}
		next, _ = m.Update(k)
		m = next.(Model)
		_ = m.View()
	}
}

// A message arriving in the open chat, then the frame.
func BenchmarkNewMessage(b *testing.B) {
	m := benchChat(b, 400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next, _ := m.Update(newMessageMsg(messages.Message{Id: fmt.Sprint("new", i), ChatId: groupJID,
			ContactId: "1@s.whatsapp.net", ContactShort: "Someone", Timestamp: uint64(1800000000 + i), Text: "hello there"}))
		m = next.(Model)
		_ = m.View()
		if len(m.msgs) > 450 {
			m.msgs = m.msgs[:400]
		}
	}
}

// A screen of messages arriving again (read receipts, reactions), then the
// frame.
func BenchmarkScreenUpdate(b *testing.B) {
	m := benchChat(b, 400)
	msgs := benchMessages(400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msgs[399].Status = i % 4
		next, _ := m.Update(screenMsg(msgs))
		m = next.(Model)
		_ = m.View()
	}
}

// Scrolling with ctrl+u / ctrl+d, then the frame.
func BenchmarkScroll(b *testing.B) {
	m := benchChat(b, 400)
	up, down := tea.KeyMsg{Type: tea.KeyCtrlU}, tea.KeyMsg{Type: tea.KeyCtrlD}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := up
		if i%2 == 1 {
			k = down
		}
		next, _ := m.Update(k)
		m = next.(Model)
		_ = m.View()
	}
}

// Opening a chat: every bubble rendered from scratch.
func BenchmarkRefreshCold400(b *testing.B) {
	m := benchChat(b, 400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.bubbles = newBubbleCache()
		m.refreshMessages(false)
	}
}
