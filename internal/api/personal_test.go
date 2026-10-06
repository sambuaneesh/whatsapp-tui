package api

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/Srindot/whatsapp-tui/internal/personal"
)

func TestListsOverAPI(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	db.SetMaxOpenConns(1)
	ps, err := personal.OpenDB(db)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Personal: ps}
	call := func(method, params string) any {
		t.Helper()
		res, err := Local(opts, method, json.RawMessage(params))
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return res
	}
	call("list_add", `{"text": "Shopping", "icon": "🛒"}`)
	it := call("task_add", `{"text": "shopping: eggs tomorrow 9am #breakfast"}`).(ItemJSON)
	if it.List != "Shopping" || it.Text != "eggs" || it.Due == "" || !it.DueTime || it.Tags[0] != "breakfast" {
		t.Fatalf("task_add: %+v", it)
	}
	call("task_add", `{"text": "milk", "list": "Shopping"}`)
	tasks := call("tasks", `{"list": "shopping"}`).([]ItemJSON)
	if len(tasks) != 2 {
		t.Fatalf("tasks: %+v", tasks)
	}
	call("task_done", `{"id": `+itoa(tasks[1].ID)+`}`)
	if tasks := call("tasks", `{"list": "Shopping"}`).([]ItemJSON); len(tasks) != 1 {
		t.Fatalf("done should hide it: %+v", tasks)
	}
	upd := call("task_update", `{"id": `+itoa(it.ID)+`, "due": "none"}`).(ItemJSON)
	if upd.Due != "" {
		t.Fatal("due none")
	}
	call("note_add", `{"title": "Wifi", "text": "hunter2"}`)
	if notes := call("notes", `{}`).([]ItemJSON); len(notes) != 1 || notes[0].Notes != "hunter2" {
		t.Fatalf("notes: %+v", notes)
	}
	lists := call("lists", ``).([]ListJSON)
	if len(lists) != 4 {
		t.Fatalf("lists: %+v", lists)
	}
	if _, err := Local(opts, "send", nil); err == nil {
		t.Fatal("chat methods need the app")
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
