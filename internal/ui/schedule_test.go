package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

type fakeScheduler struct {
	mu    sync.Mutex
	items []messages.Scheduled
	next  int64
}

func (f *fakeScheduler) Schedule(_ context.Context, it messages.Scheduled) (messages.Scheduled, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	it.ID = f.next
	f.items = append(f.items, it)
	return it, nil
}

func (f *fakeScheduler) Unschedule(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, it := range f.items {
		if it.ID == id {
			f.items = append(f.items[:i], f.items[i+1:]...)
			return nil
		}
	}
	return errors.New("gone")
}

func (f *fakeScheduler) ScheduledItems() []messages.Scheduled {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]messages.Scheduled(nil), f.items...)
}

// typeCmd types a command line and runs it.
func typeCmd(t *testing.T, m Model, line string) Model {
	t.Helper()
	ks := []string{":"}
	for _, r := range line {
		ks = append(ks, string(r))
	}
	ks = append(ks, "enter")
	m, cmds := keys(t, m, ks...)
	return runAll(t, m, cmds...)
}

func TestSendLaterSnoozeNudge(t *testing.T) {
	now := fakeClock(t)
	*now = time.Date(2026, 10, 7, 14, 0, 0, 0, time.Local)
	f := &fakeScheduler{}
	m := triageModel(t, &fakeTriage{})
	m.scheduler = f
	m, _ = keys(t, m, "enter", "i", "s", "e", "e", " ", "y", "o", "u", "esc")
	m = typeCmd(t, m, "later tomorrow 9am")
	if len(f.items) != 1 || f.items[0].Kind != messages.ScheduleSend || f.items[0].Text != "see you" ||
		f.items[0].Chat != "a@s.whatsapp.net" || !f.items[0].Due.Equal(time.Date(2026, 10, 8, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("send: %+v", f.items)
	}
	if m.compose.Value() != "" || !strings.Contains(m.notice, "Will send to Arjun tomorrow 09:00") {
		t.Fatalf("box %q notice %q", m.compose.Value(), m.notice)
	}
	// nothing written: :later says so
	m = typeCmd(t, m, "later 5pm")
	if !m.noticeErr || len(f.items) != 1 {
		t.Fatal("scheduled an empty message")
	}
	m = typeCmd(t, m, "nudge in 3h")
	if len(f.items) != 2 || f.items[1].Kind != messages.ScheduleNudge || !f.items[1].Due.Equal(now.Add(3*time.Hour)) {
		t.Fatalf("nudge: %+v", f.items)
	}
	m = typeCmd(t, m, "snooze fri")
	if len(f.items) != 3 || f.items[2].Kind != messages.ScheduleSnooze || m.screen != screenList {
		t.Fatalf("snooze: %+v (screen %v)", f.items, m.screen)
	}
	m = typeCmd(t, m, "snooze someday")
	if !m.noticeErr || len(f.items) != 3 {
		t.Fatal("bad time accepted")
	}
	// the list: badge, panel, cancel
	next, _ := m.Update(scheduledMsg(f.ScheduledItems()))
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.renderStatusLine()), "⏰ 3") {
		t.Fatalf("no badge: %q", ansi.Strip(m.renderStatusLine()))
	}
	m = typeCmd(t, m, "scheduled")
	v := ansi.Strip(m.View())
	for _, want := range []string{"SCHEDULED", "tomorrow 09:00", "send  see you", "remind if no reply", "back from snooze"} {
		if !strings.Contains(v, want) {
			t.Fatalf("panel lacks %q:\n%s", want, v)
		}
	}
	m, cmds := keys(t, m, "x")
	m = runAll(t, m, cmds...)
	if len(f.items) != 2 {
		t.Fatalf("cancel: %+v", f.items)
	}
}

func TestReminderNotification(t *testing.T) {
	title, body := notificationText(messages.Message{ChatId: "1@s.whatsapp.net", Text: "[REMINDER] ⏰ No reply from Priya since Mon 14:00"}, "Priya")
	if title != "Priya" || body != "⏰ No reply from Priya since Mon 14:00" {
		t.Fatalf("%q %q", title, body)
	}
}
