package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestVisualRangeBulkActions(t *testing.T) {
	wrote := &copied{}
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{wrote: wrote})
	fw, del := &fakeForwarder{}, &fakeDeleter{}
	m.forwarder, m.deleter = fw, del

	// select the newest two: V on "count me in", k extends to "yes!"
	m, _ = keys(t, m, "v", "V", "k")
	if got := len(m.rangeMsgs()); got != 2 {
		t.Fatalf("range of %d", got)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "2 selected") {
		t.Fatalf("no count:\n%s", v)
	}
	// y: a transcript, oldest first
	m, cmds := keys(t, m, "y")
	runCmds(cmds...)
	if !strings.Contains(wrote.text, "Priya: yes!\n") || !strings.Contains(wrote.text, "You: count me in") ||
		strings.Index(wrote.text, "yes!") > strings.Index(wrote.text, "count me in") {
		t.Fatalf("transcript %q", wrote.text)
	}
	// f: both forwarded, in order
	m, _ = keys(t, m, "f")
	if m.fwd == nil || len(m.fwd.more) != 1 {
		t.Fatalf("forward picker %+v", m.fwd)
	}
	m, cmds = keys(t, m, "enter")
	runCmds(cmds...)
	if len(fw.calls) != 2 || !strings.HasPrefix(fw.calls[0], "m2>") || !strings.HasPrefix(fw.calls[1], "m3>") {
		t.Fatalf("forwarded %v", fw.calls)
	}
	// d then enter: both deleted for you
	m, _ = keys(t, m, "v", "V", "k", "d")
	if !strings.Contains(ansi.Strip(m.renderCommandLine()), "Delete these 2 messages?") {
		t.Fatalf("confirm %q", ansi.Strip(m.renderCommandLine()))
	}
	m, cmds = keys(t, m, "enter")
	runCmds(cmds...)
	if strings.Join(del.did, ",") != "me:m2,me:m3" {
		t.Fatalf("deleted %v", del.did)
	}
	// esc ends the range but stays in visual mode
	m, _ = keys(t, m, "v", "V", "k", "esc")
	if m.rangeFrom != noRange || m.mode != modeVisual {
		t.Fatalf("range %d mode %v", m.rangeFrom, m.mode)
	}
	// s with no media says so
	m, cmds = keys(t, m, "V", "k", "s")
	m = runAll(t, m, cmds...)
	if !m.noticeErr || !strings.Contains(m.notice, "no") {
		t.Fatalf("notice %q", m.notice)
	}
}
