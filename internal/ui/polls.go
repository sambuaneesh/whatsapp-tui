package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Polls show their results: each option with its votes and a bar, ● on the
// ones you picked. i on a poll (visual mode) lists who voted for what.

// pollLines renders a poll's question and results to fit maxInner cells.
func pollLines(p *messages.Poll, maxInner int) []string {
	lines := formatText("📊 "+p.Question, maxInner, styleBase.Bold(true), nil)
	widest := 0
	for _, o := range p.Options {
		widest = max(widest, ansi.StringWidth(o.Name))
	}
	barW := min(max(widest+6, 16), maxInner)
	bar := lipgloss.NewStyle().Foreground(pal.Foam)
	mineBar := lipgloss.NewStyle().Foreground(pal.Iris)
	empty := lipgloss.NewStyle().Foreground(pal.HighlightMed)
	for _, o := range p.Options {
		mark, style := "○ ", styleBase
		if o.Mine {
			mark, style = "● ", styleBase.Bold(true)
		}
		n := fmt.Sprint(len(o.Voters))
		name := ansi.Truncate(o.Name, max(barW-2-len(n)-1, 4), "…")
		gap := max(barW-2-ansi.StringWidth(name)-len(n), 1)
		lines = append(lines, style.Render(mark+name)+strings.Repeat(" ", gap)+styleDim.Render(n))
		filled := 0
		if p.Voters > 0 {
			filled = (len(o.Voters)*barW + p.Voters/2) / p.Voters
			if len(o.Voters) > 0 {
				filled = max(filled, 1)
			}
		}
		b := bar
		if o.Mine {
			b = mineBar
		}
		lines = append(lines, b.Render(strings.Repeat("━", filled))+empty.Render(strings.Repeat("━", barW-filled)))
	}
	foot := fmt.Sprintf("%d %s", p.Voters, plural(p.Voters, "vote", "votes"))
	if p.Max > 1 {
		foot += fmt.Sprintf(" · pick up to %d", p.Max)
	}
	return append(lines, styleMuted.Render(foot))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// pollInfoLines lists who voted for each option (i on a poll).
func pollInfoLines(p *messages.Poll, width int) []string {
	lines := []string{
		styleTitle.Render("Poll") + styleDim.Render(fmt.Sprintf("  ·  %d %s", p.Voters, plural(p.Voters, "vote", "votes"))),
		styleDim.Render(ansi.Truncate(p.Question, max(width-6, 10), "…")),
		"",
	}
	for _, o := range p.Options {
		title := o.Name
		if o.Mine {
			title = "● " + title
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(pal.Foam).Bold(true).Render(fmt.Sprintf("%s  %d", title, len(o.Voters))))
		if len(o.Voters) == 0 {
			lines = append(lines, styleMuted.Render("  no votes"))
		}
		for _, v := range o.Voters {
			lines = append(lines, "  "+styleBase.Render(v.Name))
		}
		lines = append(lines, "")
	}
	return lines
}
