package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/ai"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/personal"
)

// The local model's help (Options.AI; nil when off): a message worded as
// a task (T), the to-dos found in a chat, today planned, and a chat
// caught up. Answers arrive
// as messages; nothing waits on them.

// Assistant is the local model; *ai.Client implements it.
type Assistant interface {
	TaskFromMessage(ctx context.Context, sender, text string) (task, when string, err error)
	TasksInChat(ctx context.Context, chat string, lines []string) ([]ai.Suggestion, error)
	PlanDay(ctx context.Context, tasks []ai.PlanTask, now time.Time) ([]ai.Slot, string, error)
	Summarize(ctx context.Context, chat string, lines []string) (ai.Summary, error)
}

const aiTimeout = 90 * time.Second // the first answer loads the model

func aiCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), aiTimeout)
}

// ---------- T: a message worded as a task ----------

type aiTaskMsg struct {
	prefill    string // what :task showed while it thought
	task, when string
	err        error
}

// aiTaskFromMessage asks the model to word the selected message as a
// task; the :task line is replaced with it unless you've typed meanwhile.
func (m Model) aiTaskFromMessage(sel messages.Message) tea.Cmd {
	a := m.ai
	if a == nil {
		return nil
	}
	sender := ""
	if !sel.FromMe {
		sender = sel.ContactShort
		if sender == "" {
			sender = sel.ContactName
		}
	}
	text := strings.Join(strings.Fields(plainText(prettyTags(sel.Text))), " ")
	prefill := m.cmdline.Value()
	return func() tea.Msg {
		ctx, cancel := aiCtx()
		defer cancel()
		task, when, err := a.TaskFromMessage(ctx, sender, text)
		return aiTaskMsg{prefill: prefill, task: task, when: when, err: err}
	}
}

func (m Model) applyAITask(r aiTaskMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeCommand || m.cmdline.Value() != r.prefill {
		return m, nil // you moved on, or typed
	}
	if r.err != nil || r.task == "" {
		if r.err != nil {
			m.notice, m.noticeErr = "local model: "+r.err.Error(), true
		}
		return m, nil
	}
	line := "task " + r.task
	if r.when != "" {
		if _, _, ok := personal.FindDate(r.when, time.Now()); ok {
			line += " " + r.when // read by the parser when you press enter
		}
	}
	m.cmdline.SetValue(line)
	m.cmdline.CursorEnd()
	m.notice, m.noticeErr = "✨ worded by the local model · edit it, enter adds", false
	return m, nil
}

// ---------- chat lines for the model ----------

// chatLines is the open chat's latest messages as "name: text", oldest
// first.
func (m Model) chatLines(n int) []string {
	start := max(len(m.msgs)-n, 0)
	var lines []string
	for _, msg := range m.msgs[start:] {
		who := "You"
		if !msg.FromMe {
			who = msg.ContactShort
			if who == "" {
				who = msg.ContactName
			}
			if who == "" && m.current != nil && !isGroup(m.current.JID) {
				who = chatName(m.current)
			}
		}
		text := strings.Join(strings.Fields(plainText(prettyTags(msg.Text))), " ")
		if text == "" || strings.HasPrefix(msg.Text, "[REACTION]") {
			continue
		}
		if r := []rune(text); len(r) > 400 {
			text = string(r[:400]) + "…"
		}
		lines = append(lines, who+": "+text)
	}
	return lines
}

func (m Model) needAI(what string) (Model, bool) {
	if m.ai == nil {
		m.notice, m.noticeErr = what+" needs the local model: turn on ai in config.ini and run ollama", true
		return m, false
	}
	return m, true
}

// ---------- the to-dos in a chat ----------

type aiSuggestMsg struct {
	chat, name string
	sugg       []ai.Suggestion
	err        error
}

func (m Model) findTasksInChat() (tea.Model, tea.Cmd) {
	m, ok := m.needAI("finding to-dos")
	if !ok || !inChat(m) {
		return m, nil
	}
	lines := m.chatLines(60)
	if len(lines) == 0 {
		m.notice, m.noticeErr = "no messages loaded here yet", true
		return m, nil
	}
	a, chat, name := m.ai, m.current.JID, chatName(m.current)
	m.notice, m.noticeErr = "✨ the local model is reading the last "+fmt.Sprint(len(lines))+" messages…", false
	return m, func() tea.Msg {
		ctx, cancel := aiCtx()
		defer cancel()
		sugg, err := a.TasksInChat(ctx, name, lines)
		return aiSuggestMsg{chat: chat, name: name, sugg: sugg, err: err}
	}
}

func (m Model) applySuggestions(r aiSuggestMsg) (tea.Model, tea.Cmd) {
	if r.err != nil {
		m.notice, m.noticeErr = "local model: "+r.err.Error(), true
		return m, nil
	}
	if len(r.sugg) == 0 {
		m.notice, m.noticeErr = "✨ no to-dos found in "+r.name, false
		return m, nil
	}
	m.notice = ""
	var items []palItem
	for _, s := range r.sugg {
		s := s
		right := s.From
		if s.When != "" {
			right = s.When + " · " + right
		}
		items = append(items, palItem{action: &palAction{label: "☐ " + s.Task, right: right, keep: true,
			run: func(m Model) (Model, string) {
				text := s.Task
				if s.When != "" {
					if _, _, ok := personal.FindDate(s.When, time.Now()); ok {
						text += " " + s.When
					}
				}
				from := personal.Item{SrcChat: r.chat, SrcSender: s.From}
				it, _, err := m.personal.AddTyped(text, 0, false, from)
				if err != nil {
					return m, err.Error()
				}
				m.refreshPersonal()
				label := "Added " + it.Text
				if it.Due > 0 {
					label += " · " + dueLabel(it, time.Now())
				}
				return m, label
			}}})
	}
	m.openActions("✨ To-dos in "+r.name+" · enter adds to 📥 Inbox", items)
	return m, nil
}

// ---------- planning today ----------

type aiPlanMsg struct {
	slots []ai.Slot
	note  string
	tasks map[int64]personal.Item
	err   error
}

func (m Model) planDay() (tea.Model, tea.Cmd) {
	m, ok := m.needAI("planning")
	if !ok || m.personal == nil {
		return m, nil
	}
	now := time.Now()
	_, end := dayBounds(now)
	due, err := m.personal.Dated(end)
	if err != nil || len(due) == 0 {
		m.notice, m.noticeErr = "nothing due today to plan", true
		return m, nil
	}
	tasks := map[int64]personal.Item{}
	var in []ai.PlanTask
	for _, it := range due {
		if it.ParentID != 0 {
			continue
		}
		tasks[it.ID] = it
		p := ai.PlanTask{ID: it.ID, Text: it.Text, Important: it.Important}
		if it.DueTime && it.Due > now.Unix() {
			p.At = time.Unix(it.Due, 0).Format("15:04")
		}
		in = append(in, p)
	}
	a := m.ai
	m.notice, m.noticeErr = fmt.Sprintf("✨ the local model is planning %d tasks…", len(in)), false
	return m, func() tea.Msg {
		ctx, cancel := aiCtx()
		defer cancel()
		slots, note, err := a.PlanDay(ctx, in, now)
		return aiPlanMsg{slots: slots, note: note, tasks: tasks, err: err}
	}
}

func (m Model) applyPlan(r aiPlanMsg) (tea.Model, tea.Cmd) {
	if r.err != nil {
		m.notice, m.noticeErr = "local model: "+r.err.Error(), true
		return m, nil
	}
	now := time.Now()
	start, end := dayBounds(now)
	type planned struct {
		it personal.Item
		at time.Time
	}
	var plan []planned
	for _, s := range r.slots {
		it, ok := r.tasks[s.ID]
		tm, err := time.Parse("15:04", s.Time)
		if !ok || err != nil {
			continue
		}
		at := start.Add(time.Duration(tm.Hour())*time.Hour + time.Duration(tm.Minute())*time.Minute)
		if at.After(end) {
			continue
		}
		plan = append(plan, planned{it, at})
	}
	if len(plan) == 0 {
		m.notice, m.noticeErr = "the local model's plan didn't fit today", true
		return m, nil
	}
	for i := 1; i < len(plan); i++ { // in time order
		for j := i; j > 0 && plan[j].at.Before(plan[j-1].at); j-- {
			plan[j], plan[j-1] = plan[j-1], plan[j]
		}
	}
	items := []palItem{{action: &palAction{label: "✓ Use this plan (sets these times; u undoes each)", right: "enter",
		run: func(m Model) (Model, string) {
			n := 0
			for _, p := range plan {
				it := p.it
				it.Due, it.DueTime = p.at.Unix(), true
				if m.personal.Update(it) == nil {
					n++
				}
			}
			m.refreshPersonal()
			if m.inPersonal() {
				m.refreshMessages(false)
			}
			return m, fmt.Sprintf("Planned %d tasks for today", n)
		}}}}
	for _, p := range plan {
		items = append(items, palItem{action: &palAction{label: p.at.Format("15:04") + "  " + p.it.Text, right: "", info: true}})
	}
	title := "✨ Plan for today"
	if r.note != "" {
		title += " · " + r.note
	}
	m.notice = ""
	m.openActions(title, items)
	return m, nil
}

// ---------- catching up ----------

type aiSummaryMsg struct {
	chat, name string
	sum        ai.Summary
	n          int
	err        error
}

func (m Model) catchUp() (tea.Model, tea.Cmd) {
	m, ok := m.needAI("catching up")
	if !ok || !inChat(m) {
		return m, nil
	}
	n := 60
	if m.current.Unread > 0 {
		n = min(max(int(m.current.Unread)+10, 30), 150)
	}
	lines := m.chatLines(n)
	if len(lines) == 0 {
		m.notice, m.noticeErr = "no messages loaded here yet", true
		return m, nil
	}
	a, chat, name := m.ai, m.current.JID, chatName(m.current)
	m.notice, m.noticeErr = fmt.Sprintf("✨ the local model is reading the last %d messages…", len(lines)), false
	return m, func() tea.Msg {
		ctx, cancel := aiCtx()
		defer cancel()
		sum, err := a.Summarize(ctx, name, lines)
		return aiSummaryMsg{chat: chat, name: name, sum: sum, n: len(lines), err: err}
	}
}

func (m Model) applySummary(r aiSummaryMsg) (tea.Model, tea.Cmd) {
	if r.err != nil {
		m.notice, m.noticeErr = "local model: "+r.err.Error(), true
		return m, nil
	}
	var items []palItem
	for _, p := range r.sum.Points {
		items = append(items, palItem{action: &palAction{label: "• " + p, info: true}})
	}
	for _, ask := range r.sum.Asks {
		ask := ask
		items = append(items, palItem{action: &palAction{label: "☐ " + ask, right: "enter: a task", keep: true,
			run: func(m Model) (Model, string) {
				if m.personal == nil {
					return m, "lists are off"
				}
				it, _, err := m.personal.AddTyped(ask, 0, false, personal.Item{SrcChat: r.chat})
				if err != nil {
					return m, err.Error()
				}
				m.refreshPersonal()
				return m, "Added " + it.Text + " to 📥 Inbox"
			}}})
	}
	if len(items) == 0 {
		m.notice, m.noticeErr = "✨ nothing to catch up on", false
		return m, nil
	}
	m.notice = ""
	m.openActions(fmt.Sprintf("✨ %s · the last %d messages", r.name, r.n), items)
	return m, nil
}

// applyAI handles the model's answers; ok is false for other messages.
func (m Model) applyAI(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch r := msg.(type) {
	case aiTaskMsg:
		next, cmd := m.applyAITask(r)
		return next, cmd, true
	case aiSuggestMsg:
		next, cmd := m.applySuggestions(r)
		return next, cmd, true
	case aiPlanMsg:
		next, cmd := m.applyPlan(r)
		return next, cmd, true
	case aiSummaryMsg:
		next, cmd := m.applySummary(r)
		return next, cmd, true
	}
	return m, nil, false
}
