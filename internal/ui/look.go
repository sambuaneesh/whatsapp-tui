package ui

import "strings"

// How messages look (Settings → Appearance): the space between them and
// the bubbles' borders.

type lookSettings struct {
	gap       int  // blank lines between messages
	senderGap bool // one more when the sender changes
	corners   [6]string
}

var bubbleStyles = map[string][6]string{ // ┌ ┐ └ ┘ ─ │
	"rounded": {"╭", "╮", "╰", "╯", "─", "│"},
	"square":  {"┌", "┐", "└", "┘", "─", "│"},
	"thick":   {"┏", "┓", "┗", "┛", "━", "┃"},
	"double":  {"╔", "╗", "╚", "╝", "═", "║"},
	"none":    {" ", " ", " ", " ", " ", " "},
}

// The choices, in the order Settings cycles through them.
var (
	spacingChoices = []string{"compact", "normal", "roomy"}
	bubbleChoices  = []string{"rounded", "square", "thick", "double", "none"}
)

var look = lookFor("normal", "rounded")

func lookFor(spacing, bubble string) lookSettings {
	l := lookSettings{gap: 1, senderGap: true, corners: bubbleStyles["rounded"]}
	switch strings.ToLower(spacing) {
	case "compact":
		l.gap = 0
	case "roomy":
		l.gap = 2
	}
	if c, ok := bubbleStyles[strings.ToLower(bubble)]; ok {
		l.corners = c
	}
	return l
}

// SetLook sets the spacing and bubble style (from the config at start,
// and from Settings).
func SetLook(spacing, bubble string) { look = lookFor(spacing, bubble) }
