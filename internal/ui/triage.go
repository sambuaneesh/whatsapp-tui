package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Triage keys, for going through chats like an inbox:
//
//	e  done: mark read and archive (in the archive: back to the inbox);
//	   inside a chat it then opens the next unread one
//	U  mark unread (or read)
//	J  open the next unread chat

// Triager changes chats on all your devices; *messages.SessionManager
// implements it.
type Triager interface {
	ArchiveChat(ctx context.Context, jid string, archive bool) error
	MarkChatUnread(ctx context.Context, jid string, unread bool) error
}

func (m *Model) triageCmd(ok string, f func(ctx context.Context, t Triager) error) tea.Cmd {
	t := m.triage
	if t == nil {
		m.notice, m.noticeErr = "this isn't available", true
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return actionDoneMsg{ok: ok, err: f(ctx, t)}
	}
}

// doneChat marks a chat read and archives it, or brings an archived chat
// back to the inbox.
func (m *Model) doneChat(c *messages.Conversation, note string) tea.Cmd {
	if c == nil {
		return nil
	}
	jid, name := c.JID, chatName(c)
	if c.IsArchived {
		return m.triageCmd("Moved "+name+" back to the inbox", func(ctx context.Context, t Triager) error {
			return t.ArchiveChat(ctx, jid, false)
		})
	}
	return m.triageCmd("Done with "+name+" (archived; A shows the archive)"+note, func(ctx context.Context, t Triager) error {
		if err := t.MarkChatUnread(ctx, jid, false); err != nil {
			return err
		}
		return t.ArchiveChat(ctx, jid, true)
	})
}

// archiveChat archives a chat (leaving it unread) or brings it back.
func (m *Model) archiveChat(c *messages.Conversation, archive bool) tea.Cmd {
	if c == nil {
		return nil
	}
	jid, name := c.JID, chatName(c)
	label := "Archived " + name + " (A shows the archive)"
	if !archive {
		label = "Moved " + name + " back to the inbox"
	}
	return m.triageCmd(label, func(ctx context.Context, t Triager) error {
		return t.ArchiveChat(ctx, jid, archive)
	})
}

// toggleUnread marks a read chat unread, or an unread one read.
func (m *Model) toggleUnread(c *messages.Conversation) tea.Cmd {
	if c == nil {
		return nil
	}
	jid, name := c.JID, chatName(c)
	unread := c.Unread == 0 && !c.Mentioned
	label := "Marked " + name + " read"
	if unread {
		label = "Marked " + name + " unread"
	}
	return m.triageCmd(label, func(ctx context.Context, t Triager) error {
		return t.MarkChatUnread(ctx, jid, unread)
	})
}

// nextUnread opens the next unread chat in the list after the cursor
// (wrapping), skipping skip (the chat just dealt with). ok is false when
// there's none.
func (m *Model) nextUnread(skip string) (tea.Cmd, bool) {
	n := m.listLen()
	for step := 1; step <= n; step++ {
		i := (m.cursor + step) % n
		c, _ := m.itemAt(i)
		if c == nil || c.JID == skip || (c.Unread == 0 && !c.Mentioned) || isPersonal(c.JID) {
			continue
		}
		m.cursor = i
		m.clampCursor()
		return m.openSelected(), true
	}
	return nil, false
}

// triageKey handles e, U and J in the chat list and in a chat. inChat is
// whether it applies to the open chat (else the selected one in the list).
func (m Model) triageKey(key string, inChat bool) (tea.Model, tea.Cmd) {
	c := m.selectedChat()
	if inChat {
		c = m.current
	}
	switch key {
	case "e":
		if !inChat || c == nil {
			return m, m.doneChat(c, "")
		}
		// on to the next unread chat, or back to the list: inbox zero
		next, ok := m.nextUnread(c.JID)
		note := ""
		if !ok {
			next = m.back()
			note = " · no more unread chats"
		}
		return m, tea.Batch(m.doneChat(c, note), next)
	case "U":
		cmd := m.toggleUnread(c)
		if inChat {
			// leave it, or opening it would mark it read again
			return m, tea.Batch(cmd, m.back())
		}
		return m, cmd
	case "J":
		skip := ""
		if m.current != nil && m.screen == screenChat {
			skip = m.current.JID
		}
		next, ok := m.nextUnread(skip)
		if !ok {
			m.notice, m.noticeErr = "no unread chats: all caught up", false
		}
		return m, next
	}
	return m, nil
}

// Resyncer makes this device's chat settings match WhatsApp's;
// *messages.SessionManager implements it.
type Resyncer interface {
	ResyncChatSettings(ctx context.Context) (int, error)
}

// resync fetches archive, pin and mute settings again in full: for when
// the phone and this device disagree about a chat.
func (m Model) resync() tea.Cmd {
	r, ok := m.triage.(Resyncer)
	if !ok {
		r, ok = m.actions.(Resyncer)
	}
	if !ok {
		return func() tea.Msg { return actionDoneMsg{err: errors.New("resync isn't available")} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		n, err := r.ResyncChatSettings(ctx)
		msg := "Chat settings match your phone again"
		switch {
		case n == 1:
			msg += " (1 chat fixed)"
		case n > 1:
			msg += fmt.Sprintf(" (%d chats fixed)", n)
		}
		return actionDoneMsg{ok: msg, err: err}
	}
}
