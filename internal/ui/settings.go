package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Settings (ctrl+, through kitty, :settings, or F1 → Settings): every
// option in one place. ←/→ (or h/l, enter) change a value; it's saved to
// config.ini at once and applies straight away where it can (the rest say
// "after a restart").

type settingsView struct {
	sel    int
	scroll int
}

// setting is one row. A choice cycles through values; an edit row opens
// the : line to type a value; an action row does something.
type setting struct {
	group, label, help string
	values             []string // choices; nil for edit and action rows
	get                func(m Model) string
	set                func(m Model, v string) (Model, tea.Cmd, error)
	edit               string // the : command to prefill ("download-dir ")
	action             func(m Model) (tea.Model, tea.Cmd)
	restart            bool // only takes effect when the app starts again
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

var onOffChoices = []string{"on", "off"}

// saveUI and saveGeneral write one key to config.ini.
func saveUI(key, v string) error         { return config.Save("ui", key, v) }
func saveGeneralKey(key, v string) error { return config.Save("general", key, v) }

// restyle redraws everything after an appearance change.
func (m *Model) restyle() {
	v, focused := m.compose.Value(), m.compose.Focused()
	h := m.compose.Height()
	m.compose = newCompose()
	m.compose.SetValue(v)
	m.compose.SetHeight(h)
	if focused {
		m.compose.Focus()
	}
	if m.inPersonal() {
		m.compose.Placeholder = m.personalPlaceholder()
	}
	m.bubbles, m.rows = newBubbleCache(), newBubbleCache()
	m.resize()
	m.refreshMessages(false)
}

func settingsList() []setting {
	return []setting{
		// ---------- Appearance ----------
		{group: "Appearance", label: "Theme", help: "Rosé Pine, its darker Moon, or the light Dawn",
			values: []string{"rose-pine", "rose-pine-moon", "rose-pine-dawn"},
			get: func(m Model) string {
				t := strings.ToLower(config.Config.Ui.Theme)
				if t == "" || (t != "rose-pine-moon" && t != "rose-pine-dawn") {
					return "rose-pine"
				}
				return t
			},
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				if err := saveUI("theme", v); err != nil {
					return m, nil, err
				}
				config.Config.Ui.Theme = v
				ApplyTheme(v)
				m.bgSeq = backgroundSeq(config.Config.Ui.PaintBackground)
				m.restyle()
				return m, nil, nil
			}},
		{group: "Appearance", label: "Space between messages", help: "compact: none · normal: a line · roomy: two (and one more when the sender changes)",
			values: spacingChoices,
			get:    func(m Model) string { return nonEmpty(config.Config.Ui.MessageSpacing, "normal") },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				if err := saveUI("message_spacing", v); err != nil {
					return m, nil, err
				}
				config.Config.Ui.MessageSpacing = v
				SetLook(v, config.Config.Ui.BubbleStyle)
				m.restyle()
				return m, nil, nil
			}},
		{group: "Appearance", label: "Bubble borders", help: "╭ rounded · ┌ square · ┏ thick · ╔ double · or none",
			values: bubbleChoices,
			get:    func(m Model) string { return nonEmpty(config.Config.Ui.BubbleStyle, "rounded") },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				if err := saveUI("bubble_style", v); err != nil {
					return m, nil, err
				}
				config.Config.Ui.BubbleStyle = v
				SetLook(config.Config.Ui.MessageSpacing, v)
				m.restyle()
				return m, nil, nil
			}},
		{group: "Appearance", label: "Chat list width", help: "columns next to an open chat",
			values: []string{"28", "32", "34", "38", "42", "46", "50", "56"},
			get:    func(m Model) string { return strconv.Itoa(m.sidebarW) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				n, _ := strconv.Atoi(v)
				if err := saveUI("chat_sidebar_width", v); err != nil {
					return m, nil, err
				}
				config.Config.Ui.ChatSidebarWidth = n
				m.sidebarW = n
				m.restyle()
				return m, nil, nil
			}},
		{group: "Appearance", label: "Paint the theme background", help: "off keeps your terminal's own background (and its transparency)",
			values: onOffChoices,
			get:    func(m Model) string { return onOff(m.bgSeq != "") },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				if err := saveUI("paint_background", fmt.Sprint(v == "on")); err != nil {
					return m, nil, err
				}
				config.Config.Ui.PaintBackground = v == "on"
				m.bgSeq = backgroundSeq(v == "on")
				return m, nil, nil
			}},
		{group: "Appearance", label: "Mouse", help: "click, double-click, scroll; off lets the terminal select text without shift",
			values: onOffChoices,
			get:    func(m Model) string { return onOff(m.mouse) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				if err := saveUI("mouse", fmt.Sprint(v == "on")); err != nil {
					return m, nil, err
				}
				config.Config.Ui.Mouse = v == "on"
				m.mouse = v == "on"
				if m.mouse {
					return m, tea.EnableMouseCellMotion, nil
				}
				return m, tea.DisableMouse, nil
			}},
		{group: "Appearance", label: "Pictures", help: "auto picks kitty's sharp images when it can",
			values: []string{"auto", "kitty", "blocks", "off"}, restart: true,
			get: func(m Model) string { return nonEmpty(config.Config.Ui.Images, "auto") },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				config.Config.Ui.Images = v
				return m, nil, saveUI("images", v)
			}},
		{group: "Appearance", label: "Profile pictures", help: "in the chat list (kitty)", values: onOffChoices, restart: true,
			get: func(m Model) string { return onOff(config.Config.Ui.Avatars) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				config.Config.Ui.Avatars = v == "on"
				return m, nil, saveUI("avatars", fmt.Sprint(v == "on"))
			}},
		{group: "Appearance", label: "See-through highlights", help: "how solid bubbles and bars are over a see-through kitty (1 = solid)",
			values: []string{"0.6", "0.7", "0.8", "0.9", "1"}, restart: true,
			get: func(m Model) string {
				return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", config.Config.Ui.HighlightOpacity), "0"), ".")
			},
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				f, _ := strconv.ParseFloat(v, 64)
				config.Config.Ui.HighlightOpacity = f
				return m, nil, saveUI("highlight_opacity", v)
			}},

		// ---------- Notifications and privacy ----------
		{group: "Notifications & privacy", label: "Notifications", help: "all: popup and sound · popup · sound · off (M anywhere)",
			values: notifyModes,
			get:    func(m Model) string { return nonEmpty(m.notifyMode, "off") },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				next, cmd := m.setNotifyMode(v)
				return next.(Model), cmd, nil
			}},
		{group: "Notifications & privacy", label: "Private reading", help: "open chats without blue ticks; U or :read marks one read",
			values: onOffChoices,
			get:    func(m Model) string { return onOff(m.privateRead) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				next, cmd := m.setPrivateReading(v == "on")
				return next.(Model), cmd, nil
			}},

		// ---------- The app ----------
		{group: "The app", label: "Keep running when closed", help: "notifications, scheduled messages and reminders keep working",
			values: onOffChoices, restart: true,
			get: func(m Model) string { return onOff(config.Config.General.Background) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				config.Config.General.Background = v == "on"
				return m, nil, saveGeneralKey("background", fmt.Sprint(v == "on"))
			}},
		{group: "The app", label: "Downloads folder", help: "where s saves media",
			get:  func(m Model) string { return tildePath(downloadDir()) },
			edit: "download-dir "},
		{group: "The app", label: "Media kept on disk", help: "downloaded pictures and videos; the least recently viewed go first",
			values: []string{"256", "512", "1024", "2048", "4096", "8192"},
			get:    func(m Model) string { return strconv.Itoa(config.Config.General.MediaCacheMb) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				n, _ := strconv.Atoi(v)
				config.Config.General.MediaCacheMb = n
				messages.MediaCacheLimit = int64(n) << 20
				return m, nil, saveGeneralKey("media_cache_mb", v)
			}},
		{group: "The app", label: "Scripts may send messages", help: "through the local API (docs/API.md); rate-limited",
			values: onOffChoices, restart: true,
			get: func(m Model) string { return onOff(config.Config.General.ApiAllowSend) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				config.Config.General.ApiAllowSend = v == "on"
				return m, nil, saveGeneralKey("api_allow_send", fmt.Sprint(v == "on"))
			}},

		// ---------- Search and AI ----------
		{group: "Search & AI", label: "Search by meaning", help: "EmbeddingGemma through Ollama, on your machine",
			values: onOffChoices, restart: true,
			get: func(m Model) string { return onOff(config.Config.General.SemanticSearch) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				config.Config.General.SemanticSearch = v == "on"
				return m, nil, saveGeneralKey("semantic_search", fmt.Sprint(v == "on"))
			}},
		{group: "Search & AI", label: "Pause indexing while gaming", help: "waits while the GPU is over 50% busy",
			values: onOffChoices, restart: true,
			get: func(m Model) string { return onOff(config.Config.General.SemanticPauseGaming) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				config.Config.General.SemanticPauseGaming = v == "on"
				return m, nil, saveGeneralKey("semantic_pause_gaming", fmt.Sprint(v == "on"))
			}},
		{group: "Search & AI", label: "Chats left out of search", help: "enter lists them; :noindex in a chat leaves it out",
			get: func(m Model) string {
				if len(m.noindex) == 0 {
					return "none"
				}
				return strconv.Itoa(len(m.noindex))
			},
			action: func(m Model) (tea.Model, tea.Cmd) { m.settings = nil; return m.showNotIndexed() }},
		{group: "Search & AI", label: "AI help (local model)", help: "T worded, :todos, :catchup, :plan",
			values: onOffChoices,
			get:    func(m Model) string { return onOff(m.ai != nil) },
			set: func(m Model, v string) (Model, tea.Cmd, error) {
				if err := saveGeneralKey("ai", fmt.Sprint(v == "on")); err != nil {
					return m, nil, err
				}
				config.Config.General.Ai = v == "on"
				switch {
				case v == "off" && m.ai != nil:
					m.aiStash, m.ai = m.ai, nil
				case v == "on" && m.ai == nil && m.aiStash != nil:
					m.ai, m.aiStash = m.aiStash, nil
				case v == "on" && m.ai == nil:
					m.notice, m.noticeErr = "AI help comes on when the app starts again", false
				}
				return m, nil, nil
			}},
		{group: "Search & AI", label: "AI model", help: "an Ollama chat model (ollama pull it first)", restart: true,
			get:  func(m Model) string { return nonEmpty(config.Config.General.AiModel, "qwen3:4b") },
			edit: "set ai_model "},

		// ---------- Your lists ----------
		{group: "Your lists", label: "Mirror to a folder", help: "Markdown, both ways (an Obsidian vault folder)",
			get: func(m Model) string {
				if m.mirror == nil || m.mirror.Dir() == "" {
					return "off"
				}
				return tildePath(m.mirror.Dir())
			},
			edit: "mirror "},
	}
}

func nonEmpty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func (m *Model) openSettings() {
	m.closePalette()
	m.settings = &settingsView{}
}

// change moves the selected setting's value by dir (a choice), or opens
// its edit line or action.
func (m Model) changeSetting(dir int) (tea.Model, tea.Cmd) {
	list := settingsList()
	s := list[m.settings.sel]
	switch {
	case s.action != nil:
		return s.action(m)
	case s.edit != "":
		m.settings = nil
		return prefill(s.edit)(m)
	case len(s.values) == 0:
		return m, nil
	}
	cur := s.get(m)
	i := 0
	for j, v := range s.values {
		if v == cur {
			i = j
		}
	}
	v := s.values[(i+dir+len(s.values))%len(s.values)]
	next, cmd, err := s.set(m, v)
	if err != nil {
		next.notice, next.noticeErr = err.Error(), true
		return next, cmd
	}
	if next.notice == "" || !strings.Contains(next.notice, "Notifications") {
		next.notice, next.noticeErr = s.label+": "+v, false
		if s.restart {
			next.notice += " · applies when the app starts again (:q! then open it)"
		}
	}
	return next, cmd
}

func (m Model) handleSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sv := m.settings
	n := len(settingsList())
	switch msg.String() {
	case "esc", "q", "ctrl+c", "f15":
		m.settings = nil
		return m, nil
	case "j", "down", "tab", "ctrl+n":
		sv.sel = min(sv.sel+1, n-1)
	case "k", "up", "shift+tab", "ctrl+p":
		sv.sel = max(sv.sel-1, 0)
	case "g", "home":
		sv.sel = 0
	case "G", "end":
		sv.sel = n - 1
	case "l", "right", "enter", " ", "space":
		return m.changeSetting(1)
	case "h", "left":
		return m.changeSetting(-1)
	}
	return m, nil
}

// settingsLines draws the settings, and which line each row is on.
func (m Model) settingsLines(width int) ([]string, []int) {
	list := settingsList()
	labelW, valW := 32, 24
	title := lipgloss.NewStyle().Foreground(pal.Iris).Bold(true)
	var lines []string
	rowLine := make([]int, len(list))
	lines = append(lines, "", "  "+lipgloss.NewStyle().Foreground(pal.Rose).Bold(true).Render("Settings")+
		styleMuted.Render("   j/k move · ←/→ or enter change · esc close · saved to "+tildePath(config.GetConfigFilePath())))
	group := ""
	for i, s := range list {
		if s.group != group {
			group = s.group
			lines = append(lines, "", "  "+title.Render(group))
		}
		sel := i == m.settings.sel
		val := s.get(m)
		valSt := lipgloss.NewStyle().Foreground(pal.Gold)
		switch {
		case s.action != nil:
			val += "  ›"
		case s.edit != "":
			val += "  ✎"
		case len(s.values) > 0 && sel:
			val = "‹ " + val + " ›"
		}
		if val == "off" || val == "none" {
			valSt = styleMuted
		}
		label := s.label
		if s.restart {
			label += " ↻"
		}
		var line string
		if sel {
			line = "  " + paint(styleAccent, true).Render("▌ ") + paint(lipgloss.NewStyle().Foreground(pal.Text), true).Render(padRight(label, labelW)) +
				paint(valSt, true).Render(padRight(val, valW)) + paint(styleMuted, true).Render(" "+s.help)
			line = padSelected(line, width)
		} else {
			line = "    " + lipgloss.NewStyle().Foreground(pal.Text).Render(padRight(label, labelW)) + valSt.Render(padRight(val, valW))
			if help := styleMuted.Render(" " + s.help); ansi.StringWidth(line)+ansi.StringWidth(help) <= width {
				line += help
			}
		}
		rowLine[i] = len(lines)
		lines = append(lines, ansi.Truncate(line, width, "…"))
	}
	lines = append(lines, "", "  "+styleMuted.Render("↻ applies when the app starts again (:q! quits it for real; open it again). Everything else applies now."))
	return lines, rowLine
}

func padSelected(line string, width int) string {
	w := ansi.StringWidth(line)
	if w >= width {
		return ansi.Truncate(line, width, "…")
	}
	return line + paint(lipgloss.NewStyle(), true).Render(strings.Repeat(" ", width-w))
}

func (m Model) renderSettings(width, height int) string {
	lines, rowLine := m.settingsLines(width)
	sv := m.settings
	// keep the selection on screen
	at := rowLine[sv.sel]
	if at < sv.scroll+2 {
		sv.scroll = max(at-3, 0)
	}
	if at >= sv.scroll+height-1 {
		sv.scroll = at - height + 2
	}
	sv.scroll = min(max(sv.scroll, 0), max(len(lines)-height, 0))
	return strings.Join(fitLines(strings.Join(lines[sv.scroll:], "\n"), height), "\n")
}

// mouseSettings: a click selects a row (again: changes it), the wheel
// moves.
func (m Model) mouseSettings(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	sv := m.settings
	switch {
	case msg.Button == tea.MouseButtonWheelDown:
		sv.sel = min(sv.sel+1, len(settingsList())-1)
	case msg.Button == tea.MouseButtonWheelUp:
		sv.sel = max(sv.sel-1, 0)
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		_, rowLine := m.settingsLines(m.width)
		for i, l := range rowLine {
			if l-sv.scroll == msg.Y {
				if sv.sel == i {
					return m.changeSetting(1)
				}
				sv.sel = i
			}
		}
	}
	return m, nil
}
