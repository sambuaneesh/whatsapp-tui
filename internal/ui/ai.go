package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// aiDate asks the local model what a date the parser can't read means.
func (m Model) aiDate(words string, itemID int64) tea.Cmd { return nil }

// aiTaskFromMessage asks the local model to word a message as a task.
func (m Model) aiTaskFromMessage(sel messages.Message) tea.Cmd { return nil }
