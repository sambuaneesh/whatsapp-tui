package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Srindot/whatsapp-tui/internal/personal"
)

// Your lists, tasks and notes over the API (docs/API.md, "Lists"). They're
// local: nothing here sends anything to WhatsApp.

// ListJSON is a list.
type ListJSON struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Kind     string `json:"kind"` // tasks, notes or saved
	Pinned   bool   `json:"pinned"`
	Archived bool   `json:"archived"`
	Open     int    `json:"open"` // tasks not done (tasks lists)
}

// ItemJSON is a task, note page or saved message.
type ItemJSON struct {
	ID        int64    `json:"id"`
	List      string   `json:"list"`
	Text      string   `json:"text"`
	Notes     string   `json:"notes,omitempty"` // a page's text, a task's notes
	Done      bool     `json:"done"`
	Due       string   `json:"due,omitempty"` // RFC 3339; a day without time is midnight
	DueTime   bool     `json:"due_time,omitempty"`
	Important bool     `json:"important,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Parent    int64    `json:"parent,omitempty"` // a checklist item's checklist
	FromChat  string   `json:"from_chat,omitempty"`
	FromMsg   string   `json:"from_message,omitempty"`
}

func itemJSON(it personal.Item, list string) ItemJSON {
	j := ItemJSON{ID: it.ID, List: list, Text: it.Text, Notes: it.Body, Done: it.Done, DueTime: it.DueTime,
		Important: it.Important, Tags: it.Tags, Parent: it.ParentID, FromChat: it.SrcChat, FromMsg: it.SrcMsg}
	if it.Due > 0 {
		j.Due = time.Unix(it.Due, 0).Format(time.RFC3339)
	}
	return j
}

// personalCall answers the list methods; ok is false for other methods.
func (s *Server) personalCall(method string, raw json.RawMessage) (any, bool, error) {
	switch method {
	case "lists", "tasks", "task_add", "task_done", "task_update", "task_delete", "list_add", "note_add", "notes", "today":
	default:
		return nil, false, nil
	}
	ps := s.opts.Personal
	if ps == nil {
		return nil, true, errors.New("lists aren't available")
	}
	var p struct {
		List     string `json:"list"`
		Text     string `json:"text"`
		Title    string `json:"title"`
		ID       int64  `json:"id"`
		Done     *bool  `json:"done"`
		Due      string `json:"due"`
		Kind     string `json:"kind"`
		Icon     string `json:"icon"`
		WithDone bool   `json:"with_done"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, true, fmt.Errorf("params: %w", err)
		}
	}
	names := map[int64]string{}
	lists, err := ps.Lists()
	if err != nil {
		return nil, true, err
	}
	for _, l := range lists {
		names[l.ID] = l.Name
	}
	listNamed := func(name string) (personal.List, error) {
		l, ok := ps.ListByName(name)
		if !ok {
			return l, fmt.Errorf("no list called %q", name)
		}
		return l, nil
	}
	out := func(items []personal.Item) []ItemJSON {
		res := make([]ItemJSON, 0, len(items))
		for _, it := range items {
			res = append(res, itemJSON(it, names[it.ListID]))
		}
		return res
	}
	switch method {
	case "lists":
		res := make([]ListJSON, 0, len(lists))
		for _, l := range lists {
			j := ListJSON{ID: l.ID, Name: l.Name, Icon: l.Icon, Kind: string(l.Kind), Pinned: l.Pinned, Archived: l.Archived}
			if items, err := ps.Items(l.ID); err == nil && l.Kind == personal.KindTasks {
				for _, it := range items {
					if !it.Done && it.ParentID == 0 {
						j.Open++
					}
				}
			}
			res = append(res, j)
		}
		return res, true, nil
	case "today":
		y, m, d := s.opts.Now().Date()
		end := time.Date(y, m, d, 23, 59, 59, 0, time.Local)
		items, err := ps.Dated(end)
		return out(items), true, err
	case "tasks", "notes":
		if p.List == "" {
			if method == "notes" {
				p.List = personal.NotesName
			} else {
				return nil, true, errors.New(`tasks needs "list" (or use "today")`)
			}
		}
		l, err := listNamed(p.List)
		if err != nil {
			return nil, true, err
		}
		items, err := ps.Items(l.ID)
		if err != nil {
			return nil, true, err
		}
		items = personal.Tree(items)
		if !p.WithDone {
			kept := items[:0]
			for _, it := range items {
				if !it.Done {
					kept = append(kept, it)
				}
			}
			items = kept
		}
		return out(items), true, nil
	case "task_add":
		if strings.TrimSpace(p.Text) == "" {
			return nil, true, errors.New(`task_add needs "text"`)
		}
		var listID int64
		if p.List != "" {
			l, err := listNamed(p.List)
			if err != nil {
				return nil, true, err
			}
			listID = l.ID
		}
		it, _, err := ps.AddTyped(p.Text, listID, false, personal.Item{})
		if err != nil {
			return nil, true, err
		}
		return itemJSON(it, names[it.ListID]), true, nil
	case "task_done", "task_update", "task_delete":
		if p.ID == 0 {
			return nil, true, fmt.Errorf(`%s needs "id"`, method)
		}
		it, err := ps.Item(p.ID)
		if err != nil {
			return nil, true, fmt.Errorf("no task %d", p.ID)
		}
		switch method {
		case "task_delete":
			return "ok", true, ps.Delete(it.ID)
		case "task_done":
			done := true
			if p.Done != nil {
				done = *p.Done
			}
			return "ok", true, ps.SetDone(it.ID, done)
		}
		if p.Text != "" {
			parsed := personal.ParseTask(p.Text, s.opts.Now(), nil)
			it.Text, it.Tags, it.Important = parsed.Text, parsed.Tags, parsed.Important
			if !parsed.Due.IsZero() {
				it.Due, it.DueTime = parsed.Due.Unix(), parsed.DueTime
			}
		}
		switch strings.ToLower(strings.TrimSpace(p.Due)) {
		case "":
		case "none":
			it.Due, it.DueTime = 0, false
		default:
			t, hasTime, ok := personal.FindDate(p.Due, s.opts.Now())
			if !ok {
				return nil, true, fmt.Errorf("not a date: %q", p.Due)
			}
			it.Due, it.DueTime = t.Unix(), hasTime
		}
		if p.Done != nil {
			it.Done = *p.Done
		}
		if err := ps.Update(it); err != nil {
			return nil, true, err
		}
		it, _ = ps.Item(it.ID)
		return itemJSON(it, names[it.ListID]), true, nil
	case "list_add":
		if p.Text == "" && p.Title == "" {
			return nil, true, errors.New(`list_add needs "text" (the name)`)
		}
		name := p.Text
		if name == "" {
			name = p.Title
		}
		l, err := ps.CreateList(name, personal.Kind(p.Kind), p.Icon)
		if err != nil {
			return nil, true, err
		}
		return ListJSON{ID: l.ID, Name: l.Name, Icon: l.Icon, Kind: string(l.Kind)}, true, nil
	case "note_add":
		if p.Title == "" {
			return nil, true, errors.New(`note_add needs "title"`)
		}
		var nb personal.List
		if p.List != "" {
			if nb, err = listNamed(p.List); err != nil {
				return nil, true, err
			}
		} else if nb, err = ps.ListOfKind(personal.KindNotes, personal.NotesName); err != nil {
			return nil, true, err
		}
		it, err := ps.AddItem(personal.Item{ListID: nb.ID, Text: p.Title, Body: p.Text})
		if err != nil {
			return nil, true, err
		}
		return itemJSON(it, nb.Name), true, nil
	}
	return nil, false, nil
}

// Local answers a lists method without the app running (the command
// line uses it when there's no app to ask).
func Local(opts Options, method string, raw json.RawMessage) (any, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &Server{opts: opts}
	res, ok, err := s.personalCall(method, raw)
	if !ok {
		return nil, ErrNotRunning
	}
	return res, err
}
