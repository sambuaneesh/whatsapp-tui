package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Notifier shows desktop notifications; notify.System implements it.
type Notifier interface {
	Popup(title, body string, silent bool) error
	Sound() error
}

// incomingMsg is a message just received in any chat.
type incomingMsg struct {
	msg      messages.Message
	chatName string
}

// notifyModes is the order M (or a click on the bell) cycles through.
var notifyModes = []string{config.NotifyAll, config.NotifyPopup, config.NotifySound, config.NotifyOff}

// notifyLabels are the status bar badges and what each mode means.
var notifyLabels = map[string][2]string{
	config.NotifyAll:   {"🔔 all", "popup and sound"},
	config.NotifyPopup: {"💬 popup", "silent popup"},
	config.NotifySound: {"🔊 sound", "sound only"},
	config.NotifyOff:   {"🔕 off", "off"},
}

// maxNotifyAge: older messages (delivered late, after being offline) don't
// notify.
const maxNotifyAge = 2 * time.Minute

// soundGap keeps a burst of messages from playing the sound over itself.
const soundGap = 2 * time.Second

// notifyLogf writes to the notification log, if any (the background app's
// log; see Options.NotifyLog).
func (m Model) notifyLogf(format string, args ...any) {
	if m.notifyLog != nil {
		fmt.Fprintf(m.notifyLog, time.Now().Format("15:04:05 ")+"notify: "+format+"\n", args...)
	}
}

// notifyBadge is the notification mode shown in the status bar.
func (m Model) notifyBadge() string {
	label := notifyLabels[m.notifyMode][0]
	if label == "" {
		return ""
	}
	style := lipgloss.NewStyle().Background(colorBarBg).Foreground(colorWarm)
	if m.notifyMode == config.NotifyOff {
		style = style.Foreground(pal.Muted)
	}
	return style.Render(" " + label + " ")
}

// setNotifyMode switches the mode, saves it and previews it.
func (m Model) setNotifyMode(mode string) (tea.Model, tea.Cmd) {
	if _, ok := notifyLabels[mode]; !ok {
		m.notice, m.noticeErr = "notifications: all, popup, sound or off", true
		return m, nil
	}
	m.notifyMode = mode
	m.notice, m.noticeErr = "Notifications: "+notifyLabels[mode][1]+"  (M or click "+notifyLabels[mode][0]+" to change)", false
	if err := config.SetNotificationMode(mode); err != nil {
		m.notice, m.noticeErr = "notifications: "+err.Error(), true
	}
	n := m.notifier
	if n == nil {
		return m, nil
	}
	// a sample, so you know what new messages will do
	return m, func() tea.Msg {
		var err error
		switch mode {
		case config.NotifyAll:
			if err = n.Popup("WhatsApp", "Notifications on: popup and sound", true); err == nil {
				err = n.Sound()
			}
		case config.NotifyPopup:
			err = n.Popup("WhatsApp", "Notifications on: silent popup", true)
		case config.NotifySound:
			err = n.Sound()
		}
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("notification: %w", err)}
		}
		return nil
	}
}

// cycleNotifyMode moves to the next mode.
func (m Model) cycleNotifyMode() (tea.Model, tea.Cmd) {
	next := notifyModes[0]
	for i, mode := range notifyModes {
		if mode == m.notifyMode {
			next = notifyModes[(i+1)%len(notifyModes)]
		}
	}
	return m.setNotifyMode(next)
}

// notifyIncoming notifies about a new message, unless notifications are
// off or you're looking at its chat.
func (m Model) notifyIncoming(in incomingMsg) (tea.Model, tea.Cmd) {
	msg := in.msg
	logf := m.notifyLogf
	why := ""
	switch {
	case m.notifier == nil:
		why = "notifications unavailable"
	case msg.FromMe:
		why = "your own message"
	case m.notifyMode == config.NotifyOff || m.notifyMode == "":
		why = "notifications off"
	case m.focused && m.screen == screenChat && m.current != nil && m.current.JID == msg.ChatId:
		why = "its chat is on screen"
	case time.Since(toTime(int64(msg.Timestamp))) > maxNotifyAge:
		why = fmt.Sprintf("sent %s ago", time.Since(toTime(int64(msg.Timestamp))).Round(time.Second))
	}
	if why != "" {
		logf("message %s in %s: no notification (%s)", msg.Id, msg.ChatId, why)
		return m, nil
	}
	popup := m.notifyMode == config.NotifyAll || m.notifyMode == config.NotifyPopup
	sound := m.notifyMode == config.NotifyAll || m.notifyMode == config.NotifySound
	if now := clickNow(); sound {
		if now.Sub(m.lastSound) < soundGap {
			sound = false
		} else {
			m.lastSound = now
		}
	}
	logf("message %s in %s: mode %s, focused %v: popup %v, sound %v", msg.Id, msg.ChatId, m.notifyMode, m.focused, popup, sound)
	if !popup && !sound {
		return m, nil
	}
	title, body := notificationText(msg, in.chatName)
	n := m.notifier
	return m, func() tea.Msg {
		var err error
		if popup {
			err = n.Popup(title, body, true) // the sound is ours, so it follows the mode
			logf("  popup: err=%v", err)
		}
		if sound {
			start := time.Now()
			serr := n.Sound()
			logf("  sound: err=%v (%s)", serr, time.Since(start).Round(time.Millisecond))
			if err == nil {
				err = serr
			}
		}
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("notification: %w", err)}
		}
		return nil
	}
}

// notificationText is a message's notification: the chat (or sender) as
// the title, and the text, with the sender in groups.
func notificationText(msg messages.Message, chatName string) (title, body string) {
	sender := msg.ContactShort
	if sender == "" {
		sender = msg.ContactName
	}
	text := prettyTags(strings.TrimSpace(msg.Text))
	if note, ok := strings.CutPrefix(msg.Text, "[REMINDER] "); ok {
		// from the scheduler: snoozed chat back, no reply yet
		return chatName, note
	}
	if emoji, ok := strings.CutPrefix(msg.Text, "[REACTION] "); ok {
		// a reaction to your message
		quoted := strings.ReplaceAll(prettyTags(strings.TrimSpace(msg.QuotedText)), "\n", " ")
		if r := []rune(quoted); len(r) > 80 {
			quoted = string(r[:80]) + "…"
		}
		text = "Reacted " + emoji + " to: " + quoted
	}
	for user, name := range msg.Mentions {
		text = strings.ReplaceAll(text, "@"+user, "@"+name)
	}
	if r := []rune(text); len(r) > 200 {
		text = string(r[:200]) + "…"
	}
	if isGroup(msg.ChatId) {
		title = chatName
		if sender != "" {
			text = sender + ": " + text
		}
	} else {
		title = chatName
		if title == "" {
			title = sender
		}
	}
	if title == "" {
		title = "WhatsApp"
	}
	return title, text
}

// notifyBadgeAt reports whether screen cell (x, y) is on the status bar's
// notification badge.
func (m Model) notifyBadgeAt(x, y int) bool {
	if y != m.mainHeight() {
		return false
	}
	w := lipgloss.Width(m.notifyBadge())
	start := m.width - lipgloss.Width(m.statusRight()) + lipgloss.Width(m.scheduledBadge())
	return w > 0 && x >= start && x < start+w
}

// scheduledBadgeAt reports whether screen cell (x, y) is on the status
// bar's ⏰ count.
func (m Model) scheduledBadgeAt(x, y int) bool {
	if y != m.mainHeight() {
		return false
	}
	w := lipgloss.Width(m.scheduledBadge())
	start := m.width - lipgloss.Width(m.statusRight())
	return w > 0 && x >= start && x < start+w
}
