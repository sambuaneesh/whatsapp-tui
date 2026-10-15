package ui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

type receiptActions struct {
	fakeActions
	info messages.MessageInfo
}

func (r *receiptActions) MessageReceipts(context.Context, messages.Message) (messages.MessageInfo, error) {
	return r.info, nil
}

func TestMessageInfoScreen(t *testing.T) {
	now := time.Now().UnixMilli()
	a := &receiptActions{info: messages.MessageInfo{Group: true,
		Read:      []messages.Receipt{{User: "1", Name: "Priya", Delivered: now - 60000, Read: now - 30000}},
		Delivered: []messages.Receipt{{User: "2", Name: "Ravi", Delivered: now - 50000}},
		Waiting:   []messages.Receipt{{User: "3", Name: "Arjun"}},
	}}
	m := visualModel(t, &a.fakeActions, fakeClip{})
	m.actions = a
	// "count me in" is yours and the newest: v selects it
	m, cmds := keys(t, m, "v", "i")
	if m.minfo == nil {
		t.Fatal("i should open message info")
	}
	m = runAll(t, m, cmds...)
	v := stripANSI(m.View())
	for _, want := range []string{"Message info", "Read by  1", "Priya", "Delivered to  1", "Ravi", "Not delivered yet  1", "Arjun",
		"turn blue when everyone has read it"} {
		if !strings.Contains(v, want) {
			t.Fatalf("%q missing:\n%s", want, v)
		}
	}
	if os.Getenv("WT_PEEK") != "" {
		os.WriteFile(os.Getenv("WT_PEEK"), []byte(v), 0o644)
	}
	m, _ = keys(t, m, "esc")
	if m.minfo != nil {
		t.Fatal("esc closes")
	}
	// someone else's message (still selecting: k to it): it says it's for yours
	m, _ = keys(t, m, "k", "i")
	if m.minfo != nil || !m.noticeErr {
		t.Fatal("info on someone else's message")
	}
}

func TestPollShowsVotes(t *testing.T) {
	a := &receiptActions{}
	m := visualModel(t, &a.fakeActions, fakeClip{})
	m.actions = a
	poll := &messages.Poll{Question: "Dinner?", Voters: 3, Max: 1, Options: []messages.PollOption{
		{Name: "Pizza", Mine: true, Voters: []messages.PollVoter{{Name: "You"}, {User: "1", Name: "Priya"}}},
		{Name: "Dosa", Voters: []messages.PollVoter{{User: "2", Name: "Ravi"}}},
	}}
	msgs := groupMsgs()
	last := &msgs[len(msgs)-1]
	last.Text, last.Poll, last.FromMe = "[POLL] Dinner?\n○ Pizza\n○ Dosa", poll, false
	next, _ := m.Update(screenMsg(msgs))
	m = next.(Model)
	v := stripANSI(m.View())
	for _, want := range []string{"📊 Dinner?", "● Pizza", "○ Dosa", "3 votes", "━"} {
		if !strings.Contains(v, want) {
			t.Fatalf("bubble missing %q:\n%s", want, v)
		}
	}
	m, _ = keys(t, m, "v", "i")
	if m.minfo == nil {
		t.Fatal("i on a poll should list the votes")
	}
	v = stripANSI(m.View())
	for _, want := range []string{"Poll", "Pizza  2", "Priya", "Dosa  1", "Ravi"} {
		if !strings.Contains(v, want) {
			t.Fatalf("poll info missing %q:\n%s", want, v)
		}
	}
}
