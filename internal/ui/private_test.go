package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// sentCommands drains the backend commands the model dispatched.
func sentCommands(ch chan messages.Command) []string {
	var out []string
	for {
		select {
		case c := <-ch:
			out = append(out, c.Name+" "+strings.Join(c.Params, " "))
		default:
			return out
		}
	}
}

func TestPrivateReading(t *testing.T) {
	ch := make(chan messages.Command, 50)
	chats := []*messages.Conversation{{JID: "a@s.whatsapp.net", Name: "Arjun", LastMsgTime: 9, Unread: 3}}
	m := New(ch, chats, Options{SidebarWidth: 38, Images: termimg.ModeOff, PrivateReading: true})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.renderStatusLine()), "🙈 private") {
		t.Fatal("no private badge")
	}
	m, cmds := keys(t, m, "enter")
	runCmds(cmds...)
	for _, c := range sentCommands(ch) {
		if strings.HasPrefix(c, "read ") {
			t.Fatal("opening the chat sent read receipts")
		}
	}
	if m.chats[0].Unread != 3 {
		t.Fatal("chat marked read locally")
	}
	// :read marks it read
	m = typeCmd(t, m, "read")
	found := false
	for _, c := range sentCommands(ch) {
		found = found || c == "read a@s.whatsapp.net"
	}
	if !found {
		t.Fatal(":read didn't mark it read")
	}
	// clicking the badge turns it off, and the open chat counts as read
	m.chats[0].Unread = 2
	line := ansi.Strip(m.renderStatusLine())
	x := ansi.StringWidth(line[:strings.Index(line, "🙈")])
	m, cmd := click(t, m, x+1, m.mainHeight())
	runCmds(cmd)
	if m.privateRead || m.chats[0].Unread != 0 {
		t.Fatalf("private %v, unread %d", m.privateRead, m.chats[0].Unread)
	}
}
