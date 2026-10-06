package api

import (
	"sort"
	"strings"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// The JSON shapes of the API (documented in docs/API.md). Times are unix
// seconds; chats and senders are WhatsApp JIDs ("91…@s.whatsapp.net",
// "…@g.us" for groups).

// ChatJSON is a chat in the list.
type ChatJSON struct {
	Chat      string `json:"chat"`
	Name      string `json:"name"`
	Group     bool   `json:"group"`
	Unread    int    `json:"unread"`
	Mentioned bool   `json:"mentioned"`
	Pinned    bool   `json:"pinned"`
	Archived  bool   `json:"archived"`
	LastTime  int64  `json:"last_time"`
	Preview   string `json:"preview"`
}

// MessageJSON is a message.
type MessageJSON struct {
	ID         string            `json:"id"`
	Chat       string            `json:"chat"`
	ChatName   string            `json:"chat_name"`
	Sender     string            `json:"sender"` // "" for you
	SenderName string            `json:"sender_name"`
	FromMe     bool              `json:"from_me"`
	Time       int64             `json:"time"`
	Text       string            `json:"text"`
	Media      string            `json:"media,omitempty"` // image, video, gif, sticker, audio, document
	ReplyTo    string            `json:"reply_to,omitempty"`
	Forwarded  bool              `json:"forwarded,omitempty"`
	Edited     bool              `json:"edited,omitempty"`
	Deleted    bool              `json:"deleted,omitempty"`
	Mentions   map[string]string `json:"mentions,omitempty"` // number -> name
	Reactions  []ReactionBrief   `json:"reactions,omitempty"`
	Similar    bool              `json:"similar,omitempty"` // search: found by meaning
}

// ReactionBrief is a reaction on a message.
type ReactionBrief struct {
	Sender string `json:"sender"` // "" for you
	Emoji  string `json:"emoji"`
}

// ReactionJSON is a reaction event.
type ReactionJSON struct {
	Chat       string `json:"chat"`
	ChatName   string `json:"chat_name"`
	MessageID  string `json:"message_id"`
	Sender     string `json:"sender"` // "" for you
	SenderName string `json:"sender_name,omitempty"`
	Emoji      string `json:"emoji"` // "" when taken back
	FromMe     bool   `json:"from_me"`
}

// ReminderJSON is a reminder event (snooze over, no reply yet).
type ReminderJSON struct {
	Chat     string `json:"chat"`
	ChatName string `json:"chat_name"`
	Text     string `json:"text"`
}

func chatJSON(c *messages.Conversation) ChatJSON {
	return ChatJSON{Chat: c.JID, Name: c.Name, Group: strings.HasSuffix(c.JID, "@g.us"), Unread: int(c.Unread),
		Mentioned: c.Mentioned, Pinned: c.IsPinned, Archived: c.IsArchived, LastTime: c.LastMsgTime, Preview: c.Preview}
}

// sortChats orders like the app: pinned first, then newest.
func sortChats(cs []ChatJSON) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Pinned != cs[j].Pinned {
			return cs[i].Pinned
		}
		return cs[i].LastTime > cs[j].LastTime
	})
}

func messageJSON(m messages.Message, chatName string) MessageJSON {
	out := MessageJSON{ID: m.Id, Chat: m.ChatId, ChatName: chatName, Sender: m.ContactId, SenderName: m.ContactShort,
		FromMe: m.FromMe, Time: int64(m.Timestamp), Text: m.Text, Media: m.MediaType, ReplyTo: m.QuotedID,
		Forwarded: m.Forwarded, Edited: m.Edited, Deleted: m.Deleted != 0, Mentions: m.Mentions}
	if m.FromMe {
		out.Sender, out.SenderName = "", "You"
	}
	if out.SenderName == "" {
		out.SenderName = m.ContactName
	}
	for _, r := range m.Reactions {
		out.Reactions = append(out.Reactions, ReactionBrief{Sender: r.Sender, Emoji: r.Emoji})
	}
	return out
}
