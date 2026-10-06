package messages

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// setChatFlags applies archive/pin changes from the phone (app state).
// A nil value leaves that flag alone.
func (sm *SessionManager) setChatFlags(jid types.JID, archived, pinned *bool) {
	jid = sm.pnForLID(context.Background(), jid.ToNonAD())
	key := jid.String()

	sm.mu.Lock()
	conv := sm.convByJID[key]
	if conv == nil {
		sm.mu.Unlock()
		return // a chat we don't show (no messages yet)
	}
	changed := false
	if archived != nil && conv.IsArchived != *archived {
		conv.IsArchived, changed = *archived, true
	}
	if pinned != nil && conv.IsPinned != *pinned {
		conv.IsPinned, changed = *pinned, true
		sm.priorityQueue.Update(conv, conv.LastMsgTime, conv.IsPinned)
	}
	c := *conv
	sm.mu.Unlock()

	if !changed {
		return
	}
	if err := sm.db.UpsertConversation(c); err != nil {
		sm.debugf("save chat flags for %s: %v", key, err)
	}
	sm.scheduleListPush()
}

// setChatRead applies a chat read (or marked unread) on another device.
// Nothing is sent back: that device already told WhatsApp.
func (sm *SessionManager) setChatRead(jid types.JID, read bool) {
	jid = sm.pnForLID(context.Background(), jid.ToNonAD())
	key := jid.String()

	sm.mu.Lock()
	conv := sm.convByJID[key]
	if conv == nil {
		sm.mu.Unlock()
		return
	}
	changed := false
	switch {
	case read && (conv.Unread > 0 || conv.Mentioned):
		conv.Unread, conv.Mentioned, changed = 0, false, true
	case !read && conv.Unread == 0:
		conv.Unread, changed = 1, true // "mark as unread" shows as one
	}
	c := *conv
	sm.mu.Unlock()

	if !changed {
		return
	}
	if err := sm.db.UpsertConversation(c); err != nil {
		sm.debugf("save read state for %s: %v", key, err)
	}
	sm.scheduleListPush()
}

// scheduleListPush sends the chat list to the UI shortly after changes stop
// arriving (an app state sync can deliver hundreds at once).
func (sm *SessionManager) scheduleListPush() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.listTimer != nil {
		sm.listTimer.Reset(300 * time.Millisecond)
		return
	}
	sm.listTimer = time.AfterFunc(300*time.Millisecond, func() {
		sm.mu.Lock()
		sm.listTimer = nil
		list := sm.snapshotPQ()
		sm.mu.Unlock()
		sm.uiHandler.UpdateChatList(list)
	})
}

// renameChat records a group's new name (renamed on any device).
func (sm *SessionManager) renameChat(jid types.JID, name string) {
	if name == "" {
		return
	}
	key := jid.ToNonAD().String()
	sm.mu.Lock()
	conv := sm.convByJID[key]
	if conv == nil || conv.Name == name {
		sm.mu.Unlock()
		return
	}
	conv.Name = name
	c := *conv
	sm.mu.Unlock()
	if err := sm.db.UpsertConversation(c); err != nil {
		sm.debugf("save name of %s: %v", key, err)
	}
	sm.scheduleListPush()
}

// SaveDraft keeps what you were writing in a chat ("" removes it).
func (sm *SessionManager) SaveDraft(jid, text string) error { return sm.db.SaveDraft(jid, text) }

// Drafts returns every chat's draft.
func (sm *SessionManager) Drafts() map[string]string {
	d, err := sm.db.Drafts()
	if err != nil {
		sm.debugf("load drafts: %v", err)
		return map[string]string{}
	}
	return d
}

// setChatMute records a chat muted (until a time, or always) or unmuted
// on any of your devices.
func (sm *SessionManager) setChatMute(jid types.JID, muted bool, end int64) {
	jid = sm.pnForLID(context.Background(), jid.ToNonAD())
	key := jid.String()
	until := int64(0)
	if muted {
		until = muteUntil(end)
		if end == 0 {
			until = -1
		}
	}
	sm.mu.Lock()
	conv := sm.convByJID[key]
	if conv == nil || conv.MutedUntil == until {
		sm.mu.Unlock()
		return
	}
	conv.MutedUntil = until
	c := *conv
	sm.mu.Unlock()
	if err := sm.db.UpsertConversation(c); err != nil {
		sm.debugf("save mute for %s: %v", key, err)
	}
	sm.scheduleListPush()
}
