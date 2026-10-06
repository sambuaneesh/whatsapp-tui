package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/ai"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/personal"
)

type fakeAI struct {
	task, when string
	sugg       []ai.Suggestion
	guess      time.Time
	slots      func(tasks []ai.PlanTask) []ai.Slot
	sum        ai.Summary
	lines      []string
}

func (f *fakeAI) TaskFromMessage(_ context.Context, sender, text string) (string, string, error) {
	return f.task, f.when, nil
}
func (f *fakeAI) TasksInChat(_ context.Context, _ string, lines []string) ([]ai.Suggestion, error) {
	f.lines = lines
	return f.sugg, nil
}
func (f *fakeAI) GuessDate(context.Context, string, time.Time) (time.Time, bool, bool, error) {
	return f.guess, true, !f.guess.IsZero(), nil
}
func (f *fakeAI) PlanDay(_ context.Context, tasks []ai.PlanTask, _ time.Time) ([]ai.Slot, string, error) {
	return f.slots(tasks), "important first", nil
}
func (f *fakeAI) Summarize(_ context.Context, _ string, lines []string) (ai.Summary, error) {
	f.lines = lines
	return f.sum, nil
}

func aiEnv(t *testing.T, f *fakeAI) *personalEnv {
	t.Helper()
	e := personalModel(t)
	e.m.ai = f
	return e
}

// runCmds runs commands (not ticks) and feeds results back.
func (e *personalEnv) runCmds(t *testing.T, cmds ...tea.Cmd) {
	t.Helper()
	for _, c := range cmds {
		for _, msg := range drainMsgs(c) {
			if msg == nil {
				continue
			}
			next, more := e.m.Update(msg)
			e.m = next.(Model)
			e.runCmds(t, more)
		}
	}
}

func TestAIWordsAMessageAsATask(t *testing.T) {
	f := &fakeAI{task: "Send Arjun the slides", when: "before friday evening"}
	e := aiEnv(t, f)
	e.open(t, "Hostel")
	next, _ := e.m.Update(screenMsg(groupMsgs()))
	e.m = next.(Model)
	m, cmds := keys(t, e.m, "v", "k", "k", "T")
	e.m = m
	if e.m.cmdline.Value() != "task dinner at 8?" {
		t.Fatalf("prefill %q", e.m.cmdline.Value())
	}
	e.runCmds(t, cmds...)
	if e.m.cmdline.Value() != "task Send Arjun the slides before friday evening" || !strings.Contains(e.m.notice, "✨") {
		t.Fatalf("worded: %q (%s)", e.m.cmdline.Value(), e.m.notice)
	}
	e.key(t, "enter")
	inbox, _ := e.s.ListOfKind(personal.KindTasks, personal.InboxName)
	items, _ := e.s.Items(inbox.ID)
	if len(items) != 1 || items[0].Text != "Send Arjun the slides" || items[0].Due == 0 || items[0].SrcSender != "Arjun" {
		t.Fatalf("task: %+v", items)
	}
	if d := time.Unix(items[0].Due, 0); d.Weekday() != time.Friday || d.Hour() != 18 {
		t.Fatalf("due %v", d)
	}
}

func TestAIDoesNotOverwriteWhatYouTyped(t *testing.T) {
	f := &fakeAI{task: "Something else"}
	e := aiEnv(t, f)
	e.open(t, "Hostel")
	next, _ := e.m.Update(screenMsg(groupMsgs()))
	e.m = next.(Model)
	m, cmds := keys(t, e.m, "v", "T", "!")
	e.m = m
	e.runCmds(t, cmds...)
	if strings.Contains(e.m.cmdline.Value(), "Something else") {
		t.Fatal("the model's answer replaced what you typed")
	}
}

func TestAIFindsTodosInAChat(t *testing.T) {
	f := &fakeAI{sugg: []ai.Suggestion{{Task: "Book the cab", When: "tonight", From: "You"}, {Task: "Send Ravi the wifi password", From: "Ravi"}}}
	e := aiEnv(t, f)
	e.open(t, "Hostel")
	next, _ := e.m.Update(screenMsg(groupMsgs()))
	e.m = next.(Model)
	e.m, _ = press(t, e.m, tea.KeyF1)
	e.key(t, strings.Split("find to-dos", "")...)
	m, cmds := keys(t, e.m, "enter")
	e.m = m
	e.runCmds(t, cmds...)
	if len(f.lines) != 3 || f.lines[0] != "Arjun: dinner at 8?" || f.lines[2] != "You: count me in" {
		t.Fatalf("lines sent: %q", f.lines)
	}
	if e.m.qo == nil || len(e.m.qo.items) != 2 {
		t.Fatalf("suggestions not shown: %+v", e.m.qo)
	}
	e.key(t, "enter") // adds the first; the list stays open with the second
	if e.m.qo == nil || len(e.m.qo.items) != 1 || !strings.Contains(e.m.notice, "Book the cab") {
		t.Fatalf("after adding one: %+v %q", e.m.qo, e.m.notice)
	}
	e.key(t, "enter") // the last: the list closes
	if e.m.qo != nil {
		t.Fatal("should close when all are added")
	}
	inbox, _ := e.s.ListOfKind(personal.KindTasks, personal.InboxName)
	items, _ := e.s.Items(inbox.ID)
	if len(items) != 2 || !items[0].DueTime || items[1].SrcSender != "Ravi" {
		t.Fatalf("added: %+v", items)
	}
}

func TestAIPlansTheDay(t *testing.T) {
	f := &fakeAI{slots: func(tasks []ai.PlanTask) []ai.Slot {
		var out []ai.Slot
		for i, t := range tasks {
			out = append(out, ai.Slot{ID: t.ID, Time: []string{"21:00", "21:30", "22:00"}[i%3]})
		}
		return out
	}}
	e := aiEnv(t, f)
	e.open(t, "Today")
	e.typeIn(t, "gym")
	e.typeIn(t, "laundry")
	m, cmds := keys(t, e.m, ":", "p", "l", "a", "n", "enter")
	e.m = m
	e.runCmds(t, cmds...)
	if e.m.qo == nil || !strings.Contains(e.m.qo.pickTitle, "important first") || len(e.m.qo.items) != 3 {
		t.Fatalf("plan: %+v", e.m.qo)
	}
	e.key(t, "enter") // use it
	inbox, _ := e.s.ListOfKind(personal.KindTasks, personal.InboxName)
	items, _ := e.s.Items(inbox.ID)
	for _, it := range items {
		if !it.DueTime {
			t.Fatalf("not given a time: %+v", it)
		}
	}
	if !strings.Contains(e.m.notice, "Planned 2 tasks") {
		t.Fatalf("notice %q", e.m.notice)
	}
}

func TestAICatchUpAndAsks(t *testing.T) {
	f := &fakeAI{sum: ai.Summary{Points: []string{"Dinner at 8 at Paradise", "Arjun can't come"}, Asks: []string{"Tell Priya if you're coming"}}}
	e := aiEnv(t, f)
	e.open(t, "Hostel")
	next, _ := e.m.Update(screenMsg(groupMsgs()))
	e.m = next.(Model)
	m, cmds := keys(t, e.m, ":", "c", "a", "t", "c", "h", "u", "p", "enter")
	e.m = m
	e.runCmds(t, cmds...)
	if e.m.qo == nil || len(e.m.qo.items) != 3 || !strings.Contains(stripANSI(e.m.View()), "Dinner at 8 at Paradise") {
		t.Fatalf("summary: %+v", e.m.qo)
	}
	// the cursor starts on the ask; enter makes it a task
	e.key(t, "enter")
	inbox, _ := e.s.ListOfKind(personal.KindTasks, personal.InboxName)
	if items, _ := e.s.Items(inbox.ID); len(items) != 1 || items[0].Text != "Tell Priya if you're coming" {
		t.Fatalf("ask as task: %+v", items)
	}
}

func TestAIGuessesOddDates(t *testing.T) {
	guess := time.Now().AddDate(0, 0, 10)
	guess = time.Date(guess.Year(), guess.Month(), guess.Day(), 18, 0, 0, 0, time.Local)
	f := &fakeAI{guess: guess}
	e := aiEnv(t, f)
	e.open(t, "Inbox")
	e.typeIn(t, "pack for the trip")
	e.selectRow(t, "pack for the trip")
	m, cmds := keys(t, e.m, ":", "d", "u", "e", " ", "a", "f", "t", "e", "r", " ", "d", "i", "w", "a", "l", "i", "enter")
	e.m = m
	e.runCmds(t, cmds...)
	if e.m.mode != modeCommand || !strings.HasPrefix(e.m.cmdline.Value(), "due ") || !strings.Contains(e.m.notice, "“after diwali”") {
		t.Fatalf("guess: %q %q", e.m.cmdline.Value(), e.m.notice)
	}
	e.key(t, "enter") // accept
	if it, ok := e.m.selectedItem(); !ok || time.Unix(it.Due, 0).Day() != guess.Day() || !it.DueTime {
		t.Fatalf("accepted: %+v", it)
	}
	// :later too
	e.open(t, "Arjun")
	e.m.compose.SetValue("happy diwali!")
	m, cmds = keys(t, e.m, ":", "l", "a", "t", "e", "r", " ", "o", "n", " ", "d", "i", "w", "a", "l", "i", "enter")
	e.m = m
	e.runCmds(t, cmds...)
	if !strings.HasPrefix(e.m.cmdline.Value(), "later ") {
		t.Fatalf(":later guess: %q (%s)", e.m.cmdline.Value(), e.m.notice)
	}
	_ = messages.Message{}
}

func TestNoAIMeansNoAICommands(t *testing.T) {
	e := personalModel(t)
	e.open(t, "Hostel")
	e.m, _ = press(t, e.m, tea.KeyF1)
	for _, it := range e.m.qo.items {
		if strings.HasPrefix(it.title, "AI:") {
			t.Fatalf("%s shown without a model", it.title)
		}
	}
}
