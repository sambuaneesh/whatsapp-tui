package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/config"
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
		{id: "select", title: "Message: Select (Reply, React, Forward, Copy…)", keys: "v", when: inChat, run: chatPaneKey("v")},
		{id: "attach", title: "Message: Attach Files…", keys: "a", when: inChat, run: chatPaneKey("a")},
		{id: "paste", title: "Message: Paste Screenshot or Text", keys: "p  ctrl+v", when: inChat, run: chatPaneKey("p")},
		{id: "stickers", title: "Message: Stickers & GIFs", keys: "s", when: inChat, run: chatPaneKey("s")},
		{id: "mksticker", title: "Message: Make a Sticker from a File…", keys: ":sticker", when: inChat, run: prefill("sticker ")},
		{id: "mkgif", title: "Message: Make a GIF from a File…", keys: ":gif", when: inChat, run: prefill("gif ")},
		{id: "later", title: "Message: Send Later…", keys: ":later", when: inChat, run: prefill("later ")},
		{id: "scheduled", title: "Go: Scheduled (Send Later, Snoozes, Reminders)", keys: ":scheduled", when: always, run: ex("scheduled")},

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
