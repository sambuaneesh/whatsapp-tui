package personal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskLineRoundTrip(t *testing.T) {
	due := time.Date(2026, 10, 7, 18, 0, 0, 0, time.Local)
	it := Item{ID: 12, Text: "call mom", Tags: []string{"family"}, Important: true, Due: due.Unix(), DueTime: true}
	line := TaskLine(it)
	if line != "- [ ] call mom #family ⏫ 📅 2026-10-07 ⏰ 18:00 ^t12" {
		t.Fatalf("line %q", line)
	}
	got, depth, ok := ParseTaskLine("\t" + line)
	if !ok || depth != 1 || got.ID != 12 || got.Text != "call mom" || !got.Important || got.Due != due.Unix() || !got.DueTime || got.Tags[0] != "family" {
		t.Fatalf("parsed %+v depth %d", got, depth)
	}
	// Obsidian Tasks marks we don't use are dropped, not kept in the text
	got, _, _ = ParseTaskLine("- [x] water plants 🔁 every week ⏳ 2026-10-01 ✅ 2026-10-06")
	if got.Text != "water plants" || !got.Done || got.DoneAt == 0 {
		t.Fatalf("other marks: %+v", got)
	}
	if _, _, ok := ParseTaskLine("just a line"); ok {
		t.Fatal("not a task")
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMirrorBothWays(t *testing.T) {
	s := testStore(t)
	dir := t.TempDir()
	shop, _ := s.CreateList("Shopping", KindTasks, "🛒")
	eggs, _ := s.AddItem(Item{ListID: shop.ID, Text: "eggs"})
	s.AddChecklist(Item{ListID: shop.ID, Text: "Party"}, []Item{{Text: "cake"}, {Text: "candles", Done: true}})
	notes, _ := s.ListOfKind(KindNotes, NotesName)
	s.AddItem(Item{ListID: notes.ID, Text: "Wifi", Body: "password: hunter2"})

	mi := NewMirror(s, dir)
	if err := mi.Sync(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "Shopping.md")
	got := read(t, file)
	for _, want := range []string{"list-id:", "icon: 🛒", "- [ ] eggs ^t", "- [ ] Party ^t", "\t- [ ] cake ^t", "\t- [x] candles ✅"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
	if page := read(t, filepath.Join(dir, "Notes", "Wifi.md")); !strings.Contains(page, "password: hunter2") {
		t.Fatalf("page: %s", page)
	}

	// edits in Obsidian: tick eggs, add a line, drop cake, a date
	edited := strings.Replace(got, "- [ ] eggs ^t", "- [x] eggs ^t", 1)
	edited = strings.Replace(edited, "\t- [ ] cake", "\t- [ ] DROPPED", 1)
	lines := strings.Split(edited, "\n")
	var out []string
	for _, l := range lines {
		if strings.Contains(l, "DROPPED") {
			continue
		}
		out = append(out, l)
	}
	edited = strings.Join(out, "\n") + "- [ ] milk #dairy 📅 2026-10-09\n"
	if err := os.WriteFile(file, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mi.Sync(); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.Item(eggs.ID); !it.Done {
		t.Fatal("eggs not ticked from the file")
	}
	items, _ := s.Items(shop.ID)
	var texts []string
	for _, it := range Tree(items) {
		texts = append(texts, it.Text)
	}
	if strings.Join(texts, ",") != "eggs,Party,candles,milk" {
		t.Fatalf("after file edits: %v", texts)
	}
	// the new line got an id written back
	if !strings.Contains(read(t, file), "milk #dairy 📅 2026-10-09 ^t") {
		t.Fatalf("milk without id:\n%s", read(t, file))
	}

	// edits here show in the file
	s.SetDone(eggs.ID, false)
	mi.Sync()
	if !strings.Contains(read(t, file), "- [ ] eggs ^t") {
		t.Fatal("unticked eggs not written")
	}

	// a page edited in Obsidian
	os.WriteFile(filepath.Join(dir, "Notes", "Wifi.md"), []byte("---\npage-id: 99999\n---\nnew password: swordfish\n"), 0o644)
	mi.Sync()
	pages, _ := s.Items(notes.ID)
	if len(pages) != 1 || pages[0].Body != "new password: swordfish" {
		t.Fatalf("page from file: %+v", pages)
	}

	// a new list made in Obsidian
	os.WriteFile(filepath.Join(dir, "Books.md"), []byte("- [ ] Dune\n- [ ] Hyperion\n"), 0o644)
	mi.Sync()
	books, ok := s.ListByName("Books")
	if !ok {
		t.Fatal("new file should make a list")
	}
	if items, _ := s.Items(books.ID); len(items) != 2 {
		t.Fatalf("books: %d", len(items))
	}

	// a list's file deleted: the list is archived, not deleted
	os.Remove(file)
	mi.Sync()
	if l, ok := s.ListByName("Shopping"); !ok || !l.Archived {
		t.Fatalf("deleted file: %+v %v", l, ok)
	}

	// renamed here: the old file goes, the new one comes
	b, _ := s.ListByName("Books")
	b.Name = "Reading"
	s.UpdateList(b)
	mi.Sync()
	if _, err := os.Stat(filepath.Join(dir, "Books.md")); err == nil {
		t.Fatal("old file kept")
	}
	if !strings.Contains(read(t, filepath.Join(dir, "Reading.md")), "Dune") {
		t.Fatal("renamed file")
	}

	// a fresh mirror (the app restarted) doesn't re-import unchanged files
	before := s.Version()
	mi2 := NewMirror(s, dir)
	mi2.Sync()
	if items, _ := s.Items(b.ID); len(items) != 2 {
		t.Fatalf("restart duplicated tasks: %d", len(items))
	}
	_ = before
}
