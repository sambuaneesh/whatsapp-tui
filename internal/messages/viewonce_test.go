package messages

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func TestViewOnceText(t *testing.T) {
	for want, msg := range map[string]*waE2E.Message{
		"[VIEW ONCE] photo":         {ImageMessage: &waE2E.ImageMessage{}},
		"[VIEW ONCE] video":         {VideoMessage: &waE2E.VideoMessage{}},
		"[VIEW ONCE] voice message": {AudioMessage: &waE2E.AudioMessage{}},
		"[VIEW ONCE] message":       nil,
	} {
		if got := viewOnceText(msg); got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
}
