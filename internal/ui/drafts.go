package ui

import tea "github.com/charmbracelet/bubbletea"

// DraftStore keeps what you were writing in each chat, across restarts;
// *messages.SessionManager implements it.
type DraftStore interface {
	SaveDraft(jid, text string) error
	Drafts() map[string]string
}

// stashDraft keeps what's being written in the open chat (when leaving it,
// or when the window goes to the background) and saves it in the
// background. Sending empties the box, which removes the draft.
func (m *Model) stashDraft() tea.Cmd {
	if m.current == nil {
		return nil
	}
	text := m.compose.Value()
	if m.editing != nil {
		text = m.draft // the box holds the message being edited
	}
	jid := m.current.JID
	if m.drafts[jid] == text {
		return nil
	}
	if text == "" {
		delete(m.drafts, jid)
	} else {
		m.drafts[jid] = text
	}
	store := m.draftStore
	if store == nil || isPersonal(jid) {
		return nil
	}
	return func() tea.Msg {
		if err := store.SaveDraft(jid, text); err != nil {
			return actionDoneMsg{err: err}
		}
		return nil
	}
}

// restoreDraft puts back what was being written in the chat just opened.
func (m *Model) restoreDraft() {
	if m.current == nil {
		return
	}
	m.compose.SetValue(m.drafts[m.current.JID])
	m.compose.CursorEnd()
	m.fitCompose()
}

// listDraft is the draft shown for a chat in the list: not for the open
// chat (it's in the input box).
func (m Model) listDraft(jid string) string {
	if m.screen == screenChat && m.current != nil && m.current.JID == jid {
		return ""
	}
	return m.drafts[jid]
}
