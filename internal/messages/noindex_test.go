package messages

import (
	"strings"
	"testing"
)

func TestTurningIndexingOffForAChat(t *testing.T) {
	md := newTestDB(t)
	addTestMsg(t, md, Message{Id: "a1", ChatId: "work@g.us", Text: "invoice for the flat", Timestamp: 100})
	addTestMsg(t, md, Message{Id: "b1", ChatId: "spam@g.us", Text: "flat sale flat sale", Timestamp: 101})
	md.SaveEmbeddings(map[string][]byte{"a1": {1}, "b1": {2}})
	ids := func(chat, q string) string {
		t.Helper()
		msgs, err := md.SearchMessages(chat, q, 10)
		if err != nil {
			t.Fatal(err)
		}
		var s []string
		for _, m := range msgs {
			s = append(s, m.Id)
		}
		return strings.Join(s, ",")
	}
	check := func() {
		t.Helper()
		if md.fts {
			if _, err := md.db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES ('integrity-check')`); err != nil {
				t.Fatalf("index broken: %v", err)
			}
		}
	}
	if got := ids("", "flat"); got != "b1,a1" {
		t.Fatalf("before: %s", got)
	}
	if !md.Indexed("spam@g.us") {
		t.Fatal("chats are indexed unless turned off")
	}

	if err := md.SetIndexed("spam@g.us", false); err != nil {
		t.Fatal(err)
	}
	check()
	if got := ids("", "flat"); got != "a1" {
		t.Fatalf("all-chats search should leave it out: %s", got)
	}
	// new messages there aren't indexed either; edits and deletes are safe
	addTestMsg(t, md, Message{Id: "b2", ChatId: "spam@g.us", Text: "another flat offer", Timestamp: 102})
	md.db.Exec(`UPDATE messages SET text = 'edited flat' WHERE id = 'b1'`)
	md.db.Exec(`DELETE FROM messages WHERE id = 'b2'`)
	check()
	if got := ids("", "flat"); got != "a1" {
		t.Fatalf("after new messages: %s", got)
	}
	// "/" inside the chat still finds things (by plain matching)
	if got := ids("spam@g.us", "flat"); got != "b1" {
		t.Fatalf("in-chat search: %s", got)
	}
	// its meaning vectors are gone, and it isn't queued for new ones
	var n int
	md.db.QueryRow(`SELECT COUNT(*) FROM embeddings WHERE msg_id = 'b1'`).Scan(&n)
	if n != 0 {
		t.Fatal("vectors kept")
	}
	todo, _ := md.UnembeddedMessages(10)
	for _, m := range todo {
		if m.ChatId == "spam@g.us" {
			t.Fatal("queued for meaning vectors")
		}
	}
	if list, _ := md.NotIndexed(); len(list) != 1 || list[0] != "spam@g.us" {
		t.Fatalf("list: %v", list)
	}

	// back on: found again
	if err := md.SetIndexed("spam@g.us", true); err != nil {
		t.Fatal(err)
	}
	check()
	if got := ids("", "flat"); got != "b1,a1" {
		t.Fatalf("back on: %s", got)
	}
}
