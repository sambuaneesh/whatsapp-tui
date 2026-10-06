package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// The command palette's commands (F1, ctrl+shift+p, or > in ctrl+p): every
// feature, by name, like VS Code's. A command shows only where it works.
// Keep it in sync with the handlers and helpSections.

type command struct {
	id    string
	title string // "Group: What it does"
	keys  string // its shortcut, shown on the right
	when  func(m Model) bool
	run   func(m Model) (tea.Model, tea.Cmd)
}

func inChat(m Model) bool    { return m.screen == screenChat && m.current != nil }
func hasChat(m Model) bool   { return inChat(m) || m.selectedChat() != nil }
func hasSplit(m Model) bool  { return m.split != nil }
func hasSearch(m Model) bool { return inChat(m) && m.search != nil && len(m.search.matches) > 0 }
func always(Model) bool      { return true }

// In visual mode: the selected message, and whether several are selected.
func inVisual(m Model) bool { return inChat(m) && m.mode == modeVisual }
func oneSelected(m Model) bool {
	_, ok := m.selected()
	return inVisual(m) && ok && m.rangeFrom == noRange
}
func severalSelected(m Model) bool { return inVisual(m) && m.rangeFrom != noRange }

// selWhen narrows oneSelected to messages where f holds.
func selWhen(f func(m Model, s messages.Message) bool) func(Model) bool {
	return func(m Model) bool {
		s, ok := m.selected()
		return oneSelected(m) && ok && f(m, s)
	}
}

func visualKey(k string) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
		}
		return m.handleVisual(msg)
	}
}

func pinFor(d time.Duration) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) { return m.pinSelected(d) }
}

func muteFor(d time.Duration) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) { return m.muteChat(m.theChat(), d) }
}

func hasMedia(_ Model, s messages.Message) bool { return len(s.Media) > 0 }
func isDeleted(s messages.Message) bool {
	return s.Deleted != 0 || strings.HasPrefix(s.Text, "[REVOKED]")
}
func inGroup(m Model) bool { return inChat(m) && isGroup(m.current.JID) }
func chatIs(f func(c *messages.Conversation) bool) func(Model) bool {
	return func(m Model) bool {
		c := m.theChat()
		return c != nil && c.LastMsgTime > 0 && f(c)
	}
}

// chatPaneKey runs a key in the open chat's messages pane.
func chatPaneKey(k string) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) {
		m.focus = paneMessages
		return m.handleMessagesPane(k)
	}
}

// listPaneKey runs a key in the chat list.
func listPaneKey(k string) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) { return m.handleListPane(k) }
}

// triage runs e, U or J on the open chat, else the selected one.
func triage(k string) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) { return m.triageKey(k, inChat(m)) }
}

// ex runs an ex command (":later 9am").
func ex(line string) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) { return m.runCommand(line) }
}

// prefill starts typing an ex command that needs more (":later "), so you
// finish it and press enter.
func prefill(line string) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) {
		m.mode = modeCommand
		m.cmdline.Prompt = ":"
		m.cmdline.SetValue(line)
		m.cmdline.CursorEnd()
		return m, m.cmdline.Focus()
	}
}

func notifyCmd(mode, label string) command {
	return command{
		id: "notify." + mode, title: "Notifications: " + label, keys: ":notify " + mode,
		when: func(m Model) bool { return m.notifyMode != mode },
		run:  func(m Model) (tea.Model, tea.Cmd) { return m.setNotifyMode(mode) },
	}
}

// commands is filled in init: some commands open the palette, which
// lists the commands.
var commands []command

func init() { commands = commandList() }

func commandList() []command {
	return []command{
		// Go
		{id: "goto", title: "Go to Chat…", keys: "ctrl+p", when: always,
			run: func(m Model) (tea.Model, tea.Cmd) { m.openPalette(""); return m, nil }},
		{id: "beside", title: "Split View: Open a Chat Beside…", keys: "alt+enter", when: inChat,
			run: func(m Model) (tea.Model, tea.Cmd) { m.openPalette(""); m.qo.beside = true; return m, nil }},
		{id: "nextunread", title: "Go: Next Unread Chat", keys: "J", when: always, run: triage("J")},
		{id: "back", title: "Go: Back to the Chat List", keys: "q", when: inChat,
			run: func(m Model) (tea.Model, tea.Cmd) { return m, m.back() }},
		{id: "activity", title: "Go: Activity (Mentions, Replies, Reactions to You)", keys: "I", when: always,
			run: func(m Model) (tea.Model, tea.Cmd) { return m, m.openActivity() }},
		{id: "mentions", title: "Go: Next Message That Mentions You", keys: "@", when: inChat, run: chatPaneKey("@")},
		{id: "top", title: "Go: Oldest Loaded Message", keys: "gg", when: inChat,
			run: func(m Model) (tea.Model, tea.Cmd) { m.focus = paneMessages; m.vp.GotoTop(); return m, nil }},
		{id: "bottom", title: "Go: Newest Message", keys: "G", when: inChat, run: chatPaneKey("G")},

		// Search
		{id: "find", title: "Search: Find in Chat", keys: "ctrl+f  /", when: inChat,
			run: func(m Model) (tea.Model, tea.Cmd) { m.focus = paneMessages; return m, m.startSearch() }},
		{id: "findnext", title: "Search: Next Match (Older)", keys: "F3  n", when: hasSearch,
			run: func(m Model) (tea.Model, tea.Cmd) { m.nextMatch(-1); return m, nil }},
		{id: "findprev", title: "Search: Previous Match (Newer)", keys: "shift+F3  N", when: hasSearch,
			run: func(m Model) (tea.Model, tea.Cmd) { m.nextMatch(1); return m, nil }},
		{id: "searchall", title: "Search: Messages in All Chats", keys: "ctrl+shift+f  S", when: always,
			run: func(m Model) (tea.Model, tea.Cmd) { return m, m.openGlobalSearch() }},
		{id: "filter", title: "Search: Filter the Chat List", keys: "/", when: always,
			run: func(m Model) (tea.Model, tea.Cmd) {
				m.focus = paneList
				return m.handleListPane("/")
			}},

		// Chat list
		{id: "unread", title: "Chat List: Only Unread Chats (Toggle)", keys: "u", when: always, run: ex("unread")},
		{id: "archive", title: "Chat List: Show Archived Chats", keys: "A", when: func(m Model) bool { return !m.archive }, run: ex("archive")},
		{id: "inbox", title: "Chat List: Show Inbox", keys: "A", when: func(m Model) bool { return m.archive }, run: ex("inbox")},

		// The chat
		{id: "done", title: "Chat: Done (Mark Read and Archive)", keys: "e", when: hasChat, run: triage("e")},
		{id: "unreadmark", title: "Chat: Mark Unread / Read", keys: "U", when: hasChat, run: triage("U")},
		{id: "read", title: "Chat: Mark Read", keys: ":read", when: inChat, run: ex("read")},
		{id: "info", title: "Chat: Info (Picture, Description, Members)", keys: "K", when: hasChat, run: ex("info")},
		{id: "picture", title: "Chat: Profile Picture, Full Screen", keys: "V", when: hasChat,
			run: func(m Model) (tea.Model, tea.Cmd) {
				if inChat(m) {
					return m, m.openPicture(m.current)
				}
				return m, m.openPicture(m.selectedChat())
			}},
		{id: "snooze", title: "Chat: Snooze Until…", keys: ":snooze", when: inChat, run: prefill("snooze ")},
		{id: "nudge", title: "Chat: Remind Me If No Reply By…", keys: ":nudge", when: inChat, run: prefill("nudge ")},
		{id: "backlog", title: "Chat: Fetch Older Messages", keys: ":backlog", when: inChat, run: ex("backlog")},
		{id: "delete", title: "Chat: Delete…", keys: "d", when: hasChat,
			run: func(m Model) (tea.Model, tea.Cmd) {
				if inChat(m) {
					m.askDeleteChat(m.current)
				} else {
					m.askDeleteChat(m.selectedChat())
				}
				return m, nil
			}},

		// Split view
		{id: "closesplit", title: "Split View: Close the Right Pane", keys: "X", when: hasSplit, run: ex("close")},
		{id: "swapsplit", title: "Split View: Swap the Two Chats", keys: "W", when: hasSplit, run: chatPaneKey("W")},

		// Messages
		{id: "write", title: "Message: Write", keys: "i", when: inChat, run: chatPaneKey("i")},
		{id: "select", title: "Message: Select (Reply, React, Forward, Copy…)", keys: "v", when: func(m Model) bool { return inChat(m) && !inVisual(m) }, run: chatPaneKey("v")},
		{id: "attach", title: "Message: Attach Files…", keys: "a", when: inChat, run: chatPaneKey("a")},
		{id: "paste", title: "Message: Paste Screenshot or Text", keys: "p  ctrl+v", when: inChat, run: chatPaneKey("p")},
		{id: "stickers", title: "Message: Stickers & GIFs", keys: "s", when: inChat, run: chatPaneKey("s")},
		{id: "mksticker", title: "Message: Make a Sticker from a File…", keys: ":sticker", when: inChat, run: prefill("sticker ")},
		{id: "mkgif", title: "Message: Make a GIF from a File…", keys: ":gif", when: inChat, run: prefill("gif ")},
		{id: "later", title: "Message: Send Later…", keys: ":later", when: inChat, run: prefill("later ")},
		{id: "scheduled", title: "Go: Scheduled (Send Later, Snoozes, Reminders)", keys: ":scheduled", when: always, run: ex("scheduled")},

		// The selected message (visual mode, v)
		{id: "sel.reply", title: "Selected: Reply", keys: "enter", when: selWhen(func(_ Model, s messages.Message) bool { return !isDeleted(s) }), run: visualKey("enter")},
		{id: "sel.private", title: "Selected: Reply Privately", keys: "p",
			when: selWhen(func(m Model, s messages.Message) bool { return isGroup(m.current.JID) && !s.FromMe }), run: visualKey("p")},
		{id: "sel.react", title: "Selected: React…", keys: "r", when: selWhen(func(_ Model, s messages.Message) bool { return !isDeleted(s) }), run: visualKey("r")},
		{id: "sel.who", title: "Selected: Who Reacted", keys: "w", when: selWhen(func(_ Model, s messages.Message) bool { return len(s.Reactions) > 0 }), run: visualKey("w")},
		{id: "sel.edit", title: "Selected: Edit", keys: "e", when: selWhen(func(_ Model, s messages.Message) bool { ok, _ := messages.CanEdit(s); return ok }), run: visualKey("e")},
		{id: "sel.forward", title: "Selected: Forward…", keys: "f", when: selWhen(func(_ Model, s messages.Message) bool { return !isDeleted(s) }), run: visualKey("f")},
		{id: "sel.copy", title: "Selected: Copy Text or Picture", keys: "y", when: oneSelected, run: visualKey("y")},
		{id: "sel.save", title: "Selected: Save Media to Downloads", keys: "s", when: selWhen(hasMedia), run: visualKey("s")},
		{id: "sel.view", title: "Selected: View Full Screen / Play", keys: "space", when: selWhen(hasMedia), run: visualKey("space")},
		{id: "sel.open", title: "Selected: Open in Its App, or Open the Link", keys: "o",
			when: selWhen(func(_ Model, s messages.Message) bool { return len(s.Media) > 0 || linkRe.MatchString(s.Text) }), run: visualKey("o")},
		{id: "sel.pin", title: "Selected: Pin for 7 Days", keys: "P", when: selWhen(func(_ Model, s messages.Message) bool { return !s.Pinned && !isDeleted(s) }), run: pinFor(7 * 24 * time.Hour)},
		{id: "sel.pin1", title: "Selected: Pin for 24 Hours", keys: "", when: selWhen(func(_ Model, s messages.Message) bool { return !s.Pinned && !isDeleted(s) }), run: pinFor(24 * time.Hour)},
		{id: "sel.pin30", title: "Selected: Pin for 30 Days", keys: "", when: selWhen(func(_ Model, s messages.Message) bool { return !s.Pinned && !isDeleted(s) }), run: pinFor(30 * 24 * time.Hour)},
		{id: "sel.unpin", title: "Selected: Unpin", keys: "P", when: selWhen(func(_ Model, s messages.Message) bool { return s.Pinned }), run: pinFor(-1)},
		{id: "sel.delete", title: "Selected: Delete…", keys: "d", when: oneSelected, run: visualKey("d")},
		{id: "sel.retry", title: "Selected: Retry Sending", keys: "R",
			when: selWhen(func(_ Model, s messages.Message) bool { return s.FromMe && s.Status == messages.StatusFailed }), run: visualKey("R")},
		{id: "sel.several", title: "Selected: Select Several (j/k Extend)", keys: "V", when: oneSelected, run: visualKey("V")},
		{id: "sel.done", title: "Selected: Stop Selecting", keys: "esc", when: oneSelected, run: visualKey("v")},

		// Several selected (V)
		{id: "range.copy", title: "Selected Messages: Copy as a Transcript", keys: "y", when: severalSelected, run: visualKey("y")},
		{id: "range.forward", title: "Selected Messages: Forward All…", keys: "f", when: severalSelected, run: visualKey("f")},
		{id: "range.delete", title: "Selected Messages: Delete All…", keys: "d", when: severalSelected, run: visualKey("d")},
		{id: "range.save", title: "Selected Messages: Save All Media", keys: "s", when: severalSelected, run: visualKey("s")},
		{id: "range.end", title: "Selected Messages: Stop Selecting Several", keys: "V", when: severalSelected, run: visualKey("V")},

		// Pins and mutes
		{id: "pinned", title: "Go: Pinned Message", keys: "click 📌", when: func(m Model) bool { return inChat(m) && len(m.pins) > 0 },
			run: func(m Model) (tea.Model, tea.Cmd) { return m.jumpToPin() }},
		{id: "chat.pin", title: "Chat: Pin to the Top", keys: "P  :pin", when: chatIs(func(c *messages.Conversation) bool { return !c.IsPinned }),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.pinChat(m.theChat()) }},
		{id: "chat.unpin", title: "Chat: Unpin from the Top", keys: "P  :unpin", when: chatIs(func(c *messages.Conversation) bool { return c.IsPinned }),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.pinChat(m.theChat()) }},
		{id: "chat.mute8", title: "Chat: Mute for 8 Hours", keys: ":mute 8h", when: chatIs(func(c *messages.Conversation) bool { return !c.Muted(time.Now().Unix()) }), run: muteFor(8 * time.Hour)},
		{id: "chat.mutew", title: "Chat: Mute for a Week", keys: ":mute 1w", when: chatIs(func(c *messages.Conversation) bool { return !c.Muted(time.Now().Unix()) }), run: muteFor(7 * 24 * time.Hour)},
		{id: "chat.mute", title: "Chat: Mute Always", keys: ":mute", when: chatIs(func(c *messages.Conversation) bool { return !c.Muted(time.Now().Unix()) }), run: muteFor(-1)},
		{id: "chat.unmute", title: "Chat: Unmute", keys: ":unmute", when: chatIs(func(c *messages.Conversation) bool { return c.Muted(time.Now().Unix()) }), run: muteFor(0)},

		// Groups
		{id: "group.rename", title: "Group: Rename…", keys: ":subject", when: inGroup, run: prefill("subject ")},
		{id: "group.add", title: "Group: Add a Member (Phone Number)…", keys: ":add", when: inGroup, run: prefill("add ")},
		{id: "group.remove", title: "Group: Remove a Member (Phone Number)…", keys: ":remove", when: inGroup, run: prefill("remove ")},
		{id: "group.admin", title: "Group: Make Admin (Phone Number)…", keys: ":admin", when: inGroup, run: prefill("admin ")},
		{id: "group.unadmin", title: "Group: Remove Admin (Phone Number)…", keys: ":removeadmin", when: inGroup, run: prefill("removeadmin ")},
		{id: "group.leave", title: "Group: Leave…", keys: ":leave", when: inGroup, run: prefill("leave")},
		{id: "group.create", title: "Group: New Group (Numbers, Then Name)…", keys: ":create", when: always, run: prefill("create ")},

		// Settings
		{id: "notify.cycle", title: "Notifications: Next Mode", keys: "M", when: always,
			run: func(m Model) (tea.Model, tea.Cmd) { return m.cycleNotifyMode() }},
		notifyCmd(config.NotifyAll, "Popup and Sound"),
		notifyCmd(config.NotifyPopup, "Silent Popup"),
		notifyCmd(config.NotifySound, "Sound Only"),
		notifyCmd(config.NotifyOff, "Off"),
		{id: "private.on", title: "Private Reading: On (No Blue Ticks)", keys: ":private on",
			when: func(m Model) bool { return !m.privateRead }, run: ex("private on")},
		{id: "private.off", title: "Private Reading: Off", keys: ":private off",
			when: func(m Model) bool { return m.privateRead }, run: ex("private off")},
		{id: "downloads", title: "Settings: Show Download Folder", keys: ":download-dir", when: always, run: ex("download-dir")},
		{id: "downloads.set", title: "Settings: Change Download Folder…", keys: ":download-dir", when: always, run: prefill("download-dir ")},

		// App
		{id: "help", title: "Help: Keyboard Shortcuts", keys: "?", when: always, run: ex("help")},
		{id: "close", title: "App: Close the Window (Keeps Running)", keys: ":q", when: always, run: ex("q")},
		{id: "quit", title: "App: Quit for Real", keys: ":q!", when: always, run: ex("q!")},
		{id: "logout", title: "App: Log Out (Unlink This Device)…", keys: ":logout", when: always, run: prefill("logout")},
	}
}

// availableCommands are the commands that work right now.
func (m Model) availableCommands() []*command {
	var out []*command
	for i := range commands {
		c := &commands[i]
		if c.when(m) {
			out = append(out, c)
		}
	}
	return out
}

func commandByID(id string) *command {
	for i := range commands {
		if commands[i].id == id {
			return &commands[i]
		}
	}
	return nil
}

// commandGroup is the part of a title before ": ".
func commandGroup(title string) string {
	g, _, _ := strings.Cut(title, ": ")
	return g
}
