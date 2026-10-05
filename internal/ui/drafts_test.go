package ui

import (
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeDrafts struct {
	mu    sync.Mutex
	saved map[string]string
}

func (f *fakeDrafts) SaveDraft(jid, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if text == "" {
		delete(f.saved, jid)
	} else {
		f.saved[jid] = text
	}
	return nil
}

func (f *fakeDrafts) Drafts() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.saved {
		out[k] = v
	}
	return out
}

func draftModel(t *testing.T, store *fakeDrafts) Model {
	t.Helper()
	chats := []*messages.Conversation{
		{JID: "1@s.whatsapp.net", Name: "Arjun", LastMsgTime: 300, Preview: "hi"},
		{JID: "2@s.whatsapp.net", Name: "Priya", LastMsgTime: 200, Preview: "yo"},
	}
	m := New(make(chan messages.Command, 50), chats, Options{SidebarWidth: 38, Images: termimg.ModeOff, Drafts: store})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return next.(Model)
}

func TestDraftsPerChat(t *testing.T) {
	store := &fakeDrafts{saved: map[string]string{}}
	m := draftModel(t, store)
	m, _ = keys(t, m, "enter", "i", "h", "a", "l", "f", " ", "a", " ", "t", "h", "o", "u", "g", "h", "t", "esc")
	// open the other chat from the sidebar
	m, cmds := keys(t, m, "h", "j", "enter")
	runCmds(cmds...)
	if m.current.Name != "Priya" || m.compose.Value() != "" {
		t.Fatalf("in %s with %q", m.current.Name, m.compose.Value())
	}
	if store.saved["1@s.whatsapp.net"] != "half a thought" {
		t.Fatalf("not saved: %v", store.saved)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Draft: half a thought") {
		t.Fatalf("list doesn't show the draft:\n%s", v)
	}
	// back to the first chat: the text is back
	m, _ = keys(t, m, "h", "k", "enter")
	if m.compose.Value() != "half a thought" {
		t.Fatalf("restored %q", m.compose.Value())
	}
	// sending clears it
	m, _ = keys(t, m, "i", "enter")
	m, cmds = keys(t, m, "esc", "q")
	runCmds(cmds...)
	if _, ok := store.saved["1@s.whatsapp.net"]; ok {
		t.Fatalf("draft kept after sending: %v", store.saved)
	}
	// a restarted app has them
	store.saved["2@s.whatsapp.net"] = "see you at 8"
	m = draftModel(t, store)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Draft: see you at 8") {
		t.Fatalf("saved draft not loaded:\n%s", v)
	}
	m, _ = keys(t, m, "j", "enter")
	if m.compose.Value() != "see you at 8" {
		t.Fatalf("restored %q", m.compose.Value())
	}
}

func TestDraftSavedWhenWindowCloses(t *testing.T) {
	store := &fakeDrafts{saved: map[string]string{}}
	m := draftModel(t, store)
	m, _ = keys(t, m, "enter", "i", "o", "k")
	_, cmd := m.Update(tea.BlurMsg{})
	runCmds(cmd)
	if store.saved["1@s.whatsapp.net"] != "ok" {
		t.Fatalf("saved %v", store.saved)
	}
}
