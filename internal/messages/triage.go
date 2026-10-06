package messages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

// ArchiveChat archives (or brings back) a chat on all your devices.
func (sm *SessionManager) ArchiveChat(ctx context.Context, chatStr string, archive bool) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	chat, err := types.ParseJID(chatStr)
	if err != nil {
		return fmt.Errorf("invalid chat: %w", err)
	}
	lastTS, lastKey := sm.lastMessageKey(client, chat)
	if err := client.SendAppState(ctx, appstate.BuildArchive(chat, archive, lastTS, lastKey)); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	sm.setChatFlags(chat, &archive, nil)
	return nil
}

// MarkChatUnread marks a chat unread on all your devices (or read again:
// that sends read receipts, like opening it).
func (sm *SessionManager) MarkChatUnread(ctx context.Context, chatStr string, unread bool) error {
	if !unread {
		sm.markChatAsRead(chatStr)
		return nil
	}
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	chat, err := types.ParseJID(chatStr)
	if err != nil {
		return fmt.Errorf("invalid chat: %w", err)
	}
	lastTS, lastKey := sm.lastMessageKey(client, chat)
	if err := client.SendAppState(ctx, appstate.BuildMarkChatAsRead(chat, false, lastTS, lastKey)); err != nil {
		return fmt.Errorf("mark unread: %w", err)
	}
	sm.setChatRead(chat, false)
	return nil
}

// PinChat pins (or unpins) a chat to the top of the list on all your
// devices. WhatsApp allows three pinned chats.
func (sm *SessionManager) PinChat(ctx context.Context, chatStr string, pin bool) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	chat, err := types.ParseJID(chatStr)
	if err != nil {
		return fmt.Errorf("invalid chat: %w", err)
	}
	if pin {
		n := 0
		sm.mu.RLock()
		for _, c := range sm.convByJID {
			if c.IsPinned && c.JID != chatStr {
				n++
			}
		}
		sm.mu.RUnlock()
		if n >= 3 {
			return errors.New("WhatsApp allows 3 pinned chats: unpin one first")
		}
	}
	if err := client.SendAppState(ctx, appstate.BuildPin(chat, pin)); err != nil {
		return fmt.Errorf("pin: %w", err)
	}
	sm.setChatFlags(chat, nil, &pin)
	return nil
}

// MuteChat mutes a chat for d on all your devices (d < 0: always), or
// unmutes it (d = 0).
func (sm *SessionManager) MuteChat(ctx context.Context, chatStr string, d time.Duration) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	chat, err := types.ParseJID(chatStr)
	if err != nil {
		return fmt.Errorf("invalid chat: %w", err)
	}
	var end *int64
	if d > 0 {
		e := time.Now().Add(d).UnixMilli()
		end = &e
	}
	if err := client.SendAppState(ctx, appstate.BuildMuteAbs(chat, d != 0, end)); err != nil {
		return fmt.Errorf("mute: %w", err)
	}
	if d == 0 {
		sm.setChatMute(chat, false, 0)
	} else if end != nil {
		sm.setChatMute(chat, true, *end)
	} else {
		sm.setChatMute(chat, true, 0)
	}
	return nil
}
