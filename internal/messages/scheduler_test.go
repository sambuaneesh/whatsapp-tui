package messages

import (
	"context"
	"strings"
	"testing"
	"time"
)

func schedSM(t *testing.T) (*SessionManager, *MockUiHandler) {
	t.Helper()
	ui := NewMockUiHandler()
	sm := &SessionManager{uiHandler: ui, db: newTestDB(t), convByJID: map[string]*Conversation{
		"1@s.whatsapp.net": {JID: "1@s.whatsapp.net", Name: "Priya"},
	}, schedWake: make(chan struct{}, 1)}
	return sm, ui
}

func TestScheduledStorageOrderAndCancel(t *testing.T) {
	sm, ui := schedSM(t)
	now := time.Now().Truncate(time.Millisecond)
	late, err := sm.Schedule(context.Background(), Scheduled{Kind: ScheduleSend, Chat: "1@s.whatsapp.net", Text: "later",
		Mentions: []string{"91@s.whatsapp.net"}, Due: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = sm.Schedule(context.Background(), Scheduled{Kind: ScheduleNudge, Chat: "1@s.whatsapp.net", Due: now.Add(time.Hour)})
	items := sm.ScheduledItems()
	if len(items) != 2 || items[0].Kind != ScheduleNudge || items[1].Text != "later" || items[1].Mentions[0] != "91@s.whatsapp.net" ||
		!items[1].Due.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("items %+v", items)
	}
	if len(ui.Scheduled) != 2 {
		t.Fatal("UI not told")
	}
	if _, err := sm.Schedule(context.Background(), Scheduled{Kind: ScheduleSend, Chat: "x", Text: " "}); err == nil {
		t.Fatal("empty send accepted")
	}
	if err := sm.Unschedule(context.Background(), late.ID); err != nil {
		t.Fatal(err)
	}
	if len(sm.ScheduledItems()) != 1 || len(ui.Scheduled) != 1 {
		t.Fatal("cancel didn't remove it")
	}
}

func TestNudgeRemindsOnlyWithoutReply(t *testing.T) {
	sm, ui := schedSM(t)
	set := time.Now().Add(-3 * time.Hour)
	_, _ = sm.Schedule(context.Background(), Scheduled{Kind: ScheduleNudge, Chat: "1@s.whatsapp.net", Due: time.Now().Add(-time.Minute), Created: set})
	sm.runDue()
	if len(ui.Incomings) != 1 || !strings.Contains(ui.Incomings[0].Text, "No reply from Priya") {
		t.Fatalf("reminder %+v", ui.Incomings)
	}
	if len(sm.ScheduledItems()) != 0 {
		t.Fatal("nudge not done")
	}
	// they replied after it was set: no reminder
	ui.Incomings = nil
	_ = sm.db.AddMessage(Message{Id: "r", ChatId: "1@s.whatsapp.net", Text: "sorry, here", Timestamp: uint64(set.Add(time.Hour).Unix())})
	_, _ = sm.Schedule(context.Background(), Scheduled{Kind: ScheduleNudge, Chat: "1@s.whatsapp.net", Due: time.Now().Add(-time.Minute), Created: set})
	sm.runDue()
	if len(ui.Incomings) != 0 {
		t.Fatalf("reminded although they replied: %+v", ui.Incomings)
	}
}

func TestScheduledSendWaitsForConnection(t *testing.T) {
	sm, _ := schedSM(t) // no WhatsApp connection
	_, _ = sm.Schedule(context.Background(), Scheduled{Kind: ScheduleSend, Chat: "1@s.whatsapp.net", Text: "hi", Due: time.Now().Add(-time.Second)})
	sm.runDue()
	items := sm.ScheduledItems()
	if len(items) != 1 || time.Until(items[0].Due) < 20*time.Second {
		t.Fatalf("not retried later: %+v", items)
	}
}

// chanUI hands reminders over to the test (they come from the scheduler's
// goroutine).
type chanUI struct {
	*MockUiHandler
	got chan Message
}

func (c chanUI) Incoming(m Message, _ string) { c.got <- m }

func TestSchedulerWakesForNewItems(t *testing.T) {
	sm, _ := schedSM(t)
	ui := chanUI{NewMockUiHandler(), make(chan Message, 1)}
	sm.uiHandler = ui
	stop := make(chan struct{})
	defer close(stop)
	go sm.runScheduler(stop)
	time.Sleep(20 * time.Millisecond) // asleep, nothing due
	start := time.Now()
	_, _ = sm.Schedule(context.Background(), Scheduled{Kind: ScheduleNudge, Chat: "1@s.whatsapp.net",
		Due: start.Add(1100 * time.Millisecond), Created: start.Add(-time.Hour)})
	select {
	case m := <-ui.got:
		if took := time.Since(start); took < time.Second || !strings.Contains(m.Text, "No reply") {
			t.Fatalf("after %v: %q", took, m.Text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the scheduler didn't wake for the new item")
	}
}
