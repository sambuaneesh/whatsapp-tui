package messages

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Receipts per person. In a group, one member reading your message used to
// turn it blue for everyone; now each member's receipts are kept, and the
// ticks show what's true of all of them, as on the phone: delivered (grey
// double) once everyone has it, read (blue) once everyone has read it.
// MessageReceipts lists who has seen what (Message info, i in visual mode).

func (md *MessageDatabase) initReceipts() error {
	_, err := md.db.Exec(`CREATE TABLE IF NOT EXISTS receipts (
		msg_id TEXT,
		user TEXT,
		delivered INTEGER NOT NULL DEFAULT 0, -- unix ms
		read INTEGER NOT NULL DEFAULT 0,
		played INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (msg_id, user)
	)`)
	if err != nil {
		return err
	}
	// group messages turned blue by the old rule ("anyone read it") are
	// only known to have arrived: grey until receipts say otherwise
	var done string
	md.db.QueryRow(`SELECT value FROM meta WHERE key = 'group_receipts'`).Scan(&done)
	if done != "1" {
		md.db.Exec(`UPDATE messages SET status = ? WHERE from_me = 1 AND chat_id LIKE '%@g.us' AND status > ?
			AND id NOT IN (SELECT msg_id FROM receipts)`, StatusDelivered, StatusDelivered)
		md.db.Exec(`INSERT INTO meta (key, value) VALUES ('group_receipts', '1')
			ON CONFLICT(key) DO UPDATE SET value = '1'`)
	}
	return nil
}

// Receipt is one person's receipts for a message (unix ms; 0 = not yet).
type Receipt struct {
	User      string
	Name      string
	Delivered int64
	Read      int64
	Played    int64
}

// AddReceipt records that user got (st: delivered, read or played) a
// message at ms. Earlier states are filled in: read means delivered.
func (md *MessageDatabase) AddReceipt(msgID, user string, st int, ms int64) error {
	var d, r, p int64
	switch st {
	case StatusDelivered:
		d = ms
	case StatusRead:
		d, r = ms, ms
	case StatusPlayed:
		d, r, p = ms, ms, ms
	default:
		return nil
	}
	_, err := md.db.Exec(`INSERT INTO receipts (msg_id, user, delivered, read, played) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(msg_id, user) DO UPDATE SET
			delivered = CASE WHEN receipts.delivered = 0 OR (excluded.delivered > 0 AND excluded.delivered < receipts.delivered) THEN excluded.delivered ELSE receipts.delivered END,
			read = CASE WHEN receipts.read = 0 OR (excluded.read > 0 AND excluded.read < receipts.read) THEN excluded.read ELSE receipts.read END,
			played = CASE WHEN receipts.played = 0 OR (excluded.played > 0 AND excluded.played < receipts.played) THEN excluded.played ELSE receipts.played END`,
		msgID, user, d, r, p)
	return err
}

// Receipts returns a message's receipts, by user.
func (md *MessageDatabase) Receipts(msgID string) (map[string]Receipt, error) {
	rows, err := md.db.Query(`SELECT user, delivered, read, played FROM receipts WHERE msg_id = ?`, msgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Receipt{}
	for rows.Next() {
		var r Receipt
		if err := rows.Scan(&r.User, &r.Delivered, &r.Read, &r.Played); err != nil {
			return nil, err
		}
		out[r.User] = r
	}
	return out, rows.Err()
}

// groupStatus is a message's status from its members' receipts: the
// furthest every one of them has got (Sent when someone has nothing yet).
func groupStatus(members []string, rs map[string]Receipt) int {
	if len(members) == 0 {
		return StatusSent
	}
	st := StatusPlayed
	for _, m := range members {
		r := rs[m]
		switch {
		case r.Played > 0:
		case r.Read > 0:
			st = min(st, StatusRead)
		case r.Delivered > 0:
			st = min(st, StatusDelivered)
		default:
			return StatusSent
		}
	}
	return st
}

// setStatusTo sets a message's status, up or (fixing a wrong one) down;
// failed and pending messages are left alone.
func (md *MessageDatabase) setStatusTo(id string, status int) (bool, error) {
	res, err := md.db.Exec(`UPDATE messages SET status = ? WHERE id = ? AND COALESCE(status, 0) >= ? AND COALESCE(status, 0) != ?`,
		status, id, StatusSent, status)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---------- group members ----------

// groupMembers caches who's in each group (yourself left out), so ticks
// can be worked out per receipt without asking WhatsApp each time.
type groupMembers struct {
	mu   sync.Mutex
	byID map[string]membersEntry
}

type membersEntry struct {
	users []string
	at    time.Time
}

const membersFresh = 30 * time.Minute

// members returns a group's members other than you (canonical: phone
// numbers where known), fetching them when not cached or stale.
func (sm *SessionManager) members(ctx context.Context, chat string) ([]string, error) {
	g := &sm.groupMembers
	g.mu.Lock()
	if e, ok := g.byID[chat]; ok && time.Since(e.at) < membersFresh {
		g.mu.Unlock()
		return e.users, nil
	}
	g.mu.Unlock()
	client := sm.getClient()
	if client == nil {
		return nil, fmt.Errorf("not connected")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, err
	}
	info, err := client.GetGroupInfo(ctx, jid)
	if err != nil {
		return nil, err
	}
	me, _ := sm.ownJID()
	meLID := types.EmptyJID
	if client.Store.LID.User != "" {
		meLID = client.Store.LID.ToNonAD()
	}
	var users []string
	for _, p := range info.Participants {
		who := p.JID.ToNonAD()
		if !p.PhoneNumber.IsEmpty() {
			who = p.PhoneNumber.ToNonAD()
		}
		who = sm.pnForLID(ctx, who)
		if who == me || who == meLID || p.JID.ToNonAD() == meLID {
			continue
		}
		users = append(users, who.String())
	}
	g.mu.Lock()
	if g.byID == nil {
		g.byID = map[string]membersEntry{}
	}
	g.byID[chat] = membersEntry{users: users, at: time.Now()}
	g.mu.Unlock()
	return users, nil
}

// forgetMembers drops a group's cached members (someone joined or left).
func (sm *SessionManager) forgetMembers(chat string) {
	g := &sm.groupMembers
	g.mu.Lock()
	delete(g.byID, chat)
	g.mu.Unlock()
}

// ---------- receipts arriving ----------

// receiptUser is who a receipt is from, as members() names them.
func (sm *SessionManager) receiptUser(ctx context.Context, jid types.JID) string {
	return sm.pnForLID(ctx, jid.ToNonAD()).String()
}

// handleGroupReceipt records a group member's receipt and works out the
// messages' ticks again (in the background: it may ask for the members).
func (sm *SessionManager) handleGroupReceipt(evt *events.Receipt, st int) {
	ctx := context.Background()
	user := sm.receiptUser(ctx, evt.Sender)
	ms := evt.Timestamp.UnixMilli()
	for _, id := range evt.MessageIDs {
		if err := sm.db.AddReceipt(id, user, st, ms); err != nil {
			sm.debugf("receipt for %s: %v", id, err)
		}
	}
	chat := evt.Chat.String()
	ids := append([]types.MessageID(nil), evt.MessageIDs...)
	go sm.updateGroupTicks(chat, ids)
}

// updateGroupTicks sets group messages' status from their receipts.
func (sm *SessionManager) updateGroupTicks(chat string, ids []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	members, err := sm.members(ctx, chat)
	if err != nil {
		// without the members, only "delivered" is safe to show
		sm.debugf("members of %s: %v", chat, err)
		for _, id := range ids {
			sm.db.AdvanceStatus(id, StatusDelivered)
		}
		sm.scheduleChatRefresh(chat)
		return
	}
	changed := false
	for _, id := range ids {
		rs, err := sm.db.Receipts(id)
		if err != nil {
			continue
		}
		if ok, _ := sm.db.setStatusTo(id, groupStatus(members, rs)); ok {
			changed = true
		}
	}
	if changed {
		sm.scheduleChatRefresh(chat)
	}
}

// storeHistoryReceipts keeps the per-person receipts history sync carries
// for your group messages.
func (sm *SessionManager) storeHistoryReceipts(chat, msgID string, receipts []historyReceipt) {
	ctx := context.Background()
	for _, r := range receipts {
		jid, err := types.ParseJID(r.user)
		if err != nil {
			continue
		}
		user := sm.receiptUser(ctx, jid)
		switch {
		case r.played > 0:
			sm.db.AddReceipt(msgID, user, StatusPlayed, r.played*1000)
		case r.read > 0:
			sm.db.AddReceipt(msgID, user, StatusRead, r.read*1000)
		case r.delivered > 0:
			sm.db.AddReceipt(msgID, user, StatusDelivered, r.delivered*1000)
		}
	}
}

type historyReceipt struct {
	user                    string
	delivered, read, played int64 // unix seconds
}

// ---------- who saw it ----------

// MessageInfo is who has a message of yours, and who's still to.
type MessageInfo struct {
	Read      []Receipt // read (or played), earliest first
	Delivered []Receipt // delivered, not read yet
	Waiting   []Receipt // not even delivered yet (groups)
	Group     bool
	NoRecord  bool // a group message from before receipts were kept
}

// MessageReceipts tells who has seen (or got) a message you sent. In a
// group it asks for the members it doesn't know yet.
func (sm *SessionManager) MessageReceipts(ctx context.Context, m Message) (MessageInfo, error) {
	var info MessageInfo
	if !m.FromMe {
		return info, fmt.Errorf("message info is for your own messages")
	}
	rs, err := sm.db.Receipts(m.Id)
	if err != nil {
		return info, err
	}
	info.Group = strings.HasSuffix(m.ChatId, GROUPSUFFIX)
	if info.Group && len(rs) == 0 && m.Status >= StatusDelivered {
		info.NoRecord = true // who read it wasn't recorded: don't guess
		return info, nil
	}
	users := map[string]bool{}
	for u := range rs {
		users[u] = true
	}
	if info.Group {
		if members, err := sm.members(ctx, m.ChatId); err == nil {
			for _, u := range members {
				users[u] = true
			}
		}
	} else {
		users[m.ChatId] = true
	}
	for u := range users {
		r := rs[u]
		r.User = u
		if jid, err := types.ParseJID(u); err == nil {
			r.Name = sm.contactName(ctx, jid)
		}
		switch {
		case r.Read > 0 || r.Played > 0:
			info.Read = append(info.Read, r)
		case r.Delivered > 0:
			info.Delivered = append(info.Delivered, r)
		default:
			info.Waiting = append(info.Waiting, r)
		}
	}
	if !info.Group && len(rs) == 0 {
		// one-to-one messages from before receipts were kept: the ticks
		// are all there is
		switch {
		case m.Status >= StatusRead:
			info.Read, info.Waiting = info.Waiting, nil
		case m.Status == StatusDelivered:
			info.Delivered, info.Waiting = info.Waiting, nil
		}
	}
	byTime := func(rs []Receipt, at func(Receipt) int64) {
		sort.SliceStable(rs, func(i, j int) bool {
			if at(rs[i]) != at(rs[j]) {
				return at(rs[i]) < at(rs[j])
			}
			return rs[i].Name < rs[j].Name
		})
	}
	byTime(info.Read, func(r Receipt) int64 { return max(r.Read, r.Played) })
	byTime(info.Delivered, func(r Receipt) int64 { return r.Delivered })
	byTime(info.Waiting, func(r Receipt) int64 { return 0 })
	return info, nil
}
