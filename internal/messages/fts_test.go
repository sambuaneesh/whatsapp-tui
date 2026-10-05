package messages

import (
	"testing"
)

func TestFullTextSearch(t *testing.T) {
	md := newTestDB(t)
	if !md.fts {
		t.Skip("SQLite without FTS5: build with -tags sqlite_fts5 (make test does)")
	}
	for i, text := range []string{"the address of the flat is 12B", "flat white coffee?", "Café tomorrow",
		"my apartment number", "nothing here"} {
		_ = md.AddMessage(Message{Id: string(rune('a' + i)), ChatId: "c", Text: text, Timestamp: uint64(100 + i)})
	}
	ids := func(q string) string {
		msgs, err := md.SearchMessages("c", q, 10)
		if err != nil {
			t.Fatal(err)
		}
		s := ""
		for _, m := range msgs {
			s += m.Id
		}
		return s
	}
	for q, want := range map[string]string{
		"flat addr":   "a",  // all words, any order, word beginnings
		"FLAT":        "ba", // newest first, any case
		"cafe":        "c",  // accents don't matter
		"partment":    "d",  // inside a word: falls back to substring
		"12b":         "a",
		`"the" flat*`: "a", // query syntax is neutralised
		"zzz":         "",
	} {
		if got := ids(q); got != want {
			t.Errorf("%q: %q, want %q", q, got, want)
		}
	}
	// the index follows edits and deletes
	_, _ = md.EditMessage("e", "flat hunting")
	_ = md.DeleteMessage("b")
	if got := ids("flat"); got != "ea" {
		t.Errorf("after edit/delete: %q", got)
	}
}

func TestFTSTriggersDroppedWithoutFTS5(t *testing.T) {
	md := newTestDB(t)
	if !md.fts {
		t.Skip("needs FTS5 to set up the triggers first")
	}
	// a build without FTS5 opening this database: simulate by dropping the
	// module's table so the triggers would fail, then re-init without it
	md.db.Exec(`DROP TABLE messages_fts`)
	md.db.Exec(`INSERT INTO meta (key, value) VALUES ('fts_built', '0') ON CONFLICT(key) DO UPDATE SET value='0'`)
	for _, tr := range []string{"messages_fts_ai", "messages_fts_ad", "messages_fts_au"} {
		md.db.Exec(`DROP TRIGGER IF EXISTS ` + tr)
	}
	md.initFTS() // rebuilds it
	if err := md.AddMessage(Message{Id: "x", ChatId: "c", Text: "still saves"}); err != nil {
		t.Fatal(err)
	}
	if msgs, _ := md.SearchMessages("c", "saves", 5); len(msgs) != 1 {
		t.Fatal("rebuilt index doesn't find new messages")
	}
}

func TestNoFTS5BuildStillSavesMessages(t *testing.T) {
	md := newTestDB(t)
	if md.fts {
		t.Skip("this build has FTS5; run without -tags sqlite_fts5")
	}
	// a database a build with FTS5 left behind: triggers feed the index
	for _, tr := range ftsTriggers {
		if _, err := md.db.Exec(tr); err != nil {
			t.Fatal(err)
		}
	}
	if err := md.AddMessage(Message{Id: "x", ChatId: "c", Text: "hi"}); err == nil {
		t.Fatal("expected the stale trigger to break inserts (test setup)")
	}
	md.initFTS() // what opening it does
	if err := md.AddMessage(Message{Id: "y", ChatId: "c", Text: "still saves"}); err != nil {
		t.Fatalf("insert after init: %v", err)
	}
	if msgs, _ := md.SearchMessages("c", "saves", 5); len(msgs) != 1 {
		t.Fatal("substring search fallback broken")
	}
}
