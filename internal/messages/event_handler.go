package messages

// event_handler.go — extracted from session_manager.go (audit A2-A3).
// Contains the WhatsApp event handler, message extraction, and contact
// name resolution logic.

import (
	"container/heap"
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type eventHandler struct {
	sm *SessionManager
}

func (eh *eventHandler) Handle(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		eh.handleMessage(v)
	case *events.UndecryptableMessage:
		if v.Info.Chat.Server != types.BroadcastServer {
			eh.sm.canonicalSource(context.Background(), &v.Info.MessageSource)
			eh.handleViewOnceNotice(v)
		}
	case *events.Connected:
		eh.sm.mu.Lock()
		eh.sm.reconnecting = false
		eh.sm.loggedOut = false
		eh.sm.mu.Unlock()
		eh.sm.StatusChannel <- StatusMsg{true, nil}
		go eh.sm.syncContacts()
	case *events.Contact, *events.PushName:
		eh.sm.scheduleNameRefresh()
	case *events.Receipt:
		eh.sm.handleReceipt(v)
	case *events.DeleteForMe:
		eh.sm.handleDeleteForMe(v)
	case *events.DeleteChat:
		eh.sm.handleDeleteChat(v)
	case *events.Archive:
		if v.FromFullSync {
			eh.sm.noteFullSync("archive", v.JID)
		}
		archived := v.Action.GetArchived()
		eh.sm.setChatFlags(v.JID, &archived, nil)
	case *events.Pin:
		if v.FromFullSync {
			eh.sm.noteFullSync("pin", v.JID)
		}
		pinned := v.Action.GetPinned()
		eh.sm.setChatFlags(v.JID, nil, &pinned)
	case *events.Mute:
		if v.FromFullSync {
			eh.sm.noteFullSync("mute", v.JID)
		}
		eh.sm.setChatMute(v.JID, v.Action.GetMuted(), v.Action.GetMuteEndTimestamp())
	case *events.GroupInfo:
		if len(v.Join) > 0 || len(v.Leave) > 0 {
			eh.sm.forgetMembers(eh.sm.pnForLID(context.Background(), v.JID).String())
			eh.sm.forgetMembers(v.JID.String())
		}
		if v.Name != nil {
			eh.sm.renameChat(v.JID, v.Name.Name)
		}
	case *events.MarkChatAsRead:
		// read (or marked unread) on your phone or another device
		eh.sm.setChatRead(v.JID, v.Action.GetRead())
	case *events.Disconnected:
		eh.sm.StatusChannel <- StatusMsg{false, nil}
		// Attempt auto-reconnect unless logged out or already reconnecting
		eh.sm.mu.Lock()
		shouldReconnect := !eh.sm.loggedOut && !eh.sm.reconnecting && eh.sm.started
		if shouldReconnect {
			eh.sm.reconnecting = true
		}
		eh.sm.mu.Unlock()
		if shouldReconnect {
			go eh.sm.scheduleReconnect()
		}
	case *events.LoggedOut:
		eh.sm.mu.Lock()
		eh.sm.loggedOut = true
		eh.sm.mu.Unlock()
		eh.sm.StatusChannel <- StatusMsg{false, nil}
		reasonText := fmt.Sprintf("%v", v.Reason)
		eh.sm.uiHandler.PrintText("Logged out: " + reasonText)
	case *events.HistorySync:
		go eh.sm.processHistorySync(v.Data)
	}
}

// mapLink is a map address for a location, which the UI can open.
func mapLink(lat, long float64) string {
	return fmt.Sprintf("https://maps.google.com/?q=%.6f,%.6f", lat, long)
}

// pollOf returns a message's poll, whichever version it came as.
func pollOf(msg *waE2E.Message) *waE2E.PollCreationMessage {
	for _, p := range []*waE2E.PollCreationMessage{msg.GetPollCreationMessage(), msg.GetPollCreationMessageV2(),
		msg.GetPollCreationMessageV3(), msg.GetPollCreationMessageV5(), msg.GetPollCreationMessageV6()} {
		if p != nil {
			return p
		}
	}
	if v4 := msg.GetPollCreationMessageV4(); v4 != nil && v4.GetMessage() != nil {
		return pollOf(v4.GetMessage()) // wrapped
	}
	return nil
}

// extractMessageContent extracts display text and chat-list preview from a
// whatsmeow Message proto. This is a pure function with no side effects,
// making it easy to test.
func extractMessageContent(msg *waE2E.Message) (text, preview string) {
	if msg == nil {
		return "", ""
	}

	// 1. Extended text (replies, link previews, formatted text)
	if ext := msg.GetExtendedTextMessage(); ext != nil {
		t := ext.GetText()
		if t != "" {
			return t, truncatePreview(t)
		}
	}

	// 2. Image
	if img := msg.GetImageMessage(); img != nil {
		c := img.GetCaption()
		if c != "" {
			return "[IMAGE] " + c, "[IMAGE] " + c
		}
		return "[IMAGE]", "[IMAGE]"
	}

	// 3. Video / GIF
	if vid := msg.GetVideoMessage(); vid != nil {
		tag := "[VIDEO]"
		if vid.GetGifPlayback() {
			tag = "[GIF]"
		}
		c := vid.GetCaption()
		if c != "" {
			return tag + " " + c, tag + " " + c
		}
		return tag, tag
	}

	// 4. Audio / Voice note
	if aud := msg.GetAudioMessage(); aud != nil {
		if aud.GetPTT() {
			secs := aud.GetSeconds()
			if secs > 0 {
				t := fmt.Sprintf("[VOICE NOTE] %ds", secs)
				return t, t
			}
			return "[VOICE NOTE]", "[VOICE NOTE]"
		}
		secs := aud.GetSeconds()
		if secs > 0 {
			t := fmt.Sprintf("[AUDIO] %ds", secs)
			return t, t
		}
		return "[AUDIO]", "[AUDIO]"
	}

	// 5. Document
	if doc := msg.GetDocumentMessage(); doc != nil {
		name := doc.GetFileName()
		if name == "" {
			name = doc.GetTitle()
		}
		if name != "" {
			return "[DOCUMENT] " + name, "[DOCUMENT] " + name
		}
		return "[DOCUMENT]", "[DOCUMENT]"
	}

	// 6. Sticker
	if msg.GetStickerMessage() != nil {
		return "[STICKER]", "[STICKER]"
	}

	// 7. Contact
	if con := msg.GetContactMessage(); con != nil {
		name := con.GetDisplayName()
		if name != "" {
			return "[CONTACT] " + name, "[CONTACT] " + name
		}
		return "[CONTACT]", "[CONTACT]"
	}

	// 8. Location (with a map link to click)
	if loc := msg.GetLocationMessage(); loc != nil {
		name := loc.GetName()
		if name == "" {
			name = loc.GetAddress()
		}
		link := mapLink(loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
		if name != "" {
			return "[LOCATION] " + name + "\n" + link, "[LOCATION] " + name
		}
		return "[LOCATION] " + link, "[LOCATION]"
	}
	if live := msg.GetLiveLocationMessage(); live != nil {
		t := strings.TrimSpace("[LIVE LOCATION] " + live.GetCaption())
		return t + "\n" + mapLink(live.GetDegreesLatitude(), live.GetDegreesLongitude()), t
	}

	// Polls: the question and options (votes are made on the phone)
	if poll := pollOf(msg); poll != nil {
		var b strings.Builder
		b.WriteString("[POLL] " + poll.GetName())
		for _, o := range poll.GetOptions() {
			b.WriteString("\n○ " + o.GetOptionName())
		}
		if n := poll.GetSelectableOptionsCount(); n > 1 {
			fmt.Fprintf(&b, "\n(pick up to %d)", n)
		}
		return b.String(), truncatePreview("[POLL] " + poll.GetName())
	}

	// Events: name, when, where, what
	if ev := msg.GetEventMessage(); ev != nil {
		head := "[EVENT] " + ev.GetName()
		if ev.GetIsCanceled() {
			head += " (cancelled)"
		}
		var b strings.Builder
		b.WriteString(head)
		var when []string
		if st := ev.GetStartTime(); st > 0 {
			when = append(when, time.Unix(st, 0).Format("Mon 2 Jan 15:04"))
		}
		if loc := ev.GetLocation(); loc != nil && (loc.GetName() != "" || loc.GetAddress() != "") {
			when = append(when, strings.TrimSpace(loc.GetName()+" "+loc.GetAddress()))
		}
		if len(when) > 0 {
			b.WriteString("\n🗓 " + strings.Join(when, " · "))
		}
		if d := ev.GetDescription(); d != "" {
			b.WriteString("\n" + d)
		}
		if l := ev.GetJoinLink(); l != "" {
			b.WriteString("\n" + l)
		}
		return b.String(), truncatePreview(head)
	}

	// Several contacts at once
	if cs := msg.GetContactsArrayMessage(); cs != nil {
		var names []string
		for _, c := range cs.GetContacts() {
			names = append(names, c.GetDisplayName())
		}
		t := "[CONTACTS] " + strings.Join(names, ", ")
		return t, truncatePreview(t)
	}

	// 9. Reaction
	if react := msg.GetReactionMessage(); react != nil {
		emoji := react.GetText()
		if emoji != "" {
			return "[REACTION] " + emoji, "[REACTION] " + emoji
		}
		return "[REACTION]", "[REACTION]"
	}

	// 10. Plain text (fallback — must come last)
	if t := msg.GetConversation(); t != "" {
		return t, truncatePreview(t)
	}

	return "", ""
}

// truncatePreview shortens text for the chat list preview column.
func truncatePreview(s string) string {
	const maxLen = 80
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "…"
}

// processIncomingMessage handles all common steps for any received message:
// build Message struct → persist to DB → update PQ → UI update → notification.
func (eh *eventHandler) processIncomingMessage(evt *events.Message, text, preview string) {
	if text == "" {
		return // nothing to process
	}

	chatJID := evt.Info.Chat.String()
	timestamp := uint64(evt.Info.Timestamp.Unix())

	mediaType, media := extractMedia(evt.Message)
	msg := Message{
		Id:           evt.Info.ID,
		ChatId:       chatJID,
		FromMe:       evt.Info.IsFromMe,
		Timestamp:    timestamp,
		Text:         text,
		ContactId:    evt.Info.Sender.String(),
		ContactName:  eh.getContactName(evt.Info.Sender),
		ContactShort: eh.getContactShort(evt.Info.Sender),
		MediaType:    mediaType,
		Media:        media,
	}
	quoteOf(&msg, evt.Message)
	if evt.Info.IsFromMe {
		msg.Status = StatusSent // sent from your phone
	}

	// Persist message
	if err := eh.sm.db.AddMessage(msg); err != nil {
		eh.sm.uiHandler.PrintError(fmt.Errorf("failed to save message: %v", err))
	}

	// Resolve chat name BEFORE acquiring the write lock. getChatName()
	// internally calls getClient() which acquires mu.RLock — calling it
	// under mu.Lock would deadlock.
	chatName := eh.sm.getChatName(evt.Info.Chat)
	mentionsMe := !evt.Info.IsFromMe && eh.sm.mentionsMe(evt.Message) // same: takes mu

	// Update priority queue and conversation
	eh.sm.mu.Lock()
	conv := eh.sm.convByJID[chatJID]
	var toUpsert Conversation
	if conv != nil {
		conv.LastMsgTime = int64(timestamp)
		conv.Preview = preview
		if evt.Info.IsFromMe {
			// writing in a chat (e.g. from your phone) means you've read it,
			// as on the phone
			conv.Unread, conv.Mentioned = 0, false
		} else {
			conv.Unread++
		}
		conv.Mentioned = conv.Mentioned || mentionsMe
		eh.sm.priorityQueue.Update(conv, conv.LastMsgTime, conv.IsPinned)
		toUpsert = *conv
	} else {
		unread := uint16(0)
		if !evt.Info.IsFromMe {
			unread = 1
		}
		newConv := &Conversation{
			JID:         chatJID,
			Name:        chatName,
			LastMsgTime: int64(timestamp),
			Preview:     preview,
			Unread:      unread,
			IsPinned:    false,
			Mentioned:   mentionsMe,
		}
		heap.Push(&eh.sm.priorityQueue, newConv)
		eh.sm.convByJID[chatJID] = newConv
		toUpsert = *newConv
	}
	safeList := eh.sm.snapshotPQ()
	eh.sm.mu.Unlock()

	// DB write outside lock
	if err := eh.sm.db.UpsertConversation(toUpsert); err != nil {
		eh.sm.uiHandler.PrintError(fmt.Errorf("upsert conversation: %v", err))
	}

	// UI: show in current chat or send notification
	eh.sm.mu.RLock()
	isCurrent := chatJID == eh.sm.currentReceiver
	eh.sm.mu.RUnlock()

	one := []Message{msg}
	eh.sm.resolveMentions(one)
	if isCurrent {
		eh.sm.uiHandler.NewMessage(one[0])
	}
	// the UI decides whether (and how) to notify
	if !evt.Info.IsFromMe {
		eh.sm.uiHandler.Incoming(one[0], chatName)
	}
	eh.sm.emit(Event{Kind: EventMessage, Message: one[0], MentionsYou: mentionsMe})
	eh.sm.indexMeaningSoon()

	// Update chat list ordering
	eh.sm.uiHandler.UpdateChatList(safeList)
}

// Handle incoming messages — dispatches to extractMessageContent then processIncomingMessage.
func (eh *eventHandler) handleMessage(evt *events.Message) {
	if evt.Info.Chat.Server == types.BroadcastServer {
		return // status updates and broadcast lists aren't chats (history sync skips them too)
	}
	eh.sm.canonicalSource(context.Background(), &evt.Info.MessageSource)
	if r := evt.Message.GetReactionMessage(); r != nil {
		eh.sm.handleReaction(evt.Info.Chat.String(), evt.Info.Sender, evt.Info.IsFromMe, r)
		eh.reactedToYou(evt, r)
		sender := ""
		if !evt.Info.IsFromMe {
			sender = evt.Info.Sender.ToNonAD().String()
		}
		eh.sm.emit(Event{Kind: EventReaction, Reaction: ReactionEvent{Chat: evt.Info.Chat.String(),
			MessageID: r.GetKey().GetID(), Sender: sender, Emoji: r.GetText(), FromMe: evt.Info.IsFromMe}})
		return
	}
	if evt.Message.GetPinInChatMessage() != nil {
		eh.sm.handlePin(evt.Info.Chat.String(), evt.Info.Sender, evt.Info.IsFromMe, evt.Message, evt.Info.Timestamp)
		return
	}
	if pm := evt.Message.GetProtocolMessage(); pm != nil && pm.GetType() == waE2E.ProtocolMessage_REVOKE {
		eh.sm.handleRevoke(evt.Info.Chat.String(), pm, evt.Info.IsFromMe)
		return
	}
	if id, text, ok := editOf(evt.Message); ok {
		eh.sm.handleEdit(evt.Info.Chat.String(), id, text)
		return
	}
	if evt.IsViewOnce {
		// shown, without its media (it opens only on the phone)
		text := viewOnceText(evt.Message)
		stripped := *evt
		stripped.Message = &waE2E.Message{}
		eh.processIncomingMessage(&stripped, text, text)
		return
	}
	text, preview := extractMessageContent(evt.Message)
	eh.processIncomingMessage(evt, text, preview)
}

// reactedToYou reports a reaction to one of your messages for a
// notification: Text is "[REACTION] <emoji>", QuotedText what you wrote.
func (eh *eventHandler) reactedToYou(evt *events.Message, r *waE2E.ReactionMessage) {
	if evt.Info.IsFromMe || r.GetText() == "" { // yours, or one taken back
		return
	}
	target, err := eh.sm.db.GetMessage(r.GetKey().GetID())
	if err != nil || !target.FromMe {
		return
	}
	eh.sm.uiHandler.Incoming(Message{
		Id:           evt.Info.ID,
		ChatId:       evt.Info.Chat.String(),
		ContactId:    evt.Info.Sender.String(),
		ContactShort: eh.getContactShort(evt.Info.Sender),
		Timestamp:    uint64(evt.Info.Timestamp.Unix()),
		Text:         "[REACTION] " + r.GetText(),
		QuotedID:     target.Id,
		QuotedText:   target.Text,
	}, eh.sm.getChatName(evt.Info.Chat))
}

// Helper to get contact name
func (eh *eventHandler) getContactName(jid types.JID) string {
	return eh.sm.contactName(context.Background(), jid)
}

// getContactShort is the sender name shown on messages; the same as the
// contact name (saved name, "~ push name" or number).
func (eh *eventHandler) getContactShort(jid types.JID) string {
	return eh.getContactName(jid)
}
