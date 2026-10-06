package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Search indexing per chat: every chat is indexed for search (words and
// meaning) unless you turn it off (:noindex, or F1). An unindexed chat is
// left out of searching all chats, and its messages aren't indexed at
// all; "/" inside it still finds things by plain matching.

// Indexer turns search indexing on and off per chat;
// *messages.SessionManager implements it.
type Indexer interface {
	SetChatIndexed(jid string, on bool) error
	NotIndexedChats() []string
}

func (m Model) indexer() Indexer {
	i, _ := m.actions.(Indexer)
	return i
}

// chatIndexed reports whether search indexes the chat.
func (m Model) chatIndexed(jid string) bool { return !m.noindex[jid] }

// loadNoIndex reads which chats aren't indexed (once, at start).
func (m *Model) loadNoIndex() {
	m.noindex = map[string]bool{}
	if ix := m.indexer(); ix != nil {
		for _, j := range ix.NotIndexedChats() {
			m.noindex[j] = true
		}
	}
}

// setIndexed turns indexing on or off for a chat.
func (m Model) setIndexed(c *messages.Conversation, on bool) (tea.Model, tea.Cmd) {
	ix := m.indexer()
	if c == nil || ix == nil || isPersonal(c.JID) {
		m.notice, m.noticeErr = "indexing is set per chat (open one, or select it in the list)", true
		return m, nil
	}
	if err := ix.SetChatIndexed(c.JID, on); err != nil {
		m.notice, m.noticeErr = "indexing: "+err.Error(), true
		return m, nil
	}
	if m.noindex == nil {
		m.noindex = map[string]bool{}
	}
	if on {
		delete(m.noindex, c.JID)
		m.notice, m.noticeErr = chatName(c)+" is indexed for search again", false
	} else {
		m.noindex[c.JID] = true
		m.notice, m.noticeErr = chatName(c)+" isn't indexed any more: left out of searching all chats (/ in it still works) · :index undoes", false
	}
	return m, nil
}

// showNotIndexed lists the chats indexing is off for; enter turns it back
// on for one.
func (m Model) showNotIndexed() (tea.Model, tea.Cmd) {
	if len(m.noindex) == 0 {
		m.notice, m.noticeErr = "every chat is indexed for search (:noindex in a chat turns it off for that one)", false
		return m, nil
	}
	names := map[string]string{}
	for _, c := range append(append([]*messages.Conversation(nil), m.chats...), m.allChats...) {
		names[c.JID] = chatName(c)
	}
	var items []palItem
	for jid := range m.noindex {
		jid := jid
		name := names[jid]
		if name == "" {
			name = jid
		}
		items = append(items, palItem{action: &palAction{label: "🔍 " + name, right: "enter: index it again", keep: true,
			run: func(m Model) (Model, string) {
				ix := m.indexer()
				if ix == nil {
					return m, "indexing isn't available"
				}
				if err := ix.SetChatIndexed(jid, true); err != nil {
					return m, err.Error()
				}
				delete(m.noindex, jid)
				return m, name + " is indexed for search again"
			}}})
	}
	m.openActions(fmt.Sprintf("Chats not indexed for search (%d)", len(items)), items)
	return m, nil
}
