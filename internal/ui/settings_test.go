package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

func selectSetting(t *testing.T, m Model, label string) Model {
	t.Helper()
	for i, s := range settingsList() {
		if s.label == label {
			m.settings.sel = i
			return m
		}
	}
	t.Fatalf("no setting %q", label)
	return m
}

func TestSettings(t *testing.T) {
	saved := *config.Config.Ui
	defer func() {
		*config.Config.Ui = saved
		SetLook("normal", "rounded")
		ApplyTheme("rose-pine")
	}()
	m := visualModel(t, &fakeActions{}, fakeClip{})
	// F15 is ctrl+, (mapped in kitty.conf)
	m, _ = press(t, m, tea.KeyF15)
	if m.settings == nil {
		t.Fatal("F15 should open settings")
	}
	v := stripANSI(m.View())
	for _, want := range []string{"Settings", "Appearance", "Theme", "Space between messages", "Bubble borders", "Notifications", "AI help"} {
		if !strings.Contains(v, want) {
			t.Fatalf("%q missing:\n%s", want, v)
		}
	}
	if lines := strings.Count(m.View(), "\n") + 1; lines != 40 {
		t.Fatalf("view has %d lines", lines)
	}
	countBlank := func(m Model) int {
		n := 0
		for _, l := range m.msgLines {
			if strings.TrimSpace(stripANSI(l)) == "" {
				n++
			}
		}
		return n
	}
	before := countBlank(m)
	// spacing: normal → compact (h goes back one)
	m = selectSetting(t, m, "Space between messages")
	m, _ = keys(t, m, "h")
	if config.Config.Ui.MessageSpacing != "compact" || countBlank(m) >= before {
		t.Fatalf("compact: %q, blank lines %d (were %d)", config.Config.Ui.MessageSpacing, countBlank(m), before)
	}
	// bubbles: rounded → square
	m = selectSetting(t, m, "Bubble borders")
	m, _ = keys(t, m, "l")
	m, _ = keys(t, m, "esc")
	if m.settings != nil || strings.Count(stripANSI(m.View()), "┌─") < 3 {
		t.Fatal("square bubbles not drawn")
	}
	// theme: live
	m, _ = keys(t, m, ":", "s", "e", "t", "t", "i", "n", "g", "s", "enter")
	m = selectSetting(t, m, "Theme")
	m, _ = keys(t, m, "l", "l")
	if pal.Base != RosePineDawn.Base {
		t.Fatal("theme not applied")
	}
	// a restart-only one says so
	m = selectSetting(t, m, "Pictures")
	m, _ = keys(t, m, "l")
	if !strings.Contains(m.notice, "applies when the app starts again") {
		t.Fatalf("notice %q", m.notice)
	}
	// an edit row opens the : line
	m = selectSetting(t, m, "Downloads folder")
	m, _ = keys(t, m, "enter")
	if m.settings != nil || m.mode != modeCommand || m.cmdline.Value() != "download-dir " {
		t.Fatalf("edit row: %q", m.cmdline.Value())
	}
	m, _ = keys(t, m, "esc")
	// AI off and on again, without a restart
	m.ai = &fakeAI{}
	m.openSettings()
	m = selectSetting(t, m, "AI help (local model)")
	m, _ = keys(t, m, "l")
	if m.ai != nil {
		t.Fatal("AI still on")
	}
	m, _ = keys(t, m, "l")
	if m.ai == nil {
		t.Fatal("AI not back")
	}
	// the palette has it
	m, _ = keys(t, m, "esc")
	m, _ = press(t, m, tea.KeyF1)
	m, _ = keys(t, m, strings.Split("settings", "")...)
	if m.qo.items[0].cmd.id != "settings" {
		t.Fatalf("palette: %s", m.qo.items[0].title)
	}
	_ = messages.Message{}
	_ = termimg.ModeOff
}
