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

type fakeActivity []messages.ActivityItem

func (f fakeActivity) Activity(context.Context) ([]messages.ActivityItem, error) { return f, nil }

func TestActivityFeed(t *testing.T) {
	const chat = "333@g.us"
	var history []messages.Message
	for i := 0; i < 120; i++ {
		history = append(history, messages.Message{Id: fmt.Sprint("h", i), ChatId: chat, ContactId: "1@s.whatsapp.net",
			ContactShort: "Ravi", Timestamp: uint64(1700000000 + i*60), Text: fmt.Sprint("history ", i)})
	}
	history[5].Text = "@919876 call me"
	feed := fakeActivity{
		{Kind: messages.ActivityReaction, ChatId: chat, ChatName: "Hostel", Msg: messages.Message{Id: "h100", Text: "dinner at 8?"}, Who: "Priya", Emoji: "❤️", Time: 1700009000},
		{Kind: messages.ActivityMention, ChatId: chat, ChatName: "Hostel", Msg: history[5], Who: "Ravi", Time: 1700000300},
		{Kind: messages.ActivityReply, ChatId: chat, ChatName: "Hostel", Msg: messages.Message{Id: "h3", Text: "yes!"}, Who: "Sita", Time: 1700000180},
	}
	gs := &fakeGlobal{chats: map[string][]messages.Message{chat: history}}
	m := New(make(chan messages.Command, 50), []*messages.Conversation{{JID: chat, Name: "Hostel", LastMsgTime: 9}},
		Options{SidebarWidth: 38, Images: termimg.ModeOff, Activity: feed, GlobalSearcher: gs})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m = next.(Model)

	m, cmds := keys(t, m, "I")
	m = runAll(t, m, cmds...)
	v := ansi.Strip(m.View())
	for _, want := range []string{"ACTIVITY", "❤️ Priya reacted in Hostel to: dinner at 8?", "@ Ravi mentioned you in Hostel: @919876 call me",
		"↩ Sita replied in Hostel: yes!"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	// open the mention: its chat, with the (old) message selected
	m, cmds = keys(t, m, "j", "enter")
	m = runAll(t, m, cmds...)
	if sel, _ := m.selected(); m.act != nil || m.current == nil || m.current.JID != chat || m.mode != modeVisual || sel.Id != "h5" {
		t.Fatalf("opened %v, mode %v, selected %q", m.current, m.mode, sel.Id)
	}
}
