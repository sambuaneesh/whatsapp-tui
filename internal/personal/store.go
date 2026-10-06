// Package personal is your own space next to your chats: task lists (with
// checklists, due dates and reminders), notes and saved messages. It's
// independent of WhatsApp: everything stays in its own local database
// (personal.db), and can be mirrored to Markdown files (an Obsidian vault).
package personal

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Kind is what a list holds.
type Kind string

const (
	KindTasks Kind = "tasks"
	KindNotes Kind = "notes" // pages: a title and a Markdown body
	KindSaved Kind = "saved" // messages saved from chats
)

// List is a task list, a notebook or the saved messages.
type List struct {
	ID        int64
	Name      string
	Icon      string // an emoji shown before the name
	Kind      Kind
	Pinned    bool
	Archived  bool
	Updated   int64  // unix seconds of the last change in it
	ShareChat string // a chat it was shared with: "done: <item>" there ticks items off
}

// Item is a task (or checklist), a note page or a saved message.
type Item struct {
	ID        int64
	ListID    int64
	ParentID  int64  // a checklist's items have their checklist as parent
	Text      string // the task, or the page's title
	Body      string // a page's text, or a task's notes
	Done      bool
	DoneAt    int64
	Due       int64 // unix seconds; 0 = no date
	DueTime   bool  // Due has a time of day (else it's just the day)
	Important bool
	Tags      []string
	Position  int
	Created   int64
	Updated   int64
	Reminded  bool // the reminder for Due went out

	// where it came from: a message (made a task with T, or saved with b)
	SrcChat, SrcMsg, SrcSender string
}

// Store keeps the lists and items, and undoes changes.
type Store struct {
	db  *sql.DB
	mu  sync.Mutex
	now func() time.Time

	undo []undoStep
	ver  uint64 // bumped on every change (for the mirror and the UI)
	// OnChange, when set, is called after every change (outside the lock).
	OnChange func()
}

// undoStep is how to take a change back: items to put back as they were,
// items (that the change created) to remove, and the same for lists.
type undoStep struct {
	label       string
	restore     []Item
	remove      []int64
	restoreList []List
	removeList  []int64
}

const maxUndo = 100

// Open opens (or creates) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	return OpenDB(db)
}

// OpenDB uses an open database (tests use ":memory:" with one connection).
func OpenDB(db *sql.DB) (*Store, error) {
	s := &Store{db: db, now: time.Now}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SetClock replaces the clock (tests).
func (s *Store) SetClock(now func() time.Time) { s.now = now }

func (s *Store) init() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS lists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		icon TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL DEFAULT 'tasks',
		pinned INTEGER NOT NULL DEFAULT 0,
		archived INTEGER NOT NULL DEFAULT 0,
		updated INTEGER NOT NULL DEFAULT 0,
		share_chat TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		list_id INTEGER NOT NULL,
		parent_id INTEGER NOT NULL DEFAULT 0,
		text TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		done INTEGER NOT NULL DEFAULT 0,
		done_at INTEGER NOT NULL DEFAULT 0,
		due INTEGER NOT NULL DEFAULT 0,
		due_time INTEGER NOT NULL DEFAULT 0,
		important INTEGER NOT NULL DEFAULT 0,
		tags TEXT NOT NULL DEFAULT '',
		position INTEGER NOT NULL DEFAULT 0,
		created INTEGER NOT NULL DEFAULT 0,
		updated INTEGER NOT NULL DEFAULT 0,
		reminded INTEGER NOT NULL DEFAULT 0,
		src_chat TEXT NOT NULL DEFAULT '',
		src_msg TEXT NOT NULL DEFAULT '',
		src_sender TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_items_list ON items(list_id, parent_id, position);
	CREATE INDEX IF NOT EXISTS idx_items_due ON items(done, due);`)
	if err != nil {
		return fmt.Errorf("personal: create tables: %w", err)
	}
	return s.ensureDefaults()
}

// The lists every space starts with. Tasks typed in Today go to the Inbox.
const (
	InboxName = "Inbox"
	NotesName = "Notes"
	SavedName = "Saved"
)

func (s *Store) ensureDefaults() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM lists`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	now := s.now().Unix()
	for _, l := range []List{
		{Name: InboxName, Icon: "📥", Kind: KindTasks},
		{Name: NotesName, Icon: "📝", Kind: KindNotes},
		{Name: SavedName, Icon: "🔖", Kind: KindSaved},
	} {
		if _, err := s.db.Exec(`INSERT INTO lists (name, icon, kind, updated) VALUES (?, ?, ?, ?)`,
			l.Name, l.Icon, l.Kind, now); err != nil {
			return err
		}
	}
	return nil
}

// Version changes whenever anything changes.
func (s *Store) Version() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ver
}

func (s *Store) changed() {
	s.ver++
}

func (s *Store) notify() {
	if s.OnChange != nil {
		s.OnChange()
	}
}

// ---------- lists ----------

const listCols = `id, name, icon, kind, pinned, archived, updated, share_chat`

func scanList(r interface{ Scan(...any) error }) (List, error) {
	var l List
	var kind string
	err := r.Scan(&l.ID, &l.Name, &l.Icon, &kind, &l.Pinned, &l.Archived, &l.Updated, &l.ShareChat)
	l.Kind = Kind(kind)
	return l, err
}

// Lists returns every list (archived ones too), pinned first, then the
// most recently changed.
func (s *Store) Lists() ([]List, error) {
	rows, err := s.db.Query(`SELECT ` + listCols + ` FROM lists ORDER BY pinned DESC, updated DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []List
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// List returns one list.
func (s *Store) List(id int64) (List, error) {
	return scanList(s.db.QueryRow(`SELECT `+listCols+` FROM lists WHERE id = ?`, id))
}

// ListByName finds a list by name, ignoring case (and its icon).
func (s *Store) ListByName(name string) (List, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	lists, err := s.Lists()
	if err != nil {
		return List{}, false
	}
	for _, l := range lists {
		if strings.ToLower(l.Name) == name {
			return l, true
		}
	}
	return List{}, false
}

// ListOfKind returns the first list of a kind (the Inbox, Notes, Saved),
// making it if there's none.
func (s *Store) ListOfKind(kind Kind, name string) (List, error) {
	if l, ok := s.ListByName(name); ok && l.Kind == kind {
		return l, nil
	}
	lists, err := s.Lists()
	if err != nil {
		return List{}, err
	}
	for _, l := range lists {
		if l.Kind == kind && !l.Archived {
			return l, nil
		}
	}
	icon := map[Kind]string{KindTasks: "📥", KindNotes: "📝", KindSaved: "🔖"}[kind]
	return s.CreateList(name, kind, icon)
}

// CreateList makes a list.
func (s *Store) CreateList(name string, kind Kind, icon string) (List, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return List{}, errors.New("a list needs a name")
	}
	if _, ok := s.ListByName(name); ok {
		return List{}, fmt.Errorf("there's already a list called %q", name)
	}
	if kind == "" {
		kind = KindTasks
	}
	if icon == "" {
		icon = map[Kind]string{KindTasks: "📋", KindNotes: "📝", KindSaved: "🔖"}[kind]
	}
	s.mu.Lock()
	res, err := s.db.Exec(`INSERT INTO lists (name, icon, kind, updated) VALUES (?, ?, ?, ?)`, name, icon, kind, s.now().Unix())
	if err != nil {
		s.mu.Unlock()
		return List{}, err
	}
	id, _ := res.LastInsertId()
	s.pushUndo(undoStep{label: "created " + name, removeList: []int64{id}})
	s.changed()
	s.mu.Unlock()
	s.notify()
	return s.List(id)
}

// UpdateList saves a list's name, icon, pin, archive and share settings.
func (s *Store) UpdateList(l List) error {
	l.Name = strings.TrimSpace(l.Name)
	if l.Name == "" {
		return errors.New("a list needs a name")
	}
	if other, ok := s.ListByName(l.Name); ok && other.ID != l.ID {
		return fmt.Errorf("there's already a list called %q", l.Name)
	}
	old, err := s.List(l.ID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	_, err = s.db.Exec(`UPDATE lists SET name = ?, icon = ?, pinned = ?, archived = ?, share_chat = ?, updated = ? WHERE id = ?`,
		l.Name, l.Icon, l.Pinned, l.Archived, l.ShareChat, max(l.Updated, old.Updated), l.ID)
	if err == nil {
		s.pushUndo(undoStep{label: "changed " + old.Name, restoreList: []List{old}})
		s.changed()
	}
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// DeleteList deletes a list and everything in it (undo brings it back).
func (s *Store) DeleteList(id int64) error {
	l, err := s.List(id)
	if err != nil {
		return err
	}
	items, err := s.Items(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	tx, err := s.db.Begin()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if _, err = tx.Exec(`DELETE FROM items WHERE list_id = ?`, id); err == nil {
		_, err = tx.Exec(`DELETE FROM lists WHERE id = ?`, id)
	}
	if err != nil {
		tx.Rollback()
		s.mu.Unlock()
		return err
	}
	if err = tx.Commit(); err == nil {
		s.pushUndo(undoStep{label: "deleted " + l.Name, restoreList: []List{l}, restore: items})
		s.changed()
	}
	s.mu.Unlock()
	s.notify()
	return err
}

// touchList marks a list changed now (it moves up in the chat list).
func (s *Store) touchList(id int64) {
	s.db.Exec(`UPDATE lists SET updated = ? WHERE id = ?`, s.now().Unix(), id)
}

// ---------- items ----------

const itemCols = `id, list_id, parent_id, text, body, done, done_at, due, due_time, important, tags, position,
	created, updated, reminded, src_chat, src_msg, src_sender`

func scanItem(r interface{ Scan(...any) error }) (Item, error) {
	var it Item
	var tags string
	err := r.Scan(&it.ID, &it.ListID, &it.ParentID, &it.Text, &it.Body, &it.Done, &it.DoneAt, &it.Due, &it.DueTime,
		&it.Important, &tags, &it.Position, &it.Created, &it.Updated, &it.Reminded, &it.SrcChat, &it.SrcMsg, &it.SrcSender)
	it.Tags = strings.Fields(tags)
	return it, err
}

func (s *Store) queryItems(q string, args ...any) ([]Item, error) {
	rows, err := s.db.Query(`SELECT `+itemCols+` FROM items `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Items returns a list's items (checklist items too), in their order.
func (s *Store) Items(listID int64) ([]Item, error) {
	return s.queryItems(`WHERE list_id = ? ORDER BY parent_id, position, id`, listID)
}

// Item returns one item.
func (s *Store) Item(id int64) (Item, error) {
	items, err := s.queryItems(`WHERE id = ?`, id)
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, sql.ErrNoRows
	}
	return items[0], nil
}

// Children returns a checklist's items.
func (s *Store) Children(id int64) ([]Item, error) {
	return s.queryItems(`WHERE parent_id = ? ORDER BY position, id`, id)
}

// Dated returns the open tasks (not done) due on or before until, from
// every list that isn't archived: what Today shows.
func (s *Store) Dated(until time.Time) ([]Item, error) {
	return s.queryItems(`WHERE done = 0 AND due > 0 AND due <= ?
		AND list_id IN (SELECT id FROM lists WHERE archived = 0 AND kind = 'tasks') ORDER BY due, position, id`, until.Unix())
}

// DoneSince returns tasks done since t (Today's "done" section).
func (s *Store) DoneSince(t time.Time) ([]Item, error) {
	return s.queryItems(`WHERE done = 1 AND done_at >= ? AND due > 0
		AND list_id IN (SELECT id FROM lists WHERE kind = 'tasks') ORDER BY done_at DESC`, t.Unix())
}

// Search finds items whose text, notes or tags contain all the words.
func (s *Store) Search(query string) ([]Item, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil, nil
	}
	var conds []string
	var args []any
	for _, w := range words {
		conds = append(conds, `(lower(text) LIKE ? OR lower(body) LIKE ? OR lower(tags) LIKE ?)`)
		p := "%" + w + "%"
		args = append(args, p, p, p)
	}
	return s.queryItems(`WHERE `+strings.Join(conds, " AND ")+` ORDER BY updated DESC LIMIT 200`, args...)
}

// AddItem adds an item at the end of its list (or checklist).
func (s *Store) AddItem(it Item) (Item, error) {
	if strings.TrimSpace(it.Text) == "" && strings.TrimSpace(it.Body) == "" {
		return Item{}, errors.New("nothing to add")
	}
	s.mu.Lock()
	it, err := s.insert(it)
	if err == nil {
		s.pushUndo(undoStep{label: "added " + short(it.Text), remove: []int64{it.ID}})
		s.changed()
	}
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return it, err
}

// AddChecklist adds a checklist: the title, and its items under it.
func (s *Store) AddChecklist(title Item, items []Item) (Item, error) {
	s.mu.Lock()
	head, err := s.insert(title)
	ids := []int64{head.ID}
	if err == nil {
		for _, c := range items {
			c.ListID, c.ParentID = head.ListID, head.ID
			var child Item
			if child, err = s.insert(c); err != nil {
				break
			}
			ids = append(ids, child.ID)
		}
	}
	if err == nil {
		s.pushUndo(undoStep{label: "added " + short(head.Text), remove: ids})
		s.changed()
	}
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return head, err
}

func (s *Store) insert(it Item) (Item, error) {
	now := s.now().Unix()
	if it.Created == 0 {
		it.Created = now
	}
	it.Updated = now
	if it.Done && it.DoneAt == 0 {
		it.DoneAt = now
	}
	var pos sql.NullInt64
	s.db.QueryRow(`SELECT MAX(position) FROM items WHERE list_id = ? AND parent_id = ?`, it.ListID, it.ParentID).Scan(&pos)
	it.Position = int(pos.Int64) + 1
	res, err := s.db.Exec(`INSERT INTO items (list_id, parent_id, text, body, done, done_at, due, due_time, important, tags,
		position, created, updated, reminded, src_chat, src_msg, src_sender) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ListID, it.ParentID, it.Text, it.Body, it.Done, it.DoneAt, it.Due, it.DueTime, it.Important, strings.Join(it.Tags, " "),
		it.Position, it.Created, it.Updated, it.Reminded, it.SrcChat, it.SrcMsg, it.SrcSender)
	if err != nil {
		return it, err
	}
	it.ID, _ = res.LastInsertId()
	s.touchList(it.ListID)
	return it, nil
}

// put writes an item as it is (all fields; its id must exist or is made).
func (s *Store) put(it Item) error {
	_, err := s.db.Exec(`INSERT INTO items (id, list_id, parent_id, text, body, done, done_at, due, due_time, important, tags,
		position, created, updated, reminded, src_chat, src_msg, src_sender) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET list_id = excluded.list_id, parent_id = excluded.parent_id, text = excluded.text,
		body = excluded.body, done = excluded.done, done_at = excluded.done_at, due = excluded.due, due_time = excluded.due_time,
		important = excluded.important, tags = excluded.tags, position = excluded.position, created = excluded.created,
		updated = excluded.updated, reminded = excluded.reminded, src_chat = excluded.src_chat, src_msg = excluded.src_msg,
		src_sender = excluded.src_sender`,
		it.ID, it.ListID, it.ParentID, it.Text, it.Body, it.Done, it.DoneAt, it.Due, it.DueTime, it.Important,
		strings.Join(it.Tags, " "), it.Position, it.Created, it.Updated, it.Reminded, it.SrcChat, it.SrcMsg, it.SrcSender)
	return err
}

// Update saves an item's changes (undo restores the old one). A new due
// time means a new reminder.
func (s *Store) Update(it Item) error {
	old, err := s.Item(it.ID)
	if err != nil {
		return err
	}
	return s.change("changed "+short(old.Text), []Item{old}, func() error {
		if it.Due != old.Due || it.DueTime != old.DueTime {
			it.Reminded = it.Due > 0 && it.Due <= s.now().Unix() && old.Reminded
		}
		if it.Done != old.Done {
			it.DoneAt = 0
			if it.Done {
				it.DoneAt = s.now().Unix()
			}
		}
		it.Updated = s.now().Unix()
		if err := s.put(it); err != nil {
			return err
		}
		s.touchList(it.ListID)
		if it.ListID != old.ListID {
			// a checklist moves with its items
			_, err := s.db.Exec(`UPDATE items SET list_id = ? WHERE parent_id = ?`, it.ListID, it.ID)
			s.touchList(old.ListID)
			return err
		}
		return nil
	})
}

// change runs f in the lock, remembering before (the items as they were)
// for undo.
func (s *Store) change(label string, before []Item, f func() error) error {
	s.mu.Lock()
	err := f()
	if err == nil {
		s.pushUndo(undoStep{label: label, restore: before})
		s.changed()
	}
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// SetDone ticks an item off (or back on).
func (s *Store) SetDone(id int64, done bool) error {
	it, err := s.Item(id)
	if err != nil {
		return err
	}
	it.Done = done
	return s.Update(it)
}

// Delete deletes an item (a checklist with its items).
func (s *Store) Delete(id int64) error {
	it, err := s.Item(id)
	if err != nil {
		return err
	}
	kids, _ := s.Children(id)
	before := append([]Item{it}, kids...)
	return s.change("deleted "+short(it.Text), before, func() error {
		if _, err := s.db.Exec(`DELETE FROM items WHERE id = ? OR parent_id = ?`, id, id); err != nil {
			return err
		}
		s.touchList(it.ListID)
		return nil
	})
}

// ClearDone deletes a list's done items.
func (s *Store) ClearDone(listID int64) (int, error) {
	done, err := s.queryItems(`WHERE list_id = ? AND done = 1`, listID)
	if err != nil || len(done) == 0 {
		return 0, err
	}
	var before []Item
	for _, d := range done {
		before = append(before, d)
		if kids, _ := s.Children(d.ID); len(kids) > 0 {
			before = append(before, kids...)
		}
	}
	err = s.change(fmt.Sprintf("cleared %d done", len(done)), before, func() error {
		for _, d := range done {
			if _, err := s.db.Exec(`DELETE FROM items WHERE id = ? OR parent_id = ?`, d.ID, d.ID); err != nil {
				return err
			}
		}
		s.touchList(listID)
		return nil
	})
	return len(done), err
}

// siblings returns the items next to it (same list and parent), in order.
func (s *Store) siblings(it Item) ([]Item, error) {
	return s.queryItems(`WHERE list_id = ? AND parent_id = ? ORDER BY position, id`, it.ListID, it.ParentID)
}

// Move moves an item delta places among its siblings (J/K).
func (s *Store) Move(id int64, delta int) error {
	it, err := s.Item(id)
	if err != nil {
		return err
	}
	sibs, err := s.siblings(it)
	if err != nil {
		return err
	}
	i := indexOf(sibs, id)
	j := min(max(i+delta, 0), len(sibs)-1)
	if i < 0 || i == j {
		return nil
	}
	moved := append([]Item(nil), sibs...)
	x := moved[i]
	moved = append(moved[:i], moved[i+1:]...)
	moved = append(moved[:j], append([]Item{x}, moved[j:]...)...)
	return s.change("moved "+short(it.Text), sibs, func() error { return s.renumber(moved) })
}

// MoveNextTo puts an item right before (or, with after, right after)
// another one among its siblings (reordering when some are hidden).
func (s *Store) MoveNextTo(id, other int64, after bool) error {
	it, err := s.Item(id)
	if err != nil {
		return err
	}
	sibs, err := s.siblings(it)
	if err != nil {
		return err
	}
	i := indexOf(sibs, id)
	if i < 0 || indexOf(sibs, other) < 0 || id == other {
		return nil
	}
	moved := append([]Item(nil), sibs[:i]...)
	moved = append(moved, sibs[i+1:]...)
	j := indexOf(moved, other)
	if after {
		j++
	}
	moved = append(moved[:j], append([]Item{sibs[i]}, moved[j:]...)...)
	return s.change("moved "+short(it.Text), sibs, func() error { return s.renumber(moved) })
}

func (s *Store) renumber(items []Item) error {
	for i, x := range items {
		if _, err := s.db.Exec(`UPDATE items SET position = ? WHERE id = ?`, i+1, x.ID); err != nil {
			return err
		}
	}
	return nil
}

func indexOf(items []Item, id int64) int {
	for i, x := range items {
		if x.ID == id {
			return i
		}
	}
	return -1
}

// Indent makes an item part of the checklist (or task) above it (>).
func (s *Store) Indent(id int64) error {
	it, err := s.Item(id)
	if err != nil {
		return err
	}
	if it.ParentID != 0 {
		return errors.New("checklists go one level deep")
	}
	sibs, err := s.siblings(it)
	if err != nil {
		return err
	}
	i := indexOf(sibs, id)
	if i <= 0 {
		return errors.New("there's nothing above it to go under")
	}
	if kids, _ := s.Children(id); len(kids) > 0 {
		return errors.New("a checklist can't go inside another one")
	}
	parent := sibs[i-1]
	return s.change("indented "+short(it.Text), []Item{it}, func() error {
		var pos sql.NullInt64
		s.db.QueryRow(`SELECT MAX(position) FROM items WHERE parent_id = ?`, parent.ID).Scan(&pos)
		_, err := s.db.Exec(`UPDATE items SET parent_id = ?, position = ? WHERE id = ?`, parent.ID, pos.Int64+1, id)
		return err
	})
}

// Outdent lifts a checklist item out, right after its checklist (<).
func (s *Store) Outdent(id int64) error {
	it, err := s.Item(id)
	if err != nil {
		return err
	}
	if it.ParentID == 0 {
		return nil
	}
	parent, err := s.Item(it.ParentID)
	if err != nil {
		return err
	}
	top, err := s.siblings(parent)
	if err != nil {
		return err
	}
	return s.change("outdented "+short(it.Text), append([]Item{it}, top...), func() error {
		if _, err := s.db.Exec(`UPDATE items SET parent_id = 0 WHERE id = ?`, id); err != nil {
			return err
		}
		it.ParentID = 0
		j := indexOf(top, parent.ID) + 1
		order := append(append(append([]Item(nil), top[:j]...), it), top[j:]...)
		return s.renumber(order)
	})
}

// DueReminders returns tasks whose time has come and that haven't
// reminded yet.
func (s *Store) DueReminders(now time.Time) ([]Item, error) {
	return s.queryItems(`WHERE done = 0 AND due_time = 1 AND reminded = 0 AND due > 0 AND due <= ?
		AND list_id IN (SELECT id FROM lists WHERE archived = 0) ORDER BY due`, now.Unix())
}

// MarkReminded records that a reminder went out (not undoable).
func (s *Store) MarkReminded(id int64) error {
	_, err := s.db.Exec(`UPDATE items SET reminded = 1 WHERE id = ?`, id)
	return err
}

// ---------- undo ----------

func (s *Store) pushUndo(u undoStep) {
	s.undo = append(s.undo, u)
	if len(s.undo) > maxUndo {
		s.undo = s.undo[len(s.undo)-maxUndo:]
	}
}

// CanUndo reports what undo would take back ("" when nothing).
func (s *Store) CanUndo() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.undo) == 0 {
		return ""
	}
	return s.undo[len(s.undo)-1].label
}

// Undo takes the last change back and says what it was.
func (s *Store) Undo() (string, error) {
	s.mu.Lock()
	if len(s.undo) == 0 {
		s.mu.Unlock()
		return "", errors.New("nothing to undo")
	}
	u := s.undo[len(s.undo)-1]
	s.undo = s.undo[:len(s.undo)-1]
	err := func() error {
		for _, id := range u.remove {
			if _, err := s.db.Exec(`DELETE FROM items WHERE id = ? OR parent_id = ?`, id, id); err != nil {
				return err
			}
		}
		for _, id := range u.removeList {
			if _, err := s.db.Exec(`DELETE FROM items WHERE list_id = ?`, id); err != nil {
				return err
			}
			if _, err := s.db.Exec(`DELETE FROM lists WHERE id = ?`, id); err != nil {
				return err
			}
		}
		for _, l := range u.restoreList {
			if _, err := s.db.Exec(`INSERT INTO lists (id, name, icon, kind, pinned, archived, updated, share_chat)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET name = excluded.name, icon = excluded.icon,
				kind = excluded.kind, pinned = excluded.pinned, archived = excluded.archived, updated = excluded.updated,
				share_chat = excluded.share_chat`, l.ID, l.Name, l.Icon, l.Kind, l.Pinned, l.Archived, l.Updated, l.ShareChat); err != nil {
				return err
			}
		}
		for _, it := range u.restore {
			if err := s.put(it); err != nil {
				return err
			}
		}
		return nil
	}()
	if err == nil {
		s.changed()
	}
	s.mu.Unlock()
	s.notify()
	return u.label, err
}

// ---------- helpers ----------

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 40 {
		return string(r[:39]) + "…"
	}
	return s
}

// Tree orders a list's items for display: each top-level item followed by
// its checklist items.
func Tree(items []Item) []Item {
	kids := map[int64][]Item{}
	var top []Item
	for _, it := range items {
		if it.ParentID == 0 {
			top = append(top, it)
		} else {
			kids[it.ParentID] = append(kids[it.ParentID], it)
		}
	}
	sort.SliceStable(top, func(a, b int) bool { return top[a].Position < top[b].Position })
	var out []Item
	for _, t := range top {
		out = append(out, t)
		k := kids[t.ID]
		sort.SliceStable(k, func(a, b int) bool { return k[a].Position < k[b].Position })
		out = append(out, k...)
	}
	return out
}
