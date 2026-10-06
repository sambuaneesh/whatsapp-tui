package personal

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s, err := OpenDB(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Monday 6 Oct 2026, 14:00
var monday = time.Date(2026, 10, 5, 14, 0, 0, 0, time.Local)

func TestParseTask(t *testing.T) {
	day := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.Local) }
	cases := []struct {
		in        string
		text      string
		due       time.Time
		dueTime   bool
		important bool
		tags      string
	}{
		{"send report to Arjun by 5pm", "send report to Arjun", day(5, 17, 0), true, false, ""},
		{"dentist fri 10am", "dentist", day(9, 10, 0), true, false, ""},
		{"buy milk tomorrow #home", "buy milk", day(6, 0, 0), false, false, "home"},
		{"call the bank !", "call the bank", time.Time{}, false, true, ""},
		{"pay rent 12 oct", "pay rent", day(12, 0, 0), false, false, ""},
		{"finish slides today", "finish slides", day(5, 0, 0), false, false, ""},
		{"tomorrow 9am gym #health #morning", "gym", day(6, 9, 0), true, false, "health morning"},
		{"call mom at 5", "call mom", day(5, 17, 0), true, false, ""},
		{"buy 2 eggs", "buy 2 eggs", time.Time{}, false, false, ""},
		{"enjoy the sun", "enjoy the sun", time.Time{}, false, false, ""},
		{"meet priya on sat", "meet priya", day(10, 0, 0), false, false, ""},
		{"water plants in 2h", "water plants", day(5, 16, 0), true, false, ""},
		{"read the book", "read the book", time.Time{}, false, false, ""},
	}
	for _, c := range cases {
		p := ParseTask(c.in, monday, nil)
		if p.Text != c.text || !p.Due.Equal(c.due) || p.DueTime != c.dueTime || p.Important != c.important || strings.Join(p.Tags, " ") != c.tags {
			t.Errorf("%q → %q due %v (time %v) ! %v tags %v", c.in, p.Text, p.Due, p.DueTime, p.Important, p.Tags)
		}
	}
}

func TestParseChecklistAndList(t *testing.T) {
	p := ParseTask("Trip packing:\n- passport\n- [x] charger\n* tickets fri", monday, nil)
	if p.Text != "Trip packing" || len(p.Items) != 3 || !p.Items[1].Done || p.Items[2].Text != "tickets" || p.Items[2].Due.IsZero() {
		t.Fatalf("checklist: %+v", p)
	}
	p = ParseTask("shopping: eggs tomorrow", monday, []string{"Inbox", "Shopping"})
	if p.List != "Shopping" || p.Text != "eggs" || p.Due.IsZero() {
		t.Fatalf("list prefix: %+v", p)
	}
	if p := ParseTask("note: this isn't a list", monday, []string{"Inbox"}); p.List != "" || p.Text != "note: this isn't a list" {
		t.Fatalf("unknown prefix: %+v", p)
	}
}

func TestFindDate(t *testing.T) {
	if d, hasTime, ok := FindDate("tomorrow 9am", monday); !ok || !hasTime || d.Day() != 6 || d.Hour() != 9 {
		t.Fatal("tomorrow 9am")
	}
	if _, _, ok := FindDate("whenever", monday); ok {
		t.Fatal("whenever isn't a date")
	}
}

func TestStoreListsAndItems(t *testing.T) {
	s := testStore(t)
	s.SetClock(func() time.Time { return monday })
	lists, _ := s.Lists()
	if len(lists) != 3 {
		t.Fatalf("defaults: %d lists", len(lists))
	}
	shop, err := s.CreateList("Shopping", KindTasks, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateList("shopping", KindTasks, ""); err == nil {
		t.Fatal("duplicate name allowed")
	}
	a, _ := s.AddItem(Item{ListID: shop.ID, Text: "eggs"})
	b, _ := s.AddItem(Item{ListID: shop.ID, Text: "milk", Due: monday.Add(time.Hour).Unix(), DueTime: true})
	c, _ := s.AddItem(Item{ListID: shop.ID, Text: "bread"})
	// reorder: bread to the top
	if err := s.Move(c.ID, -2); err != nil {
		t.Fatal(err)
	}
	items, _ := s.Items(shop.ID)
	if items[0].Text != "bread" || items[1].Text != "eggs" {
		t.Fatalf("order after move: %v %v %v", items[0].Text, items[1].Text, items[2].Text)
	}
	// indent milk under eggs, then out again
	if err := s.Indent(b.ID); err == nil {
		// milk is under eggs now
		kids, _ := s.Children(a.ID)
		if len(kids) != 1 || kids[0].Text != "milk" {
			t.Fatalf("indent: %v", kids)
		}
	} else {
		t.Fatal(err)
	}
	if err := s.Outdent(b.ID); err != nil {
		t.Fatal(err)
	}
	tree := Tree(mustItems(t, s, shop.ID))
	if tree[2].Text != "milk" || tree[2].ParentID != 0 {
		t.Fatalf("outdent: %+v", tree)
	}
	// done, then undo
	s.SetDone(a.ID, true)
	if it, _ := s.Item(a.ID); !it.Done || it.DoneAt == 0 {
		t.Fatal("done")
	}
	if label, err := s.Undo(); err != nil || !strings.Contains(label, "eggs") {
		t.Fatalf("undo: %q %v", label, err)
	}
	if it, _ := s.Item(a.ID); it.Done {
		t.Fatal("undo didn't un-tick")
	}
	// delete a list, undo brings it back with its items
	if err := s.DeleteList(shop.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ListByName("Shopping"); ok {
		t.Fatal("not deleted")
	}
	s.Undo()
	if l, ok := s.ListByName("shopping"); !ok || len(mustItems(t, s, l.ID)) != 3 {
		t.Fatal("undo delete list")
	}
}

func mustItems(t *testing.T, s *Store, list int64) []Item {
	t.Helper()
	items, err := s.Items(list)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestChecklistAndDueReminders(t *testing.T) {
	s := testStore(t)
	now := monday
	s.SetClock(func() time.Time { return now })
	inbox, _ := s.ListOfKind(KindTasks, InboxName)
	head, err := s.AddChecklist(Item{ListID: inbox.ID, Text: "Trip"}, []Item{{Text: "passport"}, {Text: "charger", Done: true}})
	if err != nil {
		t.Fatal(err)
	}
	kids, _ := s.Children(head.ID)
	if len(kids) != 2 || !kids[1].Done {
		t.Fatalf("checklist: %+v", kids)
	}
	// undo removes the whole checklist
	s.Undo()
	if items := mustItems(t, s, inbox.ID); len(items) != 0 {
		t.Fatalf("undo checklist left %d", len(items))
	}
	call, _ := s.AddItem(Item{ListID: inbox.ID, Text: "call", Due: monday.Add(30 * time.Minute).Unix(), DueTime: true})
	s.AddItem(Item{ListID: inbox.ID, Text: "someday"})
	if r, _ := s.DueReminders(now); len(r) != 0 {
		t.Fatal("too early")
	}
	now = monday.Add(31 * time.Minute)
	r, _ := s.DueReminders(now)
	if len(r) != 1 || r[0].ID != call.ID {
		t.Fatalf("due: %+v", r)
	}
	s.MarkReminded(call.ID)
	if r, _ := s.DueReminders(now); len(r) != 0 {
		t.Fatal("reminded twice")
	}
	// moving it later means a new reminder
	it, _ := s.Item(call.ID)
	it.Due = now.Add(time.Hour).Unix()
	s.Update(it)
	now = now.Add(2 * time.Hour)
	if r, _ := s.DueReminders(now); len(r) != 1 {
		t.Fatal("rescheduled task should remind again")
	}
	// Today: dated, open tasks up to tonight
	today, _ := s.Dated(time.Date(2026, 10, 5, 23, 59, 59, 0, time.Local))
	if len(today) != 1 || today[0].Text != "call" {
		t.Fatalf("today: %+v", today)
	}
}

func TestSearchAndClearDone(t *testing.T) {
	s := testStore(t)
	inbox, _ := s.ListOfKind(KindTasks, InboxName)
	s.AddItem(Item{ListID: inbox.ID, Text: "Book train tickets", Tags: []string{"trip"}})
	x, _ := s.AddItem(Item{ListID: inbox.ID, Text: "old thing", Done: true})
	if r, _ := s.Search("tickets trip"); len(r) != 1 {
		t.Fatalf("search: %v", r)
	}
	if n, _ := s.ClearDone(inbox.ID); n != 1 {
		t.Fatal("clear done")
	}
	if _, err := s.Item(x.ID); err == nil {
		t.Fatal("still there")
	}
	s.Undo()
	if _, err := s.Item(x.ID); err != nil {
		t.Fatal("undo clear")
	}
}
