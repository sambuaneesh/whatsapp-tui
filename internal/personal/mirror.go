package personal

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The Markdown mirror keeps a folder (in an Obsidian vault, say) in step
// with your lists, both ways:
//
//	<dir>/Shopping.md          a task list, one "- [ ]" line per task
//	<dir>/Notes/Wifi.md        a notebook is a folder, a page a file
//	<dir>/Saved.md             saved messages (written from here only)
//
// Task lines use the Obsidian Tasks plugin's marks, and end with a block id
// (^t12) that ties them to their task; edit, tick, reorder, add or delete
// lines in Obsidian and the app follows within a couple of seconds:
//
//	- [ ] call mom #family ⏫ 📅 2026-10-07 ⏰ 18:00 ^t12
//	- [ ] Packing ^t15
//		- [x] charger ✅ 2026-10-06 ^t16
//
// A list's file going away archives the list (it doesn't delete it).

// Mirror syncs a store with a folder of Markdown files.
type Mirror struct {
	store *Store
	Log   func(format string, args ...any)

	mu       sync.Mutex
	dir      string
	known    map[string]string // file → hash of what we last wrote or read
	exported uint64            // the store version last written out
	stop     chan struct{}
}

// NewMirror makes a mirror for dir ("" = off).
func NewMirror(s *Store, dir string) *Mirror {
	return &Mirror{store: s, dir: dir, known: map[string]string{}}
}

// Dir is the folder ("" when off).
func (mi *Mirror) Dir() string {
	mi.mu.Lock()
	defer mi.mu.Unlock()
	return mi.dir
}

// SetDir points the mirror at another folder ("" turns it off) and syncs.
func (mi *Mirror) SetDir(dir string) error {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	mi.mu.Lock()
	mi.dir, mi.known, mi.exported = dir, map[string]string{}, 0
	mi.mu.Unlock()
	if dir == "" {
		return nil
	}
	return mi.Sync()
}

// Start syncs every interval until Stop.
func (mi *Mirror) Start(interval time.Duration) {
	mi.mu.Lock()
	if mi.stop != nil {
		mi.mu.Unlock()
		return
	}
	mi.stop = make(chan struct{})
	stop := mi.stop
	mi.mu.Unlock()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			if err := mi.Sync(); err != nil {
				mi.logf("mirror: %v", err)
			}
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
	}()
}

// Stop ends Start's loop.
func (mi *Mirror) Stop() {
	mi.mu.Lock()
	defer mi.mu.Unlock()
	if mi.stop != nil {
		close(mi.stop)
		mi.stop = nil
	}
}

func (mi *Mirror) logf(format string, args ...any) {
	if mi.Log != nil {
		mi.Log(format, args...)
	}
}

// Sync reads files changed outside, then writes what changed inside.
func (mi *Mirror) Sync() error {
	mi.mu.Lock()
	defer mi.mu.Unlock()
	if mi.dir == "" {
		return nil
	}
	if err := os.MkdirAll(mi.dir, 0o755); err != nil {
		return err
	}
	first := len(mi.known) == 0
	if first {
		mi.loadState()
	}
	imported, err := mi.importChanged()
	if err != nil {
		return err
	}
	if imported || first || mi.store.Version() != mi.exported {
		if err := mi.export(); err != nil {
			return err
		}
		mi.exported = mi.store.Version()
		mi.saveState()
	}
	return nil
}

// ---------- state: what we wrote, so outside changes show ----------

const stateFile = ".whatsapp-tui-mirror"

func (mi *Mirror) loadState() {
	data, err := os.ReadFile(filepath.Join(mi.dir, stateFile))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if hash, path, ok := strings.Cut(line, " "); ok {
			mi.known[path] = hash
		}
	}
}

func (mi *Mirror) saveState() {
	var lines []string
	for path, hash := range mi.known {
		lines = append(lines, hash+" "+path)
	}
	sort.Strings(lines)
	os.WriteFile(filepath.Join(mi.dir, stateFile), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// ---------- writing ----------

// fileName makes a list or page name safe as a file name.
func fileName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|#^[]`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		name = "untitled"
	}
	return name
}

func (mi *Mirror) export() error {
	lists, err := mi.store.Lists()
	if err != nil {
		return err
	}
	want := map[string][]byte{}
	for _, l := range lists {
		items, err := mi.store.Items(l.ID)
		if err != nil {
			return err
		}
		switch l.Kind {
		case KindNotes:
			for _, it := range items {
				if it.ParentID == 0 {
					want[filepath.Join(fileName(l.Name), fileName(it.Text)+".md")] = []byte(pageFile(l, it))
				}
			}
			if len(items) == 0 {
				want[filepath.Join(fileName(l.Name), ".keep")] = []byte("")
			}
		case KindSaved:
			want[fileName(l.Name)+".md"] = []byte(savedFile(l, items))
		default:
			want[fileName(l.Name)+".md"] = []byte(listFile(l, items))
		}
	}
	for rel, data := range want {
		if mi.known[rel] == hashOf(data) {
			if _, err := os.Stat(filepath.Join(mi.dir, rel)); err == nil {
				continue
			}
		}
		if err := writeAtomic(filepath.Join(mi.dir, rel), data); err != nil {
			return err
		}
		mi.known[rel] = hashOf(data)
	}
	// files of things that are gone (renamed, deleted) go too: only ours
	for rel := range mi.known {
		if _, ok := want[rel]; !ok {
			os.Remove(filepath.Join(mi.dir, rel))
			delete(mi.known, rel)
		}
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func frontMatter(l List, extra ...string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "list-id: %d\n", l.ID)
	if l.Icon != "" {
		fmt.Fprintf(&b, "icon: %s\n", l.Icon)
	}
	if l.Pinned {
		b.WriteString("pinned: true\n")
	}
	if l.Archived {
		b.WriteString("archived: true\n")
	}
	for _, e := range extra {
		b.WriteString(e + "\n")
	}
	b.WriteString("---\n")
	return b.String()
}

// TaskLine writes a task as a Markdown (Obsidian Tasks) line.
func TaskLine(it Item) string {
	box := " "
	if it.Done {
		box = "x"
	}
	s := "- [" + box + "] " + strings.ReplaceAll(it.Text, "\n", " ")
	for _, t := range it.Tags {
		s += " #" + t
	}
	if it.Important {
		s += " ⏫"
	}
	if it.Due > 0 {
		d := time.Unix(it.Due, 0)
		s += " 📅 " + d.Format("2006-01-02")
		if it.DueTime {
			s += " ⏰ " + d.Format("15:04")
		}
	}
	if it.Done && it.DoneAt > 0 {
		s += " ✅ " + time.Unix(it.DoneAt, 0).Format("2006-01-02")
	}
	if it.ID > 0 {
		s += " ^t" + strconv.FormatInt(it.ID, 10)
	}
	return s
}

func listFile(l List, items []Item) string {
	var b strings.Builder
	b.WriteString(frontMatter(l))
	for _, it := range Tree(items) {
		if it.ParentID != 0 {
			b.WriteString("\t")
		}
		b.WriteString(TaskLine(it) + "\n")
	}
	return b.String()
}

func pageFile(l List, it Item) string {
	return fmt.Sprintf("---\npage-id: %d\n---\n%s", it.ID, strings.TrimRight(it.Body, "\n")+"\n")
}

func savedFile(l List, items []Item) string {
	var b strings.Builder
	b.WriteString(frontMatter(l, "note: written by whatsapp-tui; changes here are overwritten"))
	sort.SliceStable(items, func(a, c int) bool { return items[a].Created > items[c].Created })
	for _, it := range items {
		when := time.Unix(it.Created, 0).Format("2 Jan 2006")
		who := it.SrcSender
		if who != "" {
			who += ": "
		}
		where := ""
		if it.Body != "" {
			where = " — " + it.Body
		}
		fmt.Fprintf(&b, "- %s%s *(%s%s)*\n", who, strings.ReplaceAll(it.Text, "\n", " "), when, where)
	}
	return b.String()
}

// ---------- reading ----------

// importChanged reads the files that changed since we last saw them.
func (mi *Mirror) importChanged() (bool, error) {
	changed := false
	seen := map[string]bool{}
	err := filepath.WalkDir(mi.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(mi.dir, path)
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".md") || strings.Count(rel, string(filepath.Separator)) > 1 {
			return nil
		}
		seen[rel] = true
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if mi.known[rel] == hashOf(data) {
			return nil
		}
		if err := mi.importFile(rel, data); err != nil {
			mi.logf("mirror: %s: %v", rel, err)
			return nil
		}
		mi.known[rel] = hashOf(data)
		changed = true
		return nil
	})
	if err != nil {
		return changed, err
	}
	// files we knew that are gone: their lists are archived, pages deleted
	for rel := range mi.known {
		if seen[rel] || !strings.HasSuffix(rel, ".md") {
			continue
		}
		if mi.removed(rel) {
			changed = true
		}
		delete(mi.known, rel)
	}
	return changed, nil
}

func (mi *Mirror) removed(rel string) bool {
	dir, file := filepath.Split(rel)
	name := strings.TrimSuffix(file, ".md")
	if dir == "" {
		l, ok := mi.store.ListByName(name)
		if ok && !l.Archived && l.Kind == KindTasks {
			if _, err := os.Stat(filepath.Join(mi.dir, rel)); errors.Is(err, os.ErrNotExist) {
				l.Archived = true
				mi.store.UpdateList(l)
				mi.logf("mirror: %s.md went away: archived the list", name)
				return true
			}
		}
		return false
	}
	nb, ok := mi.store.ListByName(strings.TrimSuffix(dir, string(filepath.Separator)))
	if !ok || nb.Kind != KindNotes {
		return false
	}
	if _, err := os.Stat(filepath.Join(mi.dir, filepath.Dir(rel))); errors.Is(err, os.ErrNotExist) {
		return false // the whole notebook folder went: leave the pages be
	}
	items, _ := mi.store.Items(nb.ID)
	for _, it := range items {
		if it.ParentID == 0 && fileName(it.Text) == name {
			mi.store.Delete(it.ID)
			return true
		}
	}
	return false
}

var (
	frontRe   = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)
	taskRe    = regexp.MustCompile(`^(\s*)[-*] \[( |x|X)\] (.*)$`)
	blockRe   = regexp.MustCompile(`\s*\^t(\d+)\s*$`)
	dueRe     = regexp.MustCompile(`\s*📅\s*(\d{4}-\d{2}-\d{2})`)
	timeRe    = regexp.MustCompile(`\s*⏰\s*(\d{1,2}:\d{2})`)
	doneRe    = regexp.MustCompile(`\s*✅\s*(\d{4}-\d{2}-\d{2})`)
	prioRe    = regexp.MustCompile(`\s*(⏫|🔺)`)
	otherMark = regexp.MustCompile(`\s*(🔼|🔽|⏬|⏳\s*\d{4}-\d{2}-\d{2}|🛫\s*\d{4}-\d{2}-\d{2}|➕\s*\d{4}-\d{2}-\d{2}|🔁[^#📅⏰✅^]*)`)
)

func frontValues(data string) (map[string]string, string) {
	vals := map[string]string{}
	m := frontRe.FindStringSubmatch(data)
	if m == nil {
		return vals, data
	}
	for _, line := range strings.Split(m[1], "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return vals, data[len(m[0]):]
}

func (mi *Mirror) importFile(rel string, data []byte) error {
	dir, file := filepath.Split(rel)
	name := strings.TrimSuffix(file, ".md")
	vals, body := frontValues(string(data))
	if dir != "" {
		return mi.importPage(strings.TrimSuffix(dir, string(filepath.Separator)), name, vals, body)
	}
	// which list: its id, else its name; a new file is a new list
	var l List
	var err error
	if id, _ := strconv.ParseInt(vals["list-id"], 10, 64); id > 0 {
		l, err = mi.store.List(id)
	}
	if l.ID == 0 || err != nil {
		var ok bool
		if l, ok = mi.store.ListByName(name); !ok {
			if l, err = mi.store.CreateList(name, KindTasks, vals["icon"]); err != nil {
				return err
			}
		}
	}
	if l.Kind == KindSaved {
		return nil // written from here only
	}
	// list settings: the file's name, icon, pin, archive
	nl := l
	nl.Name = name
	if v, ok := vals["icon"]; ok {
		nl.Icon = v
	}
	nl.Pinned = vals["pinned"] == "true"
	nl.Archived = vals["archived"] == "true"
	if nl != l {
		if err := mi.store.UpdateList(nl); err != nil {
			mi.logf("mirror: %s: %v", rel, err)
		}
	}
	if l.Kind == KindNotes {
		return nil
	}
	return mi.importTasks(l, body)
}

// ParseTaskLine reads a Markdown task line; ok is false for other lines.
func ParseTaskLine(line string) (it Item, depth int, ok bool) {
	m := taskRe.FindStringSubmatch(line)
	if m == nil {
		return it, 0, false
	}
	depth = len(strings.ReplaceAll(m[1], "    ", "\t"))
	it.Done = m[2] != " "
	rest := m[3]
	if b := blockRe.FindStringSubmatch(rest); b != nil {
		it.ID, _ = strconv.ParseInt(b[1], 10, 64)
		rest = rest[:len(rest)-len(b[0])]
	}
	if d := doneRe.FindStringSubmatch(rest); d != nil {
		if t, err := time.ParseInLocation("2006-01-02", d[1], time.Local); err == nil {
			it.DoneAt = t.Unix()
		}
		rest = doneRe.ReplaceAllString(rest, "")
	}
	if d := dueRe.FindStringSubmatch(rest); d != nil {
		day := d[1]
		layout := "2006-01-02"
		if tm := timeRe.FindStringSubmatch(rest); tm != nil {
			day += " " + tm[1]
			layout += " 15:04"
			it.DueTime = true
		}
		if t, err := time.ParseInLocation(layout, day, time.Local); err == nil {
			it.Due = t.Unix()
		}
		rest = dueRe.ReplaceAllString(rest, "")
	}
	rest = timeRe.ReplaceAllString(rest, "")
	if prioRe.MatchString(rest) {
		it.Important = true
		rest = prioRe.ReplaceAllString(rest, "")
	}
	rest = otherMark.ReplaceAllString(rest, "")
	for _, t := range tagRe.FindAllStringSubmatch(rest, -1) {
		it.Tags = append(it.Tags, strings.ToLower(t[1]))
	}
	rest = tagRe.ReplaceAllString(rest, " ")
	it.Text = strings.Join(strings.Fields(rest), " ")
	return it, depth, it.Text != ""
}

// importTasks makes a list match its file: changed lines update their
// task, new lines add one, missing lines delete theirs.
func (mi *Mirror) importTasks(l List, body string) error {
	have, err := mi.store.Items(l.ID)
	if err != nil {
		return err
	}
	byID := map[int64]Item{}
	for _, it := range have {
		byID[it.ID] = it
	}
	kept := map[int64]bool{}
	var parent int64
	pos := map[int64]int{} // position per parent
	for _, line := range strings.Split(body, "\n") {
		got, depth, ok := ParseTaskLine(line)
		if !ok {
			continue
		}
		p := int64(0)
		if depth > 0 {
			p = parent
		}
		pos[p]++
		old, exists := byID[got.ID]
		if got.ID == 0 || !exists || kept[got.ID] {
			it := got
			it.ID, it.ListID, it.ParentID = 0, l.ID, p
			added, err := mi.store.AddItem(it)
			if err != nil {
				return err
			}
			mi.store.setPosition(added.ID, pos[p])
			kept[added.ID] = true
			if depth == 0 {
				parent = added.ID
			}
			continue
		}
		kept[got.ID] = true
		if depth == 0 {
			parent = got.ID
		}
		upd := old
		upd.Text, upd.Done, upd.Tags, upd.Important, upd.ParentID = got.Text, got.Done, got.Tags, got.Important, p
		upd.Due, upd.DueTime = got.Due, got.DueTime
		if !sameItem(upd, old) {
			if err := mi.store.Update(upd); err != nil {
				return err
			}
		}
		if old.Position != pos[p] {
			mi.store.setPosition(old.ID, pos[p])
		}
	}
	for _, it := range have {
		if !kept[it.ID] {
			if _, err := mi.store.Item(it.ID); err == nil {
				mi.store.Delete(it.ID)
			}
		}
	}
	return nil
}

func sameItem(a, b Item) bool {
	return a.Text == b.Text && a.Done == b.Done && a.Important == b.Important && a.ParentID == b.ParentID &&
		a.Due == b.Due && a.DueTime == b.DueTime && strings.Join(a.Tags, " ") == strings.Join(b.Tags, " ")
}

// importPage reads a note page: the file name is its title, the text its
// body.
func (mi *Mirror) importPage(notebook, title string, vals map[string]string, body string) error {
	nb, ok := mi.store.ListByName(notebook)
	if !ok {
		var err error
		if nb, err = mi.store.CreateList(notebook, KindNotes, ""); err != nil {
			return err
		}
	}
	if nb.Kind != KindNotes {
		return nil
	}
	body = strings.TrimRight(body, "\n")
	if id, _ := strconv.ParseInt(vals["page-id"], 10, 64); id > 0 {
		if it, err := mi.store.Item(id); err == nil && it.ListID == nb.ID {
			if it.Text != title || it.Body != body {
				it.Text, it.Body = title, body
				return mi.store.Update(it)
			}
			return nil
		}
	}
	items, _ := mi.store.Items(nb.ID)
	for _, it := range items {
		if it.ParentID == 0 && fileName(it.Text) == title {
			if it.Body != body {
				it.Body = body
				return mi.store.Update(it)
			}
			return nil
		}
	}
	_, err := mi.store.AddItem(Item{ListID: nb.ID, Text: title, Body: body})
	return err
}

// setPosition sets an item's place (the mirror's reordering; not undone
// on its own).
func (s *Store) setPosition(id int64, pos int) {
	s.mu.Lock()
	s.db.Exec(`UPDATE items SET position = ? WHERE id = ?`, pos, id)
	s.changed()
	s.mu.Unlock()
}
