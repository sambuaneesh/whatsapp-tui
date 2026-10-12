package ui

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"
)

// The colours of the people in a group: each gets one when first seen, and
// keeps it for the session. A colour someone else in the chat already has
// is skipped while free ones remain, so up to eight people never share.

type groupColors struct {
	chat string
	byID map[string]int
	used map[int]bool
}

func (g *groupColors) index(chat, id string) int {
	n := len(pal.senders)
	if g.chat != chat || g.byID == nil {
		g.chat, g.byID, g.used = chat, map[string]int{}, map[int]bool{}
	}
	if i, ok := g.byID[id]; ok {
		return i
	}
	h := fnv.New32a()
	h.Write([]byte(id))
	i := int(h.Sum32() % uint32(n))
	for step := 0; step < n && g.used[i] && len(g.used) < n; step++ {
		i = (i + 1) % n
	}
	g.byID[id], g.used[i] = i, true
	return i
}

// groupColor is a member's colour in the open chat (their hash colour
// elsewhere).
func (m Model) groupColor(id string) lipgloss.Color {
	if m.gcolors == nil || m.current == nil || !isGroup(m.current.JID) {
		return senderColor(id)
	}
	return pal.senders[m.gcolors.index(m.current.JID, id)]
}
