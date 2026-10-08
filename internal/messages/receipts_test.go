package messages

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestGroupStatusNeedsEveryone(t *testing.T) {
	members := []string{"a", "b", "c"}
	rs := map[string]Receipt{"a": {Read: 1}}
	if groupStatus(members, rs) != StatusSent {
		t.Fatal("one read, two with nothing: sent")
	}
	rs["b"] = Receipt{Delivered: 1}
	rs["c"] = Receipt{Delivered: 1}
	if groupStatus(members, rs) != StatusDelivered {
		t.Fatal("all have it: delivered")
	}
	rs["b"] = Receipt{Read: 1}
	if groupStatus(members, rs) != StatusDelivered {
		t.Fatal("two read, one only delivered: still delivered")
	}
	rs["c"] = Receipt{Played: 1}
	if groupStatus(members, rs) != StatusRead {
		t.Fatal("all read: read")
	}
}

func receiptSM(t *testing.T) *SessionManager {
	t.Helper()
	sm := &SessionManager{uiHandler: NewMockUiHandler(), db: newTestDB(t), convByJID: map[string]*Conversation{}}
	sm.groupMembers.byID = map[string]membersEntry{
		"g@g.us": {users: []string{"111@s.whatsapp.net", "222@s.whatsapp.net"}, at: time.Now()},
	}
	return sm
}

func TestGroupReceiptsTurnBlueOnlyWhenAllRead(t *testing.T) {
	sm := receiptSM(t)
	addTestMsg(t, sm.db, Message{Id: "m1", ChatId: "g@g.us", FromMe: true, Text: "dinner?", Timestamp: 100, Status: StatusSent})
	status := func() int {
		m, _ := sm.db.GetMessage("m1")
		return m.Status
	}
	receipt := func(user string, typ types.ReceiptType) {
		jid, _ := types.ParseJID(user)
		chat, _ := types.ParseJID("g@g.us")
		evt := &events.Receipt{MessageSource: types.MessageSource{Chat: chat, Sender: jid, IsGroup: true},
			MessageIDs: []types.MessageID{"m1"}, Timestamp: time.Now(), Type: typ}
		st := map[types.ReceiptType]int{types.ReceiptTypeDelivered: StatusDelivered, types.ReceiptTypeRead: StatusRead}[typ]
		for _, id := range evt.MessageIDs {
			sm.db.AddReceipt(id, sm.receiptUser(context.Background(), evt.Sender), st, evt.Timestamp.UnixMilli())
		}
		sm.updateGroupTicks("g@g.us", evt.MessageIDs)
	}
	receipt("111@s.whatsapp.net", types.ReceiptTypeRead)
	if status() != StatusSent {
		t.Fatalf("one of two read: status %d, want sent", status())
	}
	receipt("222@s.whatsapp.net", types.ReceiptTypeDelivered)
	if status() != StatusDelivered {
		t.Fatalf("both have it: %d, want delivered", status())
	}
	receipt("222@s.whatsapp.net", types.ReceiptTypeRead)
	if status() != StatusRead {
		t.Fatalf("both read: %d, want read", status())
	}
	info, err := sm.MessageReceipts(context.Background(), Message{Id: "m1", ChatId: "g@g.us", FromMe: true})
	if err != nil || len(info.Read) != 2 || len(info.Waiting) != 0 || !info.Group {
		t.Fatalf("info: %+v %v", info, err)
	}
}

func TestMessageInfoShowsWhoIsWaiting(t *testing.T) {
	sm := receiptSM(t)
	addTestMsg(t, sm.db, Message{Id: "m2", ChatId: "g@g.us", FromMe: true, Text: "hi", Timestamp: 100})
	sm.db.AddReceipt("m2", "111@s.whatsapp.net", StatusRead, 5000)
	sm.db.AddReceipt("m2", "111@s.whatsapp.net", StatusDelivered, 3000) // earlier delivery kept
	info, _ := sm.MessageReceipts(context.Background(), Message{Id: "m2", ChatId: "g@g.us", FromMe: true})
	if len(info.Read) != 1 || info.Read[0].User != "111@s.whatsapp.net" || info.Read[0].Delivered != 3000 || info.Read[0].Read != 5000 {
		t.Fatalf("read: %+v", info.Read)
	}
	if len(info.Waiting) != 1 || info.Waiting[0].User != "222@s.whatsapp.net" {
		t.Fatalf("waiting: %+v", info.Waiting)
	}
}

func TestOldGroupReadsAreDowngraded(t *testing.T) {
	md := newTestDB(t)
	addTestMsg(t, md, Message{Id: "old", ChatId: "g@g.us", FromMe: true, Text: "x", Timestamp: 1, Status: StatusRead})
	addTestMsg(t, md, Message{Id: "dm", ChatId: "1@s.whatsapp.net", FromMe: true, Text: "y", Timestamp: 1, Status: StatusRead})
	md.db.Exec(`DELETE FROM meta WHERE key = 'group_receipts'`)
	if err := md.initReceipts(); err != nil {
		t.Fatal(err)
	}
	if m, _ := md.GetMessage("old"); m.Status != StatusDelivered {
		t.Fatalf("old group read: %d", m.Status)
	}
	if m, _ := md.GetMessage("dm"); m.Status != StatusRead {
		t.Fatal("one-to-one reads are right and stay")
	}
}
