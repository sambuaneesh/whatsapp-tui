package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"gopkg.in/ini.v1"
)

var configFilePath string
var cfg *ini.File

type IniFile struct {
	*General
	*Keymap
	*Ui
	*Colors
}

type General struct {
	DownloadPath        string
	PreviewPath         string
	CmdPrefix           string
	ShowCommand         string
	EnableNotifications bool   // old switch, used when Notifications is unset
	UseTerminalBell     bool   // with EnableNotifications: a sound instead of a popup
	Notifications       string // all, popup, sound or off
	Background          bool   // keep running after the window closes; launches attach
	MediaCacheMb        int    // downloaded media kept (MB); least recently used go first
	PrivateReading      bool   // open chats without sending read receipts (mark read yourself)
	ApiAllowSend        bool   // let scripts send through the local API (docs/API.md)
	SemanticSearch      bool   // search by meaning with a local model (Ollama)
	OllamaUrl           string // where Ollama listens
	EmbedModel          string // its embedding model
	SemanticPauseGaming bool   // don't index while the GPU is busy (over 50%)
	ObsidianDir         string // mirror your lists and notes to this folder as Markdown ("" off)
	Ai                  bool   // let the local chat model help (dates, tasks from chats, plans, summaries)
	AiModel             string // the Ollama chat model for that
	NotificationTimeout int64
	BacklogMsgQuantity  int
}

type Keymap struct {
	SwitchPanels    string
	FocusMessages   string
	FocusInput      string
	FocusChats      string
	Copyuser        string
	Pasteuser       string
	CommandBacklog  string
	CommandRead     string
	CommandConnect  string
	CommandQuit     string
	CommandHelp     string
	MessageDownload string
	MessageOpen     string
	MessageShow     string
	MessageUrl      string
	MessageInfo     string
	MessageRevoke   string
}

type Ui struct {
	ChatSidebarWidth int
	QrCompact        bool
	Theme            string // rose-pine, rose-pine-moon or rose-pine-dawn
	PaintBackground  bool   // fill the screen with the theme background
	Images           string // auto, kitty, blocks or off
	Avatars          bool   // show profile pictures (kitty only)
	Mouse            bool   // click a chat to open it, wheel to scroll
	// HighlightOpacity: how solid highlights, bubbles and the status bar are
	// over a see-through terminal (0..1; 1 = solid). kitty only.
	HighlightOpacity float64
	MessageSpacing   string // compact, normal or roomy: blank lines between messages
	BubbleStyle      string // rounded, square, thick, double or none
}

type Colors struct {
	Background      string
	Text            string
	ForwardedText   string
	ListHeader      string
	ListContact     string
	ListGroup       string
	ChatContact     string
	ChatMe          string
	Borders         string
	InputBackground string
	InputText       string
	UnreadCount     string
	Positive        string
	Negative        string
}

var Config = IniFile{
	&General{
		DownloadPath:        GetHomeDir() + "Downloads",
		PreviewPath:         GetHomeDir() + "Downloads",
		CmdPrefix:           "/",
		ShowCommand:         "jp2a --color",
		EnableNotifications: false,
		UseTerminalBell:     false,
		NotificationTimeout: 60,
		Background:          true,
		MediaCacheMb:        1024,
		SemanticSearch:      true,
		OllamaUrl:           "http://127.0.0.1:11434",
		EmbedModel:          "embeddinggemma",
		SemanticPauseGaming: true,
		Ai:                  true,
		AiModel:             "qwen3:4b",
		BacklogMsgQuantity:  10,
	},
	&Keymap{
		SwitchPanels:    "Tab",
		FocusMessages:   "Ctrl+w",
		FocusInput:      "Ctrl+Space",
		FocusChats:      "Ctrl+e",
		CommandBacklog:  "Ctrl+b",
		CommandRead:     "Ctrl+n",
		Copyuser:        "Ctrl+c",
		Pasteuser:       "Ctrl+v",
		CommandConnect:  "Ctrl+r",
		CommandQuit:     "Ctrl+q",
		CommandHelp:     "Ctrl+?",
		MessageDownload: "d",
		MessageInfo:     "i",
		MessageOpen:     "o",
		MessageUrl:      "u",
		MessageRevoke:   "r",
		MessageShow:     "s",
	},
	&Ui{
		ChatSidebarWidth: 38,
		QrCompact:        false,
		Theme:            "rose-pine",
		PaintBackground:  false,
		Images:           "auto",
		Avatars:          true,
		Mouse:            true,
		HighlightOpacity: 0.8,
		MessageSpacing:   "normal",
		BubbleStyle:      "rounded",
	},
	&Colors{
		Background:      "black",
		Text:            "white",
		ForwardedText:   "purple",
		ListHeader:      "yellow",
		ListContact:     "green",
		ListGroup:       "blue",
		ChatContact:     "green",
		ChatMe:          "blue",
		Borders:         "white",
		InputBackground: "blue",
		InputText:       "white",
		UnreadCount:     "yellow",
		Positive:        "green",
		Negative:        "red",
	},
}

func InitConfig() error {
	var err error
	if configFilePath, err = xdg.ConfigFile("whatsapp-tui/config.ini"); err == nil {
		// add any new values
		var cfg *ini.File
		if cfg, err = ini.Load(configFilePath); err == nil {
			cfg.NameMapper = ini.TitleUnderscore
			cfg.ValueMapper = os.ExpandEnv
			if section, err := cfg.GetSection("general"); err == nil {
				section.MapTo(&Config.General)
			}
			if section, err := cfg.GetSection("keymap"); err == nil {
				section.MapTo(&Config.Keymap)
			}
			if section, err := cfg.GetSection("ui"); err == nil {
				section.MapTo(&Config.Ui)
			}
			if section, err := cfg.GetSection("colors"); err == nil {
				section.MapTo(&Config.Colors)
			}
		} else {
			cfg = ini.Empty()
			cfg.NameMapper = ini.TitleUnderscore
			cfg.ValueMapper = os.ExpandEnv
			if err = ini.ReflectFromWithMapper(cfg, &Config, ini.TitleUnderscore); err == nil {
				err = cfg.SaveTo(configFilePath)
			}
		}
	}
	return err
}

func GetConfigFilePath() string {
	return configFilePath
}

// GetCacheDir returns the directory for downloaded media and avatars.
func GetCacheDir() string {
	return filepath.Join(xdg.CacheHome, "whatsapp-tui")
}

func GetSessionFilePath() string {
	if sessionFilePath, err := xdg.ConfigFile("whatsapp-tui/session"); err == nil {
		return sessionFilePath
	}
	return GetHomeDir() + ".whatsapp-tui.session"
}

// gets the OS home dir with a path separator at the end
func GetHomeDir() string {
	usr, err := user.Current()
	if err == nil {
		return usr.HomeDir + string(os.PathSeparator)
	}
	// Fallback to environment variable
	home := os.Getenv("HOME")
	if home != "" {
		return home + string(os.PathSeparator)
	}
	return "." + string(os.PathSeparator)
}

// ExpandPath expands a leading "~" and environment variables.
func ExpandPath(p string) string {
	p = os.ExpandEnv(strings.TrimSpace(p))
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(GetHomeDir(), strings.TrimPrefix(p, "~"))
	}
	return filepath.Clean(p)
}

// SetDownloadPath changes where downloads are saved and writes it to the
// config file, keeping the rest of the file as it is.
func SetDownloadPath(p string) error {
	p = ExpandPath(p)
	if err := saveGeneral("download_path", p); err != nil {
		return err
	}
	Config.General.DownloadPath = p
	return nil
}

// Notification modes: what a new message does.
const (
	NotifyAll   = "all"   // a popup in the notification drawer and a sound
	NotifyPopup = "popup" // a silent popup
	NotifySound = "sound" // only a sound
	NotifyOff   = "off"
)

// NotificationMode is the configured mode, falling back to the old
// enable_notifications / use_terminal_bell switches.
func NotificationMode() string {
	g := Config.General
	switch m := strings.ToLower(strings.TrimSpace(g.Notifications)); m {
	case NotifyAll, NotifyPopup, NotifySound, NotifyOff:
		return m
	}
	switch {
	case !g.EnableNotifications:
		return NotifyOff
	case g.UseTerminalBell:
		return NotifySound
	}
	return NotifyAll
}

// SetNotificationMode changes the mode and writes it to the config file.
func SetNotificationMode(mode string) error {
	if err := saveGeneral("notifications", mode); err != nil {
		return err
	}
	Config.General.Notifications = mode
	return nil
}

// SetPrivateReading turns private reading on or off and saves it.
func SetPrivateReading(on bool) error {
	if err := saveGeneral("private_reading", fmt.Sprint(on)); err != nil {
		return err
	}
	Config.General.PrivateReading = on
	return nil
}

// SetObsidianDir sets (and saves) the folder lists are mirrored to.
func SetObsidianDir(dir string) error {
	if err := saveGeneral("obsidian_dir", dir); err != nil {
		return err
	}
	Config.General.ObsidianDir = dir
	return nil
}

// Save writes one key of a section ("general", "ui") to the config file,
// keeping the rest of it as it is. The caller updates Config itself.
func Save(section, key, value string) error {
	if configFilePath == "" {
		return nil
	}
	f, err := ini.Load(configFilePath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	f.Section(section).Key(key).SetValue(value)
	if err := f.SaveTo(configFilePath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// saveGeneral writes one [general] key to the config file, keeping the rest
// of the file as it is (nothing is written when there is no file).
func saveGeneral(key, value string) error {
	if configFilePath == "" {
		return nil
	}
	f, err := ini.Load(configFilePath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	f.Section("general").Key(key).SetValue(value)
	if err := f.SaveTo(configFilePath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
