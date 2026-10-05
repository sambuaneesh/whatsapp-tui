package ui

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeNotifier struct {
	mu     sync.Mutex
	popups []string // "title|body"
	sounds int
}

func (f *fakeNotifier) Popup(title, body string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.popups = append(f.popups, title+"|"+body)
	return nil
}

func (f *fakeNotifier) Sound() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sounds++
	return nil
}

func (f *fakeNotifier) reset() { f.popups, f.sounds = nil, 0 }

func notifyModel(t *testing.T, mode string) (Model, *fakeNotifier) {
	t.Helper()
	n := &fakeNotifier{}
	chats := []*messages.Conversation{
		{JID: groupJID, Name: "Hostel", LastMsgTime: 300},
		{JID: "91111@s.whatsapp.net", Name: "Arjun", LastMsgTime: 200},
	}
	m := New(make(chan messages.Command, 20), chats, Options{SidebarWidth: 38, Images: termimg.ModeOff,
		Notifications: mode, Notifier: n})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	return next.(Model), n
}

// receive delivers an incoming message and runs what it returns.
func receive(t *testing.T, m Model, msg messages.Message, chatName string) Model {
	t.Helper()
	next, cmd := m.Update(incomingMsg{msg: msg, chatName: chatName})
	return drain(t, next.(Model), cmd)
}

func TestNotifyIncoming(t *testing.T) {
	now := fakeClock(t)
	m, n := notifyModel(t, config.NotifyAll)
	ts := uint64(time.Now().Unix())
	direct := messages.Message{Id: "a", ChatId: "91111@s.whatsapp.net", ContactShort: "Arjun", Text: "hi there", Timestamp: ts}
	group := messages.Message{Id: "b", ChatId: groupJID, ContactShort: "Priya", Text: "[IMAGE] look", Timestamp: ts}

	m = receive(t, m, direct, "Arjun")
	if len(n.popups) != 1 || n.popups[0] != "Arjun|hi there" || n.sounds != 1 {
		t.Fatalf("direct: %v, %d sounds", n.popups, n.sounds)
	}
	// a burst: popups for each, the sound once
	m = receive(t, m, group, "Hostel")
	if len(n.popups) != 2 || n.popups[1] != "Hostel|Priya: 📷 Photo look" || n.sounds != 1 {
		t.Fatalf("group: %v, %d sounds", n.popups, n.sounds)
	}
	*now = now.Add(3 * time.Second)

	// nothing for the chat you're looking at, but for others
	n.reset()
	m, _ = keys(t, m, "enter") // opens Hostel
	m = receive(t, m, group, "Hostel")
	if len(n.popups) != 0 || n.sounds != 0 {
		t.Fatalf("open chat notified: %v", n.popups)
	}
	m = receive(t, m, direct, "Arjun")
	if len(n.popups) != 1 {
		t.Fatal("other chat didn't notify")
	}
	// ...and the open chat does when the window is in the background
	n.reset()
	*now = now.Add(3 * time.Second)
	next, _ := m.Update(tea.BlurMsg{})
	m = receive(t, next.(Model), group, "Hostel")
	if len(n.popups) != 1 || n.sounds != 1 {
		t.Fatalf("unfocused: %v, %d sounds", n.popups, n.sounds)
	}
	// your own messages never notify
	n.reset()
	*now = now.Add(3 * time.Second)
	m = receive(t, m, messages.Message{Id: "c", ChatId: groupJID, FromMe: true, Text: "me", Timestamp: ts}, "Hostel")
	if len(n.popups)+n.sounds != 0 {
		t.Fatal("own message notified")
	}
	// nor do old messages delivered late (after being offline)
	old := direct
	old.Timestamp = uint64(time.Now().Add(-10 * time.Minute).Unix())
	m = receive(t, m, old, "Arjun")
	if len(n.popups)+n.sounds != 0 {
		t.Fatal("old message notified")
	}
}

func TestNotifyModes(t *testing.T) {
	now := fakeClock(t)
	msg := messages.Message{Id: "a", ChatId: "91111@s.whatsapp.net", ContactShort: "Arjun", Text: "hi", Timestamp: uint64(time.Now().Unix())}
	for mode, want := range map[string][2]int{
		config.NotifyAll: {1, 1}, config.NotifyPopup: {1, 0}, config.NotifySound: {0, 1}, config.NotifyOff: {0, 0},
	} {
		*now = now.Add(time.Minute)
		m, n := notifyModel(t, mode)
		receive(t, m, msg, "Arjun")
		if len(n.popups) != want[0] || n.sounds != want[1] {
			t.Errorf("%s: %d popups, %d sounds; want %v", mode, len(n.popups), n.sounds, want)
		}
	}
}

func TestNotifyToggle(t *testing.T) {
	m, n := notifyModel(t, config.NotifyAll)
	status := func(m Model) string { return ansi.Strip(m.renderStatusLine()) }
	if !strings.Contains(status(m), "🔔 all") {
		t.Fatalf("no badge: %q", status(m))
	}
	// M cycles, with a sample of each mode
	var cmd tea.Cmd
	m, cmds := keys(t, m, "M")
	m = drain(t, m, tea.Batch(cmds...))
	if m.notifyMode != config.NotifyPopup || !strings.Contains(status(m), "💬 popup") || len(n.popups) != 1 || n.sounds != 0 {
		t.Fatalf("after M: %s, %v, %d sounds", m.notifyMode, n.popups, n.sounds)
	}
	// a click on the badge cycles too
	line := status(m)
	x := ansi.StringWidth(line[:strings.Index(line, "💬")])
	m, cmd = click(t, m, x+1, m.mainHeight())
	m = drain(t, m, cmd)
	if m.notifyMode != config.NotifySound || n.sounds != 1 {
		t.Fatalf("after click: %s", m.notifyMode)
	}
	m, cmd = click(t, m, 2, m.mainHeight()) // elsewhere on the status line
	if m.notifyMode != config.NotifySound {
		t.Fatal("click beside the badge changed the mode")
	}
	// :notify sets a mode, or rejects a wrong one
	m, cmds = keys(t, m, ":", "n", "o", "t", "i", "f", "y", " ", "o", "f", "f", "enter")
	if m.notifyMode != config.NotifyOff || !strings.Contains(status(m), "🔕 off") {
		t.Fatalf(":notify off: %s", m.notifyMode)
	}
	m, _ = keys(t, m, ":", "n", "o", "t", "i", "f", "y", " ", "l", "o", "u", "d", "enter")
	if m.notifyMode != config.NotifyOff || !m.noticeErr {
		t.Fatal("bad mode accepted")
	}
	// M works from inside a chat too
	m, _ = keys(t, m, "enter", "M")
	if m.notifyMode != config.NotifyAll {
		t.Fatalf("M in a chat: %s", m.notifyMode)
	}
}

func TestQuitDetachesInBackground(t *testing.T) {
	detached := 0
	m := New(make(chan messages.Command, 5), nil, Options{SidebarWidth: 38, Images: termimg.ModeOff,
		Detach: func() { detached++ }})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	isQuit := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	for _, ks := range [][]string{{"q"}, {"ctrl+c"}, {":", "q", "enter"}} {
		_, cmds := keys(t, m, ks...)
		if isQuit(cmds[len(cmds)-1]) {
			t.Fatalf("%v quit instead of detaching", ks)
		}
	}
	if detached != 3 {
		t.Fatalf("detached %d times, want 3", detached)
	}
	_, cmds := keys(t, m, ":", "q", "!", "enter")
	if !isQuit(cmds[len(cmds)-1]) {
		t.Fatal(":q! didn't quit")
	}
	// without background mode q quits as before
	m = New(make(chan messages.Command, 5), nil, Options{SidebarWidth: 38, Images: termimg.ModeOff})
	if _, cmds := keys(t, m, "q"); !isQuit(cmds[len(cmds)-1]) {
		t.Fatal("q didn't quit")
	}
}

func TestNotifyLog(t *testing.T) {
	var log strings.Builder
	m := New(make(chan messages.Command, 5), nil, Options{SidebarWidth: 38, Images: termimg.ModeOff,
		Notifications: config.NotifySound, Notifier: &fakeNotifier{}, NotifyLog: &log})
	next, _ := m.Update(tea.BlurMsg{})
	m = next.(Model)
	receive(t, m, messages.Message{Id: "x1", ChatId: "1@s.whatsapp.net", Text: "hi", Timestamp: uint64(time.Now().Unix())}, "A")
	for _, want := range []string{"window unfocused", "message x1", "sound: err=<nil>"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("log lacks %q:\n%s", want, log.String())
		}
	}
}

// progressActions reports a download in progress.
type progressActions struct {
	fakeActions
	done, total int64
	active      bool
}

func (p *progressActions) DownloadProgress(string) (int64, int64, bool) {
	return p.done, p.total, p.active
}

func TestDownloadProgress(t *testing.T) {
	now := fakeClock(t)
	a := &progressActions{total: 8 << 20, active: true}
	m := New(make(chan messages.Command, 5), nil, Options{SidebarWidth: 38, Images: termimg.ModeOff, Actions: a})
	video := messages.Message{Id: "v", MediaType: messages.MediaVideo, Media: []byte{1}}
	cmd := m.trackDownload(video)
	if cmd == nil || m.notice != "Downloading video…" {
		t.Fatalf("notice %q", m.notice)
	}
	a.done = 2 << 20
	next, cmd := m.Update(downloadTickMsg{"v"})
	m = next.(Model)
	if !strings.Contains(m.notice, "25%") || !strings.Contains(m.notice, "2.0 / 8.0 MB") || cmd == nil {
		t.Fatalf("notice %q", m.notice)
	}
	// finished: ticking stops, and the result clears it
	a.active = false
	*now = now.Add(time.Second)
	next, cmd = m.Update(downloadTickMsg{"v"})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("still ticking after the download finished")
	}
	next, _ = m.Update(videoReadyMsg{path: "/tmp/x.mp4", err: errors.New("boom")})
	m = next.(Model)
	if m.dl != nil || m.notice != "boom" {
		t.Fatalf("after done: %v %q", m.dl, m.notice)
	}
}

func TestNotifyReaction(t *testing.T) {
	fakeClock(t)
	m, n := notifyModel(t, config.NotifyPopup)
	receive(t, m, messages.Message{Id: "r1", ChatId: groupJID, ContactShort: "Priya", Timestamp: uint64(time.Now().Unix()),
		Text: "[REACTION] ❤️", QuotedID: "m1", QuotedText: "[IMAGE] dinner\nat 8?"}, "Hostel")
	if len(n.popups) != 1 || n.popups[0] != "Hostel|Priya: Reacted ❤️ to: 📷 Photo dinner at 8?" {
		t.Fatalf("popups %v", n.popups)
	}
}
