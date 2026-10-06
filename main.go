// whatsapp-tui is a vim-style terminal WhatsApp client.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	runtimedebug "runtime/debug"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/termenv"

	"github.com/Srindot/whatsapp-tui/internal/ai"
	"github.com/Srindot/whatsapp-tui/internal/api"
	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/daemon"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/notify"
	"github.com/Srindot/whatsapp-tui/internal/personal"
	"github.com/Srindot/whatsapp-tui/internal/semantic"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
	"github.com/Srindot/whatsapp-tui/internal/ui"
)

// mediaSource adapts the session manager to the UI, honouring the avatars
// setting.
type mediaSource struct {
	*messages.SessionManager
	avatars bool
}

func (m mediaSource) ProfilePicture(ctx context.Context, jid string) (string, error) {
	if !m.avatars {
		return "", nil
	}
	return m.SessionManager.ProfilePicture(ctx, jid)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "whatsapp-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	// whatsapp-tui api <method> [params]: talk to the running app (docs/API.md)
	if len(os.Args) > 1 && os.Args[1] == "api" {
		if len(os.Args) < 3 {
			return errors.New("usage: whatsapp-tui api <method> ['{json params}'] (methods: docs/API.md)")
		}
		params := ""
		if len(os.Args) > 3 {
			params = os.Args[3]
		}
		return api.Call(api.SocketPath(daemon.SocketPath()), os.Args[2], params, os.Stdout)
	}
	// whatsapp-tui todo / note / capture: your lists from the shell
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "todo", "task", "tasks":
			return todoCmd(os.Args[2:])
		case "note":
			return noteCmd(os.Args[2:])
		case "capture":
			return captureCmd()
		}
	}
	debug := flag.Bool("debug", false, "write WhatsApp protocol logs to "+debugLogPath())
	foreground := flag.Bool("foreground", false, "run in this terminal only, not in the background")
	stop := flag.Bool("stop", false, "quit the app running in the background")
	server := flag.Bool("server", false, "run as the background app (started by whatsapp-tui itself)")
	flag.Parse()

	if err := config.InitConfig(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if *stop {
		return daemon.Stop()
	}
	if mb := config.Config.General.MediaCacheMb; mb > 0 {
		messages.MediaCacheLimit = int64(mb) << 20
	}
	if !*server && !*foreground && config.Config.General.Background && term.IsTerminal(os.Stdin.Fd()) {
		// attach this window to the one app, starting it if needed
		var args []string
		if *debug {
			args = append(args, "--debug")
		}
		return daemon.Attach(args, serverLogPath())
	}

	if !*server && daemon.Running() {
		// two copies would fight over the same WhatsApp session
		return errors.New("it's already running in the background: run whatsapp-tui without --foreground to open it, or whatsapp-tui --stop first")
	}
	servePprof()
	var srv *daemon.Server
	if *server {
		var err error
		if srv, err = daemon.Listen(); err != nil {
			if errors.Is(err, daemon.ErrRunning) {
				return nil // another launch got there first
			}
			return fmt.Errorf("background: %w", err)
		}
		// deferred first so it runs last: the lock is held until WhatsApp
		// has shut down and a new server can safely start
		defer srv.Close()
		// running all day in the background: collect garbage sooner, so the
		// app holds less memory between bursts (UI updates barely allocate)
		runtimedebug.SetGCPercent(50)
		// the app runs on the server's terminal, which attached windows show
		os.Stdin, os.Stdout = srv.TTY(), srv.TTY()
		// the colour support was guessed when the program loaded, from the
		// log file it writes to then: plain text. Use the terminal's
		// (TERM/COLORTERM come from the window that started us), and don't
		// ask the terminal for its background: no window may be attached.
		lipgloss.SetColorProfile(termenv.NewOutput(srv.TTY()).EnvColorProfile())
		lipgloss.SetHasDarkBackground(!strings.Contains(config.Config.Ui.Theme, "dawn"))
	}

	// The handler forwards backend events into the program; the program
	// is created after the session manager, so wire send in afterwards.
	handler := ui.NewHandler(func(tea.Msg) {})
	sm := &messages.SessionManager{}
	if err := sm.Init(handler); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer sm.Shutdown()
	if *debug {
		if err := os.MkdirAll(filepath.Dir(debugLogPath()), 0o700); err != nil {
			return fmt.Errorf("debug log: %w", err)
		}
		f, err := os.OpenFile(debugLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("debug log: %w", err)
		}
		defer f.Close()
		sm.SetDebug(true)
		sm.SetLogWriter(f)
	}

	opts := ui.Options{
		SidebarWidth:    config.Config.Ui.ChatSidebarWidth,
		Theme:           config.Config.Ui.Theme,
		PaintBackground: config.Config.Ui.PaintBackground,
		Images:          termimg.Detect(config.Config.Ui.Images),
		Media:           mediaSource{sm, config.Config.Ui.Avatars},
		Sender:          sm,
		Actions:         sm,
		Searcher:        sm,
		GlobalSearcher:  sm,
		Forwarder:       sm,
		Privacy:         sm,
		Stickers:        sm,
		Mentioner:       sm,
		Pictures:        sm,
		Deleter:         sm,
		Drafts:          sm,
		Triage:          sm,
		Scheduler:       sm,
		Activity:        sm,
		Chats:           sm,
	}
	// the screen and the images share the terminal; writes go out whole
	out := termimg.NewOutput(os.Stdout)
	if opts.Images == termimg.ModeKitty {
		kitty, err := termimg.NewKitty(out)
		if err != nil {
			opts.Images = termimg.ModeBlocks
		} else {
			opts.Kitty = kitty
			defer kitty.Close()
		}
	}
	if srv != nil {
		opts.Detach = srv.Detach
		opts.NotifyLog = os.Stderr // the background app's log
	}
	// your own lists, notes and saved messages, in their own database
	ps, err := personal.Open(filepath.Join(filepath.Dir(config.GetConfigFilePath()), "personal.db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "lists and notes are off: %v\n", err)
	} else {
		defer ps.Close()
		opts.Personal = ps
		mirror := personal.NewMirror(ps, config.ExpandPath(config.Config.General.ObsidianDir))
		mirror.Log = func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
		mirror.Start(2 * time.Second)
		defer mirror.Stop()
		opts.Mirror = mirror
	}
	if g := config.Config.General; g.Ai && g.AiModel != "" {
		// the local chat model: tasks from messages, odd dates, plans, catch-ups
		opts.AI = &ai.Client{URL: g.OllamaUrl, Model: g.AiModel}
	}
	opts.Mouse = config.Config.Ui.Mouse
	opts.PrivateReading = config.Config.General.PrivateReading
	opts.Notifications, opts.Notifier = config.NotificationMode(), notify.System{}
	model := ui.New(sm.CommandChannel, sm.Conversations(), opts)
	progOpts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithReportFocus(), tea.WithOutput(out)} // focus: only mark chats read while you look
	if config.Config.Ui.Mouse {
		progOpts = append(progOpts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(model, progOpts...)
	handler.SetSend(p.Send)
	if ps != nil {
		// changes from the API, the CLI or the Markdown mirror redraw
		ps.OnChange = func() { go p.Send(ui.PersonalChangedMsg{}) }
	}

	// Mark this kitty window so kitty.conf can pass keys it normally handles
	// itself (ctrl+v, shift+enter) to us; see the README. Also see-through
	// highlights over a see-through window.
	var hello, bye string
	if termimg.Detect("auto") == termimg.ModeKitty {
		hello, bye = "\x1b]1337;SetUserVar=whatsapp_tui=MQ==\x07", "\x1b]1337;SetUserVar=whatsapp_tui\x07"
		if !config.Config.Ui.PaintBackground {
			on, off := ui.KittyTransparency(config.Config.Ui.Theme, config.Config.Ui.HighlightOpacity)
			hello, bye = hello+on, bye+off
		}
	}
	if srv != nil {
		// every window that attaches gets the marks, and its own redraw
		mouse := ""
		if config.Config.Ui.Mouse {
			mouse = "\x1b[?1002h\x1b[?1006h"
		}
		// With no window attached, Bubble Tea lets go of the terminal: its
		// renderer otherwise wakes 60 times a second for nothing. The app
		// keeps running (messages, notifications, scheduled sends).
		var termMu sync.Mutex
		released := false
		release := func() {
			termMu.Lock()
			defer termMu.Unlock()
			if !released && p.ReleaseTerminal() == nil {
				released = true
			}
		}
		restore := func() {
			termMu.Lock()
			defer termMu.Unlock()
			if released && p.RestoreTerminal() == nil {
				released = false
			}
		}
		srv.Serve(daemon.Hooks{
			Prelude: func() string { return mouse + hello },
			Goodbye: func() string { return bye },
			Attach: func(z daemon.Size) {
				restore()
				p.Send(ui.ReattachMsg{})
				p.Send(tea.WindowSizeMsg{Width: int(z.Cols), Height: int(z.Rows)})
				p.Send(tea.FocusMsg{})
			},
			Resize: func(z daemon.Size) { p.Send(tea.WindowSizeMsg{Width: int(z.Cols), Height: int(z.Rows)}) },
			Detach: func() {
				p.Send(tea.BlurMsg{}) // nobody's looking: notify for every chat
				release()
			},
			Quit: p.Quit,
		})
		srv.WaitFirst() // start with the first window's size and terminal
	} else {
		fmt.Fprint(out, hello)
		defer fmt.Fprint(out, bye)
	}

	if err := sm.StartManager(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	// search by meaning, if a local model runs (does nothing otherwise)
	if g := config.Config.General; g.SemanticSearch && g.OllamaUrl != "" {
		var busy func() bool
		if g.SemanticPauseGaming {
			busy = func() bool { return semantic.GPUBusy(50) }
		}
		sm.StartSemanticGentle(semantic.Ollama{URL: strings.TrimRight(g.OllamaUrl, "/"), Model: g.EmbedModel}, busy)
	}
	// the local API for scripts and hooks (docs/API.md)
	apiOpts := api.Options{
		AllowSend: config.Config.General.ApiAllowSend,
		HooksDir:  filepath.Join(filepath.Dir(config.GetConfigFilePath()), "hooks"),
	}
	if srv != nil {
		apiOpts.Log = func(s string) { fmt.Fprintln(os.Stderr, s) } // the background app's log
	}
	apiOpts.Personal = opts.Personal
	if apiSrv, err := api.Listen(api.SocketPath(daemon.SocketPath()), sm, apiOpts); err == nil {
		defer apiSrv.Close()
	}
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("ui: %w", err)
	}
	return nil
}

func serverLogPath() string { return filepath.Join(config.GetCacheDir(), "server.log") }

func debugLogPath() string { return filepath.Join(config.GetCacheDir(), "debug.log") }
