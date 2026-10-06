package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Srindot/whatsapp-tui/internal/api"
	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/daemon"
	"github.com/Srindot/whatsapp-tui/internal/personal"
)

// Your lists from the shell:
//
//	whatsapp-tui todo                       what's due today (and overdue)
//	whatsapp-tui todo call mom 6pm #family  add a task (shopping: eggs → Shopping)
//	whatsapp-tui todo list Shopping         a list
//	whatsapp-tui todo lists                 all lists
//	whatsapp-tui todo done 12               tick task 12 off
//	whatsapp-tui note "Wifi" "password: …"  a note page
//	whatsapp-tui capture                    a small prompt to add one (bind it to a key)
//
// They go through the running app (so it redraws at once), else straight
// to the lists' database.

const todoUsage = `usage:
  whatsapp-tui todo                        what's due today (and overdue)
  whatsapp-tui todo <task, as you'd say it> add one: "call mom 6pm #family !", "shopping: eggs"
  whatsapp-tui todo list <name>            the tasks in a list
  whatsapp-tui todo lists                  your lists
  whatsapp-tui todo done <id>              tick a task off (undone <id> puts it back)
  whatsapp-tui todo rm <id>                delete a task
  whatsapp-tui note <title> [text]         add a note page
  whatsapp-tui capture                     a small prompt to add one`

// personalCall runs a lists method on the running app, or locally.
func personalCall(method string, params any, out any) error {
	raw, _ := json.Marshal(params)
	var buf bytes.Buffer
	err := api.Call(api.SocketPath(daemon.SocketPath()), method, string(raw), &buf)
	if errors.Is(err, api.ErrNotRunning) {
		ps, err := openPersonal()
		if err != nil {
			return err
		}
		defer ps.Close()
		res, err := api.Local(api.Options{Personal: ps}, method, raw)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(res)
		return json.Unmarshal(b, out)
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(buf.Bytes(), out)
}

func openPersonal() (*personal.Store, error) {
	if err := config.InitConfig(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return personal.Open(filepath.Join(filepath.Dir(config.GetConfigFilePath()), "personal.db"))
}

func todoCmd(args []string) error {
	if len(args) == 0 {
		var items []api.ItemJSON
		if err := personalCall("today", nil, &items); err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("Nothing due today.")
			return nil
		}
		printItems(items, true)
		return nil
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Println(todoUsage)
		return nil
	case "lists":
		var lists []api.ListJSON
		if err := personalCall("lists", nil, &lists); err != nil {
			return err
		}
		for _, l := range lists {
			extra := ""
			if l.Kind == "tasks" {
				extra = fmt.Sprintf("  %d to do", l.Open)
			}
			if l.Archived {
				extra += "  (archived)"
			}
			fmt.Printf("%s %s%s\n", l.Icon, l.Name, extra)
		}
		return nil
	case "list", "show":
		if len(args) < 2 {
			return errors.New(todoUsage)
		}
		var items []api.ItemJSON
		if err := personalCall("tasks", map[string]any{"list": strings.Join(args[1:], " ")}, &items); err != nil {
			return err
		}
		printItems(items, false)
		return nil
	case "done", "undone", "rm", "delete":
		if len(args) < 2 {
			return errors.New(todoUsage)
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(args[1], "#"), 10, 64)
		if err != nil {
			return fmt.Errorf("%q isn't a task number", args[1])
		}
		var res any
		switch args[0] {
		case "rm", "delete":
			err = personalCall("task_delete", map[string]any{"id": id}, &res)
		default:
			err = personalCall("task_done", map[string]any{"id": id, "done": args[0] == "done"}, &res)
		}
		if err == nil {
			fmt.Println("ok")
		}
		return err
	}
	var it api.ItemJSON
	if err := personalCall("task_add", map[string]any{"text": strings.Join(args, " ")}, &it); err != nil {
		return err
	}
	fmt.Print("Added: ")
	printItems([]api.ItemJSON{it}, true)
	return nil
}

func noteCmd(args []string) error {
	if len(args) == 0 {
		return errors.New(todoUsage)
	}
	var it api.ItemJSON
	text := ""
	if len(args) > 1 {
		text = strings.Join(args[1:], " ")
	}
	if err := personalCall("note_add", map[string]any{"title": args[0], "text": text}, &it); err != nil {
		return err
	}
	fmt.Printf("Added the page %s to %s\n", it.Text, it.List)
	return nil
}

func printItems(items []api.ItemJSON, withList bool) {
	for _, it := range items {
		box := "☐"
		if it.Done {
			box = "☑"
		}
		indent := ""
		if it.Parent != 0 {
			indent = "    "
		}
		line := fmt.Sprintf("%s%s %s", indent, box, it.Text)
		if it.Important {
			line += " !"
		}
		for _, t := range it.Tags {
			line += " #" + t
		}
		var meta []string
		if it.Due != "" {
			meta = append(meta, describeDue(it))
		}
		if withList && it.List != "" {
			meta = append(meta, it.List)
		}
		meta = append(meta, "#"+strconv.FormatInt(it.ID, 10))
		fmt.Printf("%s  (%s)\n", line, strings.Join(meta, " · "))
	}
}

func describeDue(it api.ItemJSON) string {
	t, err := time.Parse(time.RFC3339, it.Due)
	if err != nil {
		return it.Due
	}
	now := time.Now()
	day := t.Format("Mon 2 Jan")
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		day = "today"
	case t.Sub(time.Date(y1, m1, d1, 0, 0, 0, 0, time.Local)) < 48*time.Hour && t.After(now):
		day = "tomorrow"
	}
	if it.DueTime {
		return day + " " + t.Format("15:04")
	}
	return day
}
