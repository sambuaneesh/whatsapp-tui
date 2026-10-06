package messages

import (
	"context"
	"errors"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
)

// sendAppState sends a chat setting change (archive, pin, mute, read,
// delete) to WhatsApp. When our copy of that settings collection has
// drifted from the server's, WhatsApp answers 409 "conflict" and
// whatsmeow can fail to catch up from the patches it sends back; then
// the collection is fetched again in full and the change sent once more.
func (sm *SessionManager) sendAppState(ctx context.Context, client *whatsmeow.Client, patch appstate.PatchInfo) error {
	err := client.SendAppState(ctx, patch)
	if err == nil || !errors.Is(err, whatsmeow.ErrAppStateUpdate) {
		return err
	}
	sm.debugf("app state %s out of sync (%v): fetching it again in full", patch.Type, err)
	if ferr := client.FetchAppState(ctx, patch.Type, true, false); ferr != nil {
		sm.debugf("full app state %s fetch failed: %v", patch.Type, ferr)
		return err
	}
	return client.SendAppState(ctx, patch)
}
