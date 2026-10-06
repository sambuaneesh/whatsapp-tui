package messages

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

// A full app state sync replays the archive, pin and mute settings
// WhatsApp has for your chats. A chat it doesn't mention isn't archived,
// pinned or muted, but nothing says so: a phone that unarchived a chat
// while this device missed the update (say, after a sync conflict) left it
// archived here for good. So the sync notes which chats it mentions, and
// afterwards clears the settings of the ones it didn't.

type syncSeen struct {
	archived, pinned, muted map[string]bool
}

// noteFullSync records a chat a full sync mentioned (kind: "archive",
// "pin" or "mute").
func (sm *SessionManager) noteFullSync(kind string, jid types.JID) {
	key := sm.pnForLID(context.Background(), jid.ToNonAD()).String()
	sm.mu.Lock()
	defer sm.mu.Unlock()
	s := sm.syncSeen
	if s == nil {
		return
	}
	switch kind {
	case "archive":
		s.archived[key] = true
	case "pin":
		s.pinned[key] = true
	case "mute":
		s.muted[key] = true
	}
}

// ResyncChatSettings fetches your chats' archive, pin, mute and read
// settings from WhatsApp again in full and makes this device match: what
// your phone shows is what you get. Returns how many chats changed.
func (sm *SessionManager) ResyncChatSettings(ctx context.Context) (int, error) {
	if sm.getClient() == nil {
		return 0, errors.New("not connected to WhatsApp")
	}
	sm.mu.Lock()
	sm.syncSeen = &syncSeen{archived: map[string]bool{}, pinned: map[string]bool{}, muted: map[string]bool{}}
	sm.mu.Unlock()
	defer func() {
		sm.mu.Lock()
		sm.syncSeen = nil
		sm.mu.Unlock()
	}()

	lowErr := sm.fetchAppState(ctx, appstate.WAPatchRegularLow)   // archive, pin, read
	highErr := sm.fetchAppState(ctx, appstate.WAPatchRegularHigh) // mute

	sm.mu.RLock()
	seen := sm.syncSeen
	var stale []Conversation
	for _, c := range sm.convByJID {
		if (lowErr == nil && ((c.IsArchived && !seen.archived[c.JID]) || (c.IsPinned && !seen.pinned[c.JID]))) ||
			(highErr == nil && c.MutedUntil != 0 && !seen.muted[c.JID]) {
			stale = append(stale, *c)
		}
	}
	sm.mu.RUnlock()

	no := false
	for _, c := range stale {
		jid, err := types.ParseJID(c.JID)
		if err != nil {
			continue
		}
		sm.debugf("resync: %s isn't archived/pinned/muted on WhatsApp any more", c.JID)
		if lowErr == nil {
			if c.IsArchived && !seen.archived[c.JID] {
				sm.setChatFlags(jid, &no, nil)
			}
			if c.IsPinned && !seen.pinned[c.JID] {
				sm.setChatFlags(jid, nil, &no)
			}
		}
		if highErr == nil && c.MutedUntil != 0 && !seen.muted[c.JID] {
			sm.setChatMute(jid, false, 0)
		}
	}
	switch {
	case lowErr != nil:
		return len(stale), fmt.Errorf("archive/pin sync: %w", lowErr)
	case highErr != nil:
		return len(stale), fmt.Errorf("mute sync: %w", highErr)
	}
	return len(stale), nil
}
