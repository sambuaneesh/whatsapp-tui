package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Things the background app does later, since WhatsApp can't: send a
// message at a set time, bring back a snoozed chat, remind you when someone
// hasn't replied.

// Kinds of scheduled items.
const (
	ScheduleSend   = "send"   // send Text to Chat
	ScheduleSnooze = "snooze" // the chat was archived; bring it back, unread
	ScheduleNudge  = "nudge"  // remind you if Chat hasn't replied since Created
)

// Scheduled is one thing to do later.
type Scheduled struct {
	ID       int64
	Kind     string
	Chat     string
	Text     string   // the message (send)
	Mentions []string // its mentions (send)
	Due      time.Time
	Created  time.Time
}

// retryOffline is how soon a send due while disconnected is tried again.
const retryOffline = 30 * time.Second

// Schedule stores an item and wakes the scheduler. A snooze archives the
// chat now.
func (sm *SessionManager) Schedule(ctx context.Context, it Scheduled) (Scheduled, error) {
	switch it.Kind {
	case ScheduleSend:
		if strings.TrimSpace(it.Text) == "" {
			return it, errors.New("nothing to send")
		}
	case ScheduleSnooze:
		if err := sm.ArchiveChat(ctx, it.Chat, true); err != nil {
			return it, err
		}
	case ScheduleNudge:
	default:
		return it, fmt.Errorf("unknown kind %q", it.Kind)
	}
	if it.Created.IsZero() {
		it.Created = time.Now()
	}
	id, err := sm.db.AddScheduled(it)
	if err != nil {
		return it, err
	}
	it.ID = id
	sm.scheduleChanged()
	return it, nil
}

// Unschedule cancels an item; a cancelled snooze brings the chat back now.
func (sm *SessionManager) Unschedule(ctx context.Context, id int64) error {
	items, err := sm.db.ScheduledItems()
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.ID != id {
			continue
		}
		if err := sm.db.DeleteScheduled(id); err != nil {
			return err
		}
		if it.Kind == ScheduleSnooze {
			if err := sm.ArchiveChat(ctx, it.Chat, false); err != nil {
				sm.debugf("unsnooze %s: %v", it.Chat, err)
			}
		}
		sm.scheduleChanged()
		return nil
	}
	return errors.New("already done or cancelled")
}

// ScheduledItems lists what's pending, soonest first.
func (sm *SessionManager) ScheduledItems() []Scheduled {
	items, err := sm.db.ScheduledItems()
	if err != nil {
		sm.debugf("scheduled: %v", err)
	}
	return items
}

// scheduleChanged tells the UI and wakes the scheduler.
func (sm *SessionManager) scheduleChanged() {
	sm.uiHandler.ScheduledChanged(sm.ScheduledItems())
	select {
	case sm.schedWake <- struct{}{}:
	default:
	}
}

// runScheduler sleeps until the next item is due, does it, and repeats. It
// only wakes for due items or changes (no polling).
func (sm *SessionManager) runScheduler(stop <-chan struct{}) {
	for {
		wait := 24 * time.Hour
		if items := sm.ScheduledItems(); len(items) > 0 {
			wait = max(time.Until(items[0].Due), 0)
		}
		timer := time.NewTimer(wait)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-sm.schedWake:
			timer.Stop()
			continue
		case <-timer.C:
		}
		sm.runDue()
	}
}

// runDue does every item that's due.
func (sm *SessionManager) runDue() {
	now := time.Now()
	changed := false
	for _, it := range sm.ScheduledItems() {
		if it.Due.After(now) {
			break
		}
		retry, err := sm.runScheduled(it)
		switch {
		case retry:
			// not connected: try again shortly
			_ = sm.db.RescheduleScheduled(it.ID, now.Add(retryOffline))
		case err != nil:
			sm.uiHandler.PrintError(fmt.Errorf("scheduled %s to %s: %w", it.Kind, sm.ChatName(context.Background(), it.Chat), err))
			fallthrough
		default:
			_ = sm.db.DeleteScheduled(it.ID)
		}
		changed = true
	}
	if changed {
		sm.uiHandler.ScheduledChanged(sm.ScheduledItems())
	}
}

// runScheduled does one item. retry means "not now, try again shortly".
func (sm *SessionManager) runScheduled(it Scheduled) (retry bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client := sm.getClient()
	online := client != nil && client.IsConnected()
	name := sm.ChatName(ctx, it.Chat)
	switch it.Kind {
	case ScheduleSend:
		if !online {
			return true, nil
		}
		return false, sm.SendText(ctx, it.Chat, it.Text, it.Mentions)
	case ScheduleSnooze:
		if !online {
			return true, nil
		}
		if err := sm.ArchiveChat(ctx, it.Chat, false); err != nil {
			return false, err
		}
		if err := sm.MarkChatUnread(ctx, it.Chat, true); err != nil {
			return false, err
		}
		sm.remind(it.Chat, "⏰ Back from snooze: "+name)
	case ScheduleNudge:
		if sm.repliedSince(it.Chat, it.Created) {
			return false, nil // they did: nothing to remind
		}
		if online {
			_ = sm.MarkChatUnread(ctx, it.Chat, true)
		}
		sm.remind(it.Chat, "⏰ No reply from "+name+" since "+it.Created.Format("Mon 15:04"))
	}
	return false, nil
}

// repliedSince reports a message from someone else in chat after t.
func (sm *SessionManager) repliedSince(chat string, t time.Time) bool {
	msgs, err := sm.db.GetLatestMessages(chat, 50)
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if !m.FromMe && int64(m.Timestamp) > t.Unix() {
			return true
		}
	}
	return false
}

// remind notifies you, like a message (following the notification mode).
func (sm *SessionManager) remind(chat, text string) {
	sm.uiHandler.Incoming(Message{
		Id: fmt.Sprintf("reminder-%d", time.Now().UnixNano()), ChatId: chat,
		Timestamp: uint64(time.Now().Unix()), Text: "[REMINDER] " + text,
	}, sm.ChatName(context.Background(), chat))
}

// ---------- storage ----------

func (md *MessageDatabase) initScheduled() error {
	_, err := md.db.Exec(`CREATE TABLE IF NOT EXISTS scheduled (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT, chat TEXT, text TEXT, mentions TEXT,
		due INTEGER, created INTEGER) -- unix milliseconds`)
	return err
}

// AddScheduled stores an item and returns its ID.
func (md *MessageDatabase) AddScheduled(it Scheduled) (int64, error) {
	mentions, _ := json.Marshal(it.Mentions)
	res, err := md.db.Exec(`INSERT INTO scheduled (kind, chat, text, mentions, due, created) VALUES (?, ?, ?, ?, ?, ?)`,
		it.Kind, it.Chat, it.Text, string(mentions), it.Due.UnixMilli(), it.Created.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ScheduledItems lists the pending items, soonest first.
func (md *MessageDatabase) ScheduledItems() ([]Scheduled, error) {
	rows, err := md.db.Query(`SELECT id, kind, chat, text, mentions, due, created FROM scheduled ORDER BY due, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Scheduled
	for rows.Next() {
		var it Scheduled
		var mentions string
		var due, created int64
		if err := rows.Scan(&it.ID, &it.Kind, &it.Chat, &it.Text, &mentions, &due, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(mentions), &it.Mentions)
		it.Due, it.Created = time.UnixMilli(due), time.UnixMilli(created)
		out = append(out, it)
	}
	return out, rows.Err()
}

// DeleteScheduled removes an item.
func (md *MessageDatabase) DeleteScheduled(id int64) error {
	_, err := md.db.Exec(`DELETE FROM scheduled WHERE id = ?`, id)
	return err
}

// RescheduleScheduled moves an item to a new time.
func (md *MessageDatabase) RescheduleScheduled(id int64, due time.Time) error {
	_, err := md.db.Exec(`UPDATE scheduled SET due = ? WHERE id = ?`, due.UnixMilli(), id)
	return err
}
