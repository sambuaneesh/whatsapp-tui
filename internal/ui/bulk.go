package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Bulk actions in visual mode: V starts a range at the selected message,
// j/k extend it, then y copies, f forwards, d deletes and s saves the media
// of every message in it. esc (or V) ends the range.

// noRange is rangeFrom when no range is being selected.
const noRange = -1

// inRange reports whether message i is in the visual-mode range.
func (m Model) inRange(i int) bool {
	if m.mode != modeVisual || m.rangeFrom == noRange {
		return false
	}
	lo, hi := min(m.rangeFrom, m.sel), max(m.rangeFrom, m.sel)
	return i >= lo && i <= hi
}

// rangeMsgs is the range's messages, oldest first (nil without a range).
func (m Model) rangeMsgs() []messages.Message {
	if m.rangeFrom == noRange || m.rangeFrom >= len(m.msgs) {
		return nil
	}
	lo, hi := min(m.rangeFrom, m.sel), max(m.rangeFrom, m.sel)
	return append([]messages.Message(nil), m.msgs[lo:hi+1]...)
}

// handleRange handles keys while a range is selected; ok is false for
// keys it leaves to visual mode (moving, searching).
func (m Model) handleRange(key string) (tea.Model, tea.Cmd, bool) {
	msgs := m.rangeMsgs()
	switch key {
	case "V", "esc":
		m.rangeFrom = noRange
		m.refreshMessages(false)
		return m, nil, true
	case "y":
		return m, m.copyTranscript(msgs), true
	case "f":
		cmd := m.openForward(msgs[0])
		if m.fwd != nil {
			m.fwd.more = msgs[1:]
		}
		return m, cmd, true
	case "d":
		if m.deleter == nil {
			m.notice, m.noticeErr = "deleting isn't available", true
			return m, nil, true
		}
		m.confirm = &confirmDelete{msgs: msgs}
		return m, nil, true
	case "s":
		return m, m.saveAll(msgs), true
	case "enter", "p", "r", "e", "w", "o", " ", "space", "R", "P", "T", "b":
		m.notice, m.noticeErr = "with several selected: y copy · f forward · d delete · s save media · V or esc ends", true
		return m, nil, true
	}
	return m, nil, false
}

// copyTranscript copies the messages as "[Mon 12 Oct 18:00] Name: text".
func (m Model) copyTranscript(msgs []messages.Message) tea.Cmd {
	var b strings.Builder
	for _, x := range msgs {
		fmt.Fprintf(&b, "[%s] %s: %s\n", toTime(int64(x.Timestamp)).Format("Mon 2 Jan 15:04"), m.senderName(x), prettyTags(x.Text))
	}
	clip, text := m.clip, b.String()
	return func() tea.Msg {
		return actionDoneMsg{ok: fmt.Sprintf("Copied %d messages", len(msgs)), err: clip.WriteText(text)}
	}
}

// saveAll downloads the media of every message that has any.
func (m Model) saveAll(msgs []messages.Message) tea.Cmd {
	var withMedia []messages.Message
	for _, x := range msgs {
		if len(x.Media) > 0 {
			withMedia = append(withMedia, x)
		}
	}
	if len(withMedia) == 0 {
		return func() tea.Msg { return actionDoneMsg{err: errors.New("none of these has media to save")} }
	}
	a, dir := m.actions, downloadDir()
	return m.action("", func(ctx context.Context) (string, error) {
		if a == nil {
			return "", errors.New("downloads aren't available")
		}
		for _, x := range withMedia {
			if _, err := a.SaveMedia(ctx, x.Id, dir); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("Saved %d files to %s", len(withMedia), tildePath(dir)), nil
	})
}
