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
