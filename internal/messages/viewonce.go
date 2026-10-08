package messages

import (
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
)

// View-once photos, videos and voice messages can only be opened on the
// phone: WhatsApp doesn't send them to linked devices (they arrive as an
// "unavailable" notice), and the rare one that does arrive isn't kept. So
// they're shown, as "[VIEW ONCE] photo", without their media.

// ViewOnceTag starts a view-once message's text.
const ViewOnceTag = "[VIEW ONCE]"

// viewOnceText is a view-once message's text: what it was, if known.
func viewOnceText(msg *waE2E.Message) string {
	kind := "message"
	switch {
	case msg.GetImageMessage() != nil:
		kind = "photo"
	case msg.GetVideoMessage() != nil:
		kind = "video"
	case msg.GetAudioMessage() != nil:
		kind = "voice message"
	}
	return ViewOnceTag + " " + kind
}

// handleViewOnceNotice stores the notice WhatsApp sends linked devices in
// place of a view-once message, so it shows in the chat.
func (eh *eventHandler) handleViewOnceNotice(v *events.UndecryptableMessage) {
	if !v.IsUnavailable || v.UnavailableType != events.UnavailableTypeViewOnce {
		return
	}
	text := viewOnceText(nil)
	eh.processIncomingMessage(&events.Message{Info: v.Info, Message: &waE2E.Message{}}, text, text)
}
