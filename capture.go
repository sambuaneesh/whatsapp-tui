package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Srindot/whatsapp-tui/internal/api"
	"github.com/Srindot/whatsapp-tui/internal/personal"
)

// whatsapp-tui capture: a one-line prompt for a task (or "note: Title"),
// meant for a small floating window on a key. It shows what it read as
// you type: the list, the date, the tags.

var (
	capRose  = lipgloss.Color("#ebbcba")
	capGold  = lipgloss.Color("#f6c177")
	capFoam  = lipgloss.Color("#9ccfd8")
	capIris  = lipgloss.Color("#c4a7e7")
	capMuted = lipgloss.Color("#6e6a86")
	capLove  = lipgloss.Color("#eb6f92")
)

type captureModel struct {
	in    textinput.Model
	lists []string
	done  string
	err   error
	w     int
}

type captureDone struct {
	msg string
	err error
}

func captureCmd() error {
	var lists []api.ListJSON
	if err := personalCall("lists", nil, &lists); err != nil {
		return err
	}
	var names []string
	for _, l := range lists {
		if l.Kind == "tasks" {
			names = append(names, l.Name)
		}
	}
	in := textinput.New()
	in.Prompt = "❯ "
	in.PromptStyle = lipgloss.NewStyle().Foreground(capRose).Bold(true)
	in.Placeholder = "call mom 6pm #family ! · shopping: eggs · note: Title"
	in.PlaceholderStyle = lipgloss.NewStyle().Foreground(capMuted)
	in.Focus()
	_, err := tea.NewProgram(captureModel{in: in, lists: names}, tea.WithAltScreen()).Run()
	return err
}

func (m captureModel) Init() tea.Cmd { return textinput.Blink }

func (m captureModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w = msg.Width
		m.in.Width = max(msg.Width-6, 10)
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			return m, tea.Quit
		case "enter":
			text := strings.TrimSpace(m.in.Value())
			if text == "" {
				return m, tea.Quit
			}
			return m, func() tea.Msg { return captureAdd(text) }
		}
	case captureDone:
		m.done, m.err = msg.msg, msg.err
		if msg.err != nil {
			return m, nil
		}
		return m, tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg { return tea.Quit() })
	}
	var cmd tea.Cmd
	m.in, cmd = m.in.Update(msg)
	m.err = nil
	return m, cmd
}

func captureAdd(text string) captureDone {
	if title, ok := strings.CutPrefix(text, "note:"); ok {
		title, body, _ := strings.Cut(strings.TrimSpace(title), " - ")
		var it api.ItemJSON
		err := personalCall("note_add", map[string]any{"title": strings.TrimSpace(title), "text": body}, &it)
		return captureDone{msg: "📝 Added the page " + it.Text, err: err}
	}
	var it api.ItemJSON
	if err := personalCall("task_add", map[string]any{"text": text}, &it); err != nil {
		return captureDone{err: err}
	}
	msg := "✓ Added " + it.Text + " to " + it.List
	if it.Due != "" {
		msg += " · " + describeDue(it)
	}
	return captureDone{msg: msg}
}

// preview says what the text will make.
func (m captureModel) preview() string {
	text := strings.TrimSpace(m.in.Value())
	dim := lipgloss.NewStyle().Foreground(capMuted)
	if text == "" {
		return dim.Render("type a task as you'd say it · enter adds · esc closes")
	}
	if title, ok := strings.CutPrefix(text, "note:"); ok {
		return lipgloss.NewStyle().Foreground(capIris).Render("📝 a note page: " + strings.TrimSpace(title))
	}
	p := personal.ParseTask(text, time.Now(), m.lists)
	list := p.List
	if list == "" {
		list = personal.InboxName
	}
	parts := []string{lipgloss.NewStyle().Foreground(capIris).Render("→ " + list)}
	if !p.Due.IsZero() {
		d := p.Due.Format("Mon 2 Jan")
		if p.DueTime {
			d += " " + p.Due.Format("15:04")
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(capGold).Render("📅 "+d))
	}
	for _, t := range p.Tags {
		parts = append(parts, lipgloss.NewStyle().Foreground(capFoam).Render("#"+t))
	}
	if p.Important {
		parts = append(parts, lipgloss.NewStyle().Foreground(capLove).Bold(true).Render("! important"))
	}
	return lipgloss.NewStyle().Render(fmt.Sprintf("%s  %s", strings.Join(parts, "  "), dim.Render("“"+p.Text+"”")))
}

func (m captureModel) View() string {
	title := lipgloss.NewStyle().Foreground(capIris).Bold(true).Render(" 📋 add to your lists")
	line := m.preview()
	switch {
	case m.err != nil:
		line = lipgloss.NewStyle().Foreground(capLove).Render(m.err.Error())
	case m.done != "":
		line = lipgloss.NewStyle().Foreground(capFoam).Bold(true).Render(m.done)
	}
	return "\n" + title + "\n\n " + m.in.View() + "\n\n " + line + "\n"
}
