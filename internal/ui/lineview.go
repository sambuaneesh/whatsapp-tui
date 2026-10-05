package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// lineView scrolls through rendered lines. It replaces the bubbles
// viewport, which split and measured the whole content on every update and
// padded it again on every frame; this keeps the lines as rendered and only
// touches the ones on screen.
type lineView struct {
	Width, Height int
	YOffset       int
	lines         []string
}

// SetLines replaces the content, keeping the scroll position in range.
func (v *lineView) SetLines(lines []string) {
	v.lines = lines
	v.SetYOffset(v.YOffset)
}

// SetContent is SetLines for text with newlines.
func (v *lineView) SetContent(s string) { v.SetLines(strings.Split(s, "\n")) }

func (v lineView) maxYOffset() int { return max(len(v.lines)-v.Height, 0) }

// SetYOffset scrolls to line n (clamped).
func (v *lineView) SetYOffset(n int) { v.YOffset = min(max(n, 0), v.maxYOffset()) }

func (v lineView) AtTop() bool         { return v.YOffset <= 0 }
func (v lineView) AtBottom() bool      { return v.YOffset >= v.maxYOffset() }
func (v lineView) TotalLineCount() int { return len(v.lines) }
func (v *lineView) GotoTop()           { v.YOffset = 0 }
func (v *lineView) GotoBottom()        { v.YOffset = v.maxYOffset() }
func (v *lineView) LineDown(n int)     { v.SetYOffset(v.YOffset + n) }
func (v *lineView) LineUp(n int)       { v.SetYOffset(v.YOffset - n) }
func (v *lineView) ScrollUp(n int)     { v.LineUp(n) }
func (v *lineView) ScrollDown(n int)   { v.LineDown(n) }
func (v *lineView) ViewDown()          { v.LineDown(v.Height) }
func (v *lineView) ViewUp()            { v.LineUp(v.Height) }

// View is the lines on screen, each padded to the width (and blank lines
// up to the height).
func (v lineView) View() string {
	var b strings.Builder
	for i := 0; i < v.Height; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := ""
		if j := v.YOffset + i; j < len(v.lines) {
			line = v.lines[j]
		}
		w := ansi.StringWidth(line)
		if w > v.Width {
			line, w = ansi.Truncate(line, v.Width, ""), v.Width
		}
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", v.Width-w))
	}
	return b.String()
}
