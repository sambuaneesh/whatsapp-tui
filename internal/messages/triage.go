package messages

import (
	"context"
	"errors"
	"fmt"

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
