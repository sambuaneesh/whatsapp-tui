package ui

import (
	"strings"
	"testing"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

func TestViewOnceShown(t *testing.T) {
	m := visualModel(t, &fakeActions{}, fakeClip{})
	msgs := append(groupMsgs(), messages.Message{Id: "v1", ChatId: groupJID, ContactId: "91111@s.whatsapp.net",
		ContactShort: "Arjun", Timestamp: 1700000300, Text: messages.ViewOnceTag + " photo"})
	next, _ := m.Update(screenMsg(msgs))
	m = next.(Model)
	v := stripANSI(m.View())
	if !strings.Contains(v, "👁 View once photo") || !strings.Contains(v, "opens only on your phone") {
		t.Fatalf("view once not shown:\n%s", v)
	}
	if title, body := notificationText(msgs[3], "Hostel"); title != "Hostel" || body != "Arjun: 👁 View once photo" {
		t.Fatalf("notification %q %q", title, body)
	}
}
