package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// Pinned messages: anyone in a chat can pin a message for everyone, for 24
// hours, 7 days or 30 days; WhatsApp shows the pins at the top of the chat.

// Pin durations WhatsApp offers.
var PinDurations = []time.Duration{24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour}

// DefaultPinDuration is what pinning without choosing uses (as the phone
// does).
const DefaultPinDuration = 7 * 24 * time.Hour

func (md *MessageDatabase) initPins() error {
	_, err := md.db.Exec(`CREATE TABLE IF NOT EXISTS pins (
		msg_id TEXT PRIMARY KEY,
		chat_id TEXT,
		pinned_by TEXT,
		timestamp INTEGER, -- ms, when it was pinned (or unpinned)
		expires INTEGER,   -- ms; 0 when unpinned
		pinned INTEGER
	)`)
	if err != nil {
		return err
	}
	_, err = md.db.Exec(`CREATE INDEX IF NOT EXISTS idx_pins_chat ON pins(chat_id, pinned)`)
	return err
}

// SetPin records a pin or unpin (by "" for you), keeping the newest when
// events arrive out of order.
func (md *MessageDatabase) SetPin(chat, msgID, by string, pinned bool, ts, expires int64) error {
	if md.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if !pinned {
		expires = 0
	}
	_, err := md.db.Exec(`
	INSERT INTO pins (msg_id, chat_id, pinned_by, timestamp, expires, pinned) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(msg_id) DO UPDATE SET pinned_by = excluded.pinned_by, timestamp = excluded.timestamp,
		expires = excluded.expires, pinned = excluded.pinned
	WHERE excluded.timestamp >= pins.timestamp`, msgID, chat, by, ts, expires, pinned)
	return err
}

// PinnedIDs returns the messages pinned in chat now, newest pin first.
func (md *MessageDatabase) PinnedIDs(chat string, now time.Time) ([]string, error) {
	if md.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := md.db.Query(`SELECT msg_id FROM pins WHERE chat_id = ? AND pinned = 1 AND expires > ?
		ORDER BY timestamp DESC`, chat, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PinnedMessages returns the messages pinned in chat, newest pin first.
func (sm *SessionManager) PinnedMessages(_ context.Context, chat string) ([]Message, error) {
	ids, err := sm.db.PinnedIDs(chat, time.Now())
	if err != nil {
		return nil, err
	}
	var out []Message
	for _, id := range ids {
		m, err := sm.db.GetMessage(id)
		if err != nil {
			continue // pinned before our history starts
		}
		m.Pinned = true
		out = append(out, m)
	}
	sm.resolveSenders(out)
	return out, nil
}

// attachPins sets Message.Pinned.
func (sm *SessionManager) attachPins(chat string, msgs []Message) {
	ids, err := sm.db.PinnedIDs(chat, time.Now())
	if err != nil || len(ids) == 0 {
		return
	}
	pinned := make(map[string]bool, len(ids))
	for _, id := range ids {
		pinned[id] = true
	}
	for i := range msgs {
		msgs[i].Pinned = pinned[msgs[i].Id]
	}
}

// PinMessage pins m for everyone in the chat for d, or unpins it (d = 0).
func (sm *SessionManager) PinMessage(ctx context.Context, m Message, d time.Duration) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	if strings.HasPrefix(m.Text, "[REVOKED]") || m.Deleted != 0 {
		return errors.New("a deleted message can't be pinned")
	}
	chat, err := types.ParseJID(m.ChatId)
	if err != nil {
		return fmt.Errorf("invalid chat: %w", err)
	}
	author, err := sm.senderJID(m)
	if err != nil {
		return err
	}
	now := time.Now()
	typ := waE2E.PinInChatMessage_PIN_FOR_ALL
	if d <= 0 {
		typ = waE2E.PinInChatMessage_UNPIN_FOR_ALL
	}
	msg := &waE2E.Message{PinInChatMessage: &waE2E.PinInChatMessage{
		Key:               client.BuildMessageKey(chat, author, m.Id),
		Type:              typ.Enum(),
		SenderTimestampMS: proto.Int64(now.UnixMilli()),
	}}
	if d > 0 {
		msg.MessageContextInfo = &waE2E.MessageContextInfo{
			MessageAddOnDurationInSecs: proto.Uint32(uint32(d / time.Second)),
			MessageAddOnExpiryType:     waE2E.MessageContextInfo_STATIC.Enum(),
		}
	}
	if _, err := client.SendMessage(ctx, chat, msg); err != nil {
		return fmt.Errorf("pin: %w", err)
	}
	if err := sm.db.SetPin(m.ChatId, m.Id, "", d > 0, now.UnixMilli(), now.Add(d).UnixMilli()); err != nil {
		return fmt.Errorf("save the pin: %w", err)
	}
	sm.refreshIfOpen(m.ChatId)
	return nil
}

// pinDuration is how long a pin lasts, from the message carrying it
// (7 days when it doesn't say).
func pinDuration(secs uint32) time.Duration {
	if secs == 0 {
		return DefaultPinDuration
	}
	return time.Duration(secs) * time.Second
}

// handlePin stores a pin or unpin someone (or your phone) made.
func (sm *SessionManager) handlePin(chat string, sender types.JID, fromMe bool, msg *waE2E.Message, at time.Time) {
	if sm.handlePinQuiet(chat, sender, fromMe, msg, at) {
		sm.refreshIfOpen(chat)
	}
}

// handlePinQuiet stores it without redrawing (history sync).
func (sm *SessionManager) handlePinQuiet(chat string, sender types.JID, fromMe bool, msg *waE2E.Message, at time.Time) bool {
	p := msg.GetPinInChatMessage()
	target := p.GetKey().GetID()
	if target == "" {
		return false
	}
	who := ""
	if !fromMe {
		who = sender.ToNonAD().String()
	}
	ts := p.GetSenderTimestampMS()
	if ts == 0 {
		ts = at.UnixMilli()
	}
	pinned := p.GetType() == waE2E.PinInChatMessage_PIN_FOR_ALL
	expires := time.UnixMilli(ts).Add(pinDuration(msg.GetMessageContextInfo().GetMessageAddOnDurationInSecs())).UnixMilli()
	if err := sm.db.SetPin(chat, target, who, pinned, ts, expires); err != nil {
		sm.debugf("store pin: %v", err)
		return false
	}
	return true
}
