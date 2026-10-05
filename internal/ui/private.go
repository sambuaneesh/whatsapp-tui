package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Srindot/whatsapp-tui/internal/config"
)

// Private reading: open chats without sending read receipts (no blue
// ticks); they stay unread until you mark them read (U, :read).

// setPrivateReading turns private reading on or off and saves it.
func (m Model) setPrivateReading(on bool) (tea.Model, tea.Cmd) {
	m.privateRead = on
	m.notice, m.noticeErr = "Private reading off: opening a chat marks it read", false
	if on {
		m.notice = "Private reading on: no read receipts until you mark a chat read (U or :read)"
	}
	if err := config.SetPrivateReading(on); err != nil {
		m.notice, m.noticeErr = "private reading: "+err.Error(), true
	}
	if !on {
		return m, m.markSeen() // the open chat is being read now
	}
	return m, nil
}

// privateBadge shows in the status bar while private reading is on.
func (m Model) privateBadge() string {
	if !m.privateRead {
		return ""
	}
	return lipgloss.NewStyle().Background(colorBarBg).Foreground(pal.Iris).Render(" 🙈 private ")
}
