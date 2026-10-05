package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// runCmds runs what a step returned (no follow-up updates).
func runCmds(cmds ...tea.Cmd) {
	for _, c := range cmds {
		if c == nil {
			continue
		}
		if b, ok := c().(tea.BatchMsg); ok {
			runCmds(b...)
		}
	}
}

func TestQuickBarKeysAndClicks(t *testing.T) {
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{})
	m, cmds := keys(t, m, "v", "r", "2") // newest: m3
	runCmds(cmds...)
	if len(a.reactions) != 1 || a.reactions[0] != "m3|❤️" || m.picker {
		t.Fatalf("reactions %v, picker %v", a.reactions, m.picker)
	}
	// click a chip on the bar
	m, _ = keys(t, m, "r")
	bar := ansi.Strip(m.renderCommandLine())
	x := ansi.StringWidth(bar[:strings.Index(bar, "😂")])
	m, cmd := click(t, m, x, m.height-1)
	runCmds(cmd)
	if len(a.reactions) != 2 || a.reactions[1] != "m3|😂" {
		t.Fatalf("click on 😂: %v", a.reactions)
	}
	// "x remove" chip
	m, _ = keys(t, m, "r")
	bar = ansi.Strip(m.renderCommandLine())
	m, cmd = click(t, m, ansi.StringWidth(bar[:strings.Index(bar, "x remove")])+2, m.height-1)
	runCmds(cmd)
	if a.reactions[2] != "m3|" {
		t.Fatalf("remove: %v", a.reactions)
	}
}

func TestRightClickOpensQuickBar(t *testing.T) {
	m := visualModel(t, &fakeActions{}, fakeClip{})
	x, y := findLast(t, m, "yes!")
	next, _ := m.Update(tea.MouseMsg{X: x + 1, Y: y, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	m = next.(Model)
	if sel, _ := m.selected(); m.mode != modeVisual || !m.picker || sel.Id != "m2" {
		t.Fatalf("mode %v picker %v sel %s", m.mode, m.picker, sel.Id)
	}
	if !strings.Contains(ansi.Strip(m.View()), "1 👍") {
		t.Fatal("quick bar not shown")
	}
}

func TestEmojiGrid(t *testing.T) {
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{})
	// typing a name in the quick bar opens the grid on it
	m, _ = keys(t, m, "v", "r", "f", "i", "r", "e")
	if m.emo == nil || m.emo.query != "fire" || m.emo.items[0].Char != "🔥" {
		t.Fatalf("grid %+v", m.emo)
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "🔥") || !strings.Contains(v, "EMOJI") {
		t.Fatalf("grid not drawn:\n%s", v)
	}
	m, cmds := keys(t, m, "enter")
	runCmds(cmds...)
	if m.emo != nil || len(a.reactions) != 1 || a.reactions[0] != "m3|🔥" {
		t.Fatalf("after enter: %v", a.reactions)
	}
	// + opens it empty; arrows move; a click picks
	m, _ = keys(t, m, "r", "+")
	if m.emo == nil || m.emo.query != "" {
		t.Fatal("+ didn't open the grid")
	}
	m, _ = keys(t, m, "right", "right")
	if m.emo.cursor != 2 {
		t.Fatalf("cursor %d", m.emo.cursor)
	}
	want := m.emo.items[1].Char
	m, cmd := click(t, m, 2+emojiCell+1, emojiGridTop)
	runCmds(cmd)
	if a.reactions[1] != "m3|"+want {
		t.Fatalf("click picked %v, want %s", a.reactions, want)
	}
	// backspace edits the search; esc goes back without reacting
	m, _ = keys(t, m, "r", "t", "e", "a", "backspace", "esc")
	if m.emo != nil || len(a.reactions) != 2 {
		t.Fatal("esc reacted or didn't close")
	}
}

func TestWhoReacted(t *testing.T) {
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{})
	// click the ❤️ under "yes!" (m2: 👍 from you and a, ❤️ from b)
	x, y := findLast(t, m, "❤️")
	m, cmd := click(t, m, x, y)
	if m.reactors == nil || m.reactors.msgID != "m2" {
		t.Fatalf("panel %+v", m.reactors)
	}
	rows := m.reactorRows()
	if len(rows) != 3 || !rows[0].mine || rows[0].emoji != "👍" || rows[2].emoji != "❤️" || m.reactors.cursor != 2 {
		t.Fatalf("rows %+v cursor %d", rows, m.reactors.cursor)
	}
	// names load for the others
	m = drain(t, m, cmd)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "You") || !strings.Contains(v, "Priya") || !strings.Contains(v, "REACTIONS") {
		t.Fatalf("panel:\n%s", v)
	}
	// x on someone else's does nothing; on yours removes it
	m, _ = keys(t, m, "x")
	if !m.noticeErr || len(a.reactions) != 0 {
		t.Fatal("removed someone else's reaction")
	}
	m, cmds := keys(t, m, "k", "k", "x")
	runCmds(cmds...)
	if len(a.reactions) != 1 || a.reactions[0] != "m2|" {
		t.Fatalf("remove: %v", a.reactions)
	}
	// w in visual mode opens it too; clicking your row removes
	m.reactors = nil
	m, _ = keys(t, m, "v", "k", "w")
	if m.reactors == nil || m.reactors.msgID != "m2" {
		t.Fatal("w didn't open the list")
	}
	m, cmd = click(t, m, 5, reactorsTop)
	runCmds(cmd)
	if len(a.reactions) != 2 {
		t.Fatalf("click on your row: %v", a.reactions)
	}
	m, _ = keys(t, m, "esc")
	if m.reactors != nil {
		t.Fatal("esc didn't close")
	}
}
