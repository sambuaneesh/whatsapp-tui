package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Hooks are executables in the hooks directory named after events:
//
//	on-message   every message (yours too; check "from_me")
//	on-mention   a message that mentions you (or @all)
//	on-reaction  a reaction (yours too)
//	on-reminder  a snoozed chat came back, or a nudge
//
// Each runs with the event's JSON (see EventJSON) on stdin and
// WT_EVENT / WT_CHAT in the environment, at most hookTimeout, and at most
// hookParallel at a time (more wait). Output goes to the background app's
// log when a hook fails.

const (
	hookTimeout  = 30 * time.Second
	hookParallel = 4
)

func (s *Server) runHooks() {
	slots := make(chan struct{}, hookParallel)
	s.b.Subscribe(func(e messages.Event) {
		for _, ev := range s.eventsFor(e) {
			path := filepath.Join(s.opts.HooksDir, "on-"+ev.Event)
			if st, err := os.Stat(path); err != nil || st.Mode()&0o111 == 0 {
				continue // no hook for it (or not executable)
			}
			data, _ := json.Marshal(ev)
			chat := ""
			switch {
			case ev.Message != nil:
				chat = ev.Message.Chat
			case ev.Reaction != nil:
				chat = ev.Reaction.Chat
			case ev.Reminder != nil:
				chat = ev.Reminder.Chat
			}
			slots <- struct{}{}
			go func() {
				defer func() { <-slots }()
				ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
				defer cancel()
				cmd := exec.CommandContext(ctx, path)
				cmd.Stdin = bytes.NewReader(append(data, '\n'))
				cmd.Env = append(os.Environ(), "WT_EVENT="+ev.Event, "WT_CHAT="+chat)
				var out bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &out
				if err := cmd.Run(); err != nil && s.opts.Log != nil {
					s.opts.Log(fmt.Sprintf("hook %s: %v: %s", filepath.Base(path), err, bytes.TrimSpace(out.Bytes())))
				}
			}()
		}
	})
}
