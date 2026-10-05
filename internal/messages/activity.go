package messages

import (
	"context"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// What happened to you across all chats: mentions, replies to your
// messages and reactions to them, newest first.

// Kinds of activity.
const (
	ActivityMention  = "mention"
	ActivityReply    = "reply"
	ActivityReaction = "reaction"
)

// ActivityItem is one thing that happened to you.
type ActivityItem struct {
	Kind     string
	ChatId   string
	ChatName string
	Msg      Message // the mention or reply; for a reaction, your message
	Who      string  // who reacted (reactions); otherwise Msg's sender
	Emoji    string  // reactions
	Time     int64   // unix seconds
}

// activityLimit caps each kind of activity.
const activityLimit = 200

// Activity lists your recent mentions, replies and reactions, newest first.
func (sm *SessionManager) Activity(ctx context.Context) ([]ActivityItem, error) {
	var me []string // your number and LID: how mentions name you
	if client := sm.getClient(); client != nil && client.Store.ID != nil {
		me = append(me, client.Store.ID.User)
		if !client.Store.LID.IsEmpty() {
			me = append(me, client.Store.LID.User)
		}
	}
	items, err := sm.db.Activity(me, activityLimit)
	if err != nil {
		return nil, err
	}
	for i := range items {
		it := &items[i]
		it.ChatName = sm.ChatName(ctx, it.ChatId)
		if it.Kind == ActivityReaction {
			if jid, err := types.ParseJID(it.Who); err == nil {
				it.Who = sm.contactName(ctx, jid)
			}
		}
	}
	return items, nil
}

// Activity reads the activity for an account named me (numbers / LIDs).
func (md *MessageDatabase) Activity(me []string, limit int) ([]ActivityItem, error) {
	var items []ActivityItem
	add := func(kind string, msgs []Message) {
		for _, m := range msgs {
			items = append(items, ActivityItem{Kind: kind, ChatId: m.ChatId, Msg: m, Who: m.ContactShort, Time: int64(m.Timestamp)})
		}
	}

	// replies to your messages
	rows, err := md.db.Query(`SELECT `+msgColumns+` FROM messages
		WHERE from_me = 0 AND quoted_id != '' AND quoted_id IN (SELECT id FROM messages WHERE from_me = 1)
		ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	replies, err := collectMessages(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	add(ActivityReply, replies)
	seen := map[string]bool{}
	for _, m := range replies {
		seen[m.Id] = true
	}

	// mentions of you (or of everyone)
	var conds []string
	var args []any
	for _, user := range me {
		conds = append(conds, `text LIKE ? ESCAPE '\'`)
		args = append(args, "%@"+escapeLike(user)+"%")
	}
	conds = append(conds, `text LIKE ? ESCAPE '\'`)
	args = append(args, "%@"+escapeLike(MentionAll)+"%")
	rows, err = md.db.Query(`SELECT `+msgColumns+` FROM messages WHERE from_me = 0 AND (`+strings.Join(conds, " OR ")+`)
		ORDER BY timestamp DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	mentions, err := collectMessages(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, m := range mentions {
		if !seen[m.Id] && mentionsAny(m.Text, append(me, MentionAll)) {
			add(ActivityMention, []Message{m})
		}
	}

	// reactions from others to your messages
	rows, err = md.db.Query(`SELECT msg_id, sender, emoji, timestamp FROM reactions
		WHERE sender != '' AND emoji != '' AND msg_id IN (SELECT id FROM messages WHERE from_me = 1)
		ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	type reaction struct {
		id, who, emoji string
		ts             int64
	}
	var reactions []reaction
	for rows.Next() {
		var r reaction
		if err := rows.Scan(&r.id, &r.who, &r.emoji, &r.ts); err != nil {
			rows.Close()
			return nil, err
		}
		reactions = append(reactions, r)
	}
	rows.Close()
	if len(reactions) > 0 {
		ids := make([]any, len(reactions))
		for i, r := range reactions {
			ids[i] = r.id
		}
		rows, err = md.db.Query(`SELECT `+msgColumns+` FROM messages WHERE id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, ids...)
		if err != nil {
			return nil, err
		}
		yours, err := collectMessages(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		byID := map[string]Message{}
		for _, m := range yours {
			byID[m.Id] = m
		}
		for _, r := range reactions {
			m, ok := byID[r.id]
			if !ok {
				continue
			}
			ts := r.ts
			if ts > 1e12 {
				ts /= 1000 // reaction times are in milliseconds
			}
			if ts == 0 {
				ts = int64(m.Timestamp)
			}
			items = append(items, ActivityItem{Kind: ActivityReaction, ChatId: m.ChatId, Msg: m, Who: r.who, Emoji: r.emoji, Time: ts})
		}
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].Time > items[j].Time })
	return items, nil
}

// mentionsAny reports "@user" in text for any of users, as a whole mention
// (@9198 doesn't match @91987).
func mentionsAny(text string, users []string) bool {
	for _, u := range users {
		for i := 0; ; {
			j := strings.Index(text[i:], "@"+u)
			if j < 0 {
				break
			}
			end := i + j + 1 + len(u)
			if end == len(text) || !isWordByte(text[end]) {
				return true
			}
			i = end
		}
	}
	return false
}

func isWordByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_'
}
