package messages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/Srindot/whatsapp-tui/internal/config"
)

// Media types stored in Message.MediaType.
const (
	MediaImage   = "image"
	MediaSticker = "sticker"
	MediaGIF     = "gif"
	MediaVideo   = "video"
	MediaDoc     = "document"
	MediaAudio   = "audio"
)

// extractMedia returns the media type and a marshalled waE2E.Message holding
// only the media part of msg, or ("", nil) when msg has no displayable media.
func extractMedia(msg *waE2E.Message) (string, []byte) {
	if msg == nil {
		return "", nil
	}
	var typ string
	var m *waE2E.Message
	switch {
	case msg.GetImageMessage() != nil:
		typ, m = MediaImage, &waE2E.Message{ImageMessage: msg.GetImageMessage()}
	case msg.GetStickerMessage() != nil:
		typ, m = MediaSticker, &waE2E.Message{StickerMessage: msg.GetStickerMessage()}
	case msg.GetVideoMessage() != nil:
		typ = MediaVideo
		if msg.GetVideoMessage().GetGifPlayback() {
			typ = MediaGIF
		}
		m = &waE2E.Message{VideoMessage: msg.GetVideoMessage()}
	case msg.GetDocumentMessage() != nil:
		typ, m = MediaDoc, &waE2E.Message{DocumentMessage: msg.GetDocumentMessage()}
	case msg.GetAudioMessage() != nil:
		typ, m = MediaAudio, &waE2E.Message{AudioMessage: msg.GetAudioMessage()}
	default:
		return "", nil
	}
	b, err := proto.Marshal(m)
	if err != nil {
		return "", nil
	}
	return typ, b
}

// MediaMeta describes a message's media for display.
type MediaMeta struct {
	Type          string
	Width, Height int    // original size in pixels, 0 if unknown
	Thumbnail     []byte // small embedded JPEG/PNG preview, may be empty
	Animated      bool
	Seconds       int // video/GIF duration
}

// MediaMeta decodes the stored media info. ok is false for messages without
// media (or with media stored before media support was added).
func (m Message) MediaMeta() (meta MediaMeta, ok bool) {
	if m.MediaType == "" || len(m.Media) == 0 {
		return MediaMeta{}, false
	}
	var pm waE2E.Message
	if err := proto.Unmarshal(m.Media, &pm); err != nil {
		return MediaMeta{}, false
	}
	meta.Type = m.MediaType
	switch {
	case pm.GetImageMessage() != nil:
		im := pm.GetImageMessage()
		meta.Width, meta.Height = int(im.GetWidth()), int(im.GetHeight())
		meta.Thumbnail = im.GetJPEGThumbnail()
	case pm.GetStickerMessage() != nil:
		st := pm.GetStickerMessage()
		meta.Width, meta.Height = int(st.GetWidth()), int(st.GetHeight())
		meta.Thumbnail = st.GetPngThumbnail()
		meta.Animated = st.GetIsAnimated()
	case pm.GetVideoMessage() != nil:
		v := pm.GetVideoMessage()
		meta.Width, meta.Height = int(v.GetWidth()), int(v.GetHeight())
		meta.Thumbnail = v.GetJPEGThumbnail()
		meta.Seconds = int(v.GetSeconds())
	default:
		return MediaMeta{}, false
	}
	return meta, true
}

// cacheDir returns (and creates) a subdirectory of the app cache.
func cacheDir(sub string) (string, error) {
	dir := filepath.Join(config.GetCacheDir(), sub)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	return dir, nil
}

// safeName turns an ID or JID into a file name.
func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, s)
}

// ErrNoMedia is returned by DownloadMedia for messages without stored media.
var ErrNoMedia = errors.New("no media stored for this message")

// DownloadMedia downloads (or returns the cached copy of) a message's media
// and returns the local file path. Safe to call from any goroutine.
func (sm *SessionManager) DownloadMedia(ctx context.Context, msgID string) (string, error) {
	dir, err := cacheDir("media")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeName(msgID))
	if _, err := os.Stat(path); err == nil {
		now := time.Now()
		_ = os.Chtimes(path, now, now) // recently used: pruned last
		return path, nil
	}
	defer sm.pruneMediaSoon()

	msg, err := sm.db.GetMessage(msgID)
	if err != nil {
		return "", fmt.Errorf("load message: %w", err)
	}
	if len(msg.Media) == 0 {
		return "", ErrNoMedia
	}
	var pm waE2E.Message
	if err := proto.Unmarshal(msg.Media, &pm); err != nil {
		return "", fmt.Errorf("decode media info: %w", err)
	}
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return "", errors.New("not connected")
	}

	dl, total := downloadable(&pm)
	state := &download{total: total}
	sm.downloads.Store(msgID, state) // waiting for a download slot
	defer sm.downloads.Delete(msgID)

	defer acquire(sm.mediaSem)()

	if dl == nil { // no file-backed download for this kind
		data, err := client.DownloadAny(ctx, &pm)
		if err != nil {
			return "", fmt.Errorf("download media: %w", err)
		}
		tmp := path + ".part"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return "", fmt.Errorf("save media: %w", err)
		}
		return path, os.Rename(tmp, path)
	}
	// stream into a file, which DownloadProgress watches grow
	f, err := os.CreateTemp(dir, safeName(msgID)+".*.part")
	if err != nil {
		return "", fmt.Errorf("save media: %w", err)
	}
	state.setFile(f.Name())
	err = client.DownloadToFile(ctx, dl, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("download media: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("save media: %w", err)
	}
	return path, nil
}

// download is a media download in progress.
type download struct {
	total int64 // expected size in bytes, 0 if unknown
	mu    sync.Mutex
	file  string // where it's being written; "" while waiting for a slot
}

func (d *download) setFile(f string) {
	d.mu.Lock()
	d.file = f
	d.mu.Unlock()
}

// DownloadProgress reports how far a message's media download has got: bytes
// done and the total (0 if unknown). ok is false when it isn't downloading;
// done is -1 while it waits for other downloads to finish.
func (sm *SessionManager) DownloadProgress(msgID string) (done, total int64, ok bool) {
	v, ok := sm.downloads.Load(msgID)
	if !ok {
		return 0, 0, false
	}
	d := v.(*download)
	d.mu.Lock()
	file := d.file
	d.mu.Unlock()
	if file == "" {
		return -1, d.total, true
	}
	if st, err := os.Stat(file); err == nil {
		done = st.Size()
	}
	if d.total > 0 && done > d.total {
		done = d.total // the encrypted file is a little bigger
	}
	return done, d.total, true
}

// downloadable picks the downloadable part of a stored media message and its
// size, or nil for kinds DownloadToFile doesn't take.
func downloadable(pm *waE2E.Message) (whatsmeow.DownloadableMessage, int64) {
	switch {
	case pm.GetImageMessage() != nil:
		return pm.GetImageMessage(), int64(pm.GetImageMessage().GetFileLength())
	case pm.GetVideoMessage() != nil:
		return pm.GetVideoMessage(), int64(pm.GetVideoMessage().GetFileLength())
	case pm.GetAudioMessage() != nil:
		return pm.GetAudioMessage(), int64(pm.GetAudioMessage().GetFileLength())
	case pm.GetDocumentMessage() != nil:
		return pm.GetDocumentMessage(), int64(pm.GetDocumentMessage().GetFileLength())
	case pm.GetStickerMessage() != nil:
		return pm.GetStickerMessage(), int64(pm.GetStickerMessage().GetFileLength())
	}
	return nil, 0
}

// acquire takes a slot of sem (a no-op for a nil sem) and returns its release.
func acquire(sem chan struct{}) func() {
	if sem == nil {
		return func() {}
	}
	sem <- struct{}{}
	return func() { <-sem }
}

// avatarTTL is how long a cached profile picture (or "no picture") is reused.
const avatarTTL = 3 * 24 * time.Hour

// ProfilePicture returns the local path of a chat's profile picture, fetching
// it when the cache is missing or stale. It returns "" with a nil error when
// the chat has no (visible) picture. Safe to call from any goroutine.
func (sm *SessionManager) ProfilePicture(ctx context.Context, jidStr string) (string, error) {
	dir, err := cacheDir("avatars")
	if err != nil {
		return "", err
	}
	base := filepath.Join(dir, safeName(jidStr))
	if st, err := os.Stat(base + ".jpg"); err == nil && time.Since(st.ModTime()) < avatarTTL {
		return base + ".jpg", nil
	}
	if st, err := os.Stat(base + ".none"); err == nil && time.Since(st.ModTime()) < avatarTTL {
		return "", nil
	}

	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return "", fmt.Errorf("invalid JID: %w", err)
	}
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return "", errors.New("not connected")
	}

	defer acquire(sm.avatarSem)()

	info, err := client.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true})
	if errors.Is(err, whatsmeow.ErrProfilePictureNotSet) || errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) ||
		(err == nil && (info == nil || info.URL == "")) {
		_ = os.WriteFile(base+".none", nil, 0o600)
		_ = os.Remove(base + ".jpg")
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("profile picture info: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download profile picture: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download profile picture: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", fmt.Errorf("download profile picture: %w", err)
	}
	if err := os.WriteFile(base+".jpg.part", data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(base+".jpg.part", base+".jpg"); err != nil {
		return "", err
	}
	_ = os.Remove(base + ".none")
	return base + ".jpg", nil
}

// requestHistory asks the phone for up to count messages older than anchor
// (the newest-known message is fine: WhatsApp then re-sends recent messages,
// which also fills in media info for rows stored without it). With a nil
// anchor it asks for the latest messages of chat. The answer arrives later as
// an ON_DEMAND history sync.
func (sm *SessionManager) requestHistory(chat string, anchor *Message, count int) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() || client.Store.ID == nil {
		return errors.New("not connected")
	}
	chatJID, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chatJID},
		Timestamp:     time.Now(),
	}
	if anchor != nil {
		info.ID = anchor.Id
		info.IsFromMe = anchor.FromMe
		info.Timestamp = time.Unix(int64(anchor.Timestamp), 0)
		if sender, err := types.ParseJID(anchor.ContactId); err == nil {
			info.Sender = sender
		}
	}
	req := client.BuildHistorySyncRequest(info, count)
	if _, err := client.SendPeerMessage(context.Background(), req); err != nil {
		sm.debugf("history request for %s failed: %v", chat, err)
		return fmt.Errorf("history request: %w", err)
	}
	sm.debugf("requested %d messages of %s before %q (%s)", count, chat, info.ID, info.Timestamp.Format(time.RFC3339))
	return nil
}

// autoBackfill requests recent history for a chat once per session, so
// opening a chat pulls messages (and media info) the initial sync missed.
func (sm *SessionManager) autoBackfill(chat string) {
	sm.mu.Lock()
	if sm.backfilled == nil {
		sm.backfilled = make(map[string]bool)
	}
	if sm.backfilled[chat] {
		sm.mu.Unlock()
		return
	}
	sm.backfilled[chat] = true
	sm.mu.Unlock()

	var anchor *Message
	if latest, err := sm.db.GetLatestMessages(chat, 1); err == nil && len(latest) == 1 {
		anchor = &latest[0]
	}
	if err := sm.requestHistory(chat, anchor, 50); err != nil {
		// Allow a retry on the next open, e.g. after reconnecting.
		sm.mu.Lock()
		delete(sm.backfilled, chat)
		sm.mu.Unlock()
	}
}

// MediaCacheLimit is how much downloaded media is kept (bytes); the least
// recently used files go first. Set from the config.
var MediaCacheLimit int64 = 1 << 30

// pruneEvery spaces out cache pruning (it walks the directory).
const pruneEvery = 10 * time.Minute

// pruneMediaSoon prunes the media cache in the background, at most once per
// pruneEvery.
func (sm *SessionManager) pruneMediaSoon() {
	sm.mu.Lock()
	due := time.Since(sm.lastPrune) >= pruneEvery
	if due {
		sm.lastPrune = time.Now()
	}
	sm.mu.Unlock()
	if due {
		go func() {
			if dir, err := cacheDir("media"); err == nil {
				if n, err := pruneDir(dir, MediaCacheLimit); err != nil || n > 0 {
					sm.debugf("media cache: removed %d files (%v)", n, err)
				}
			}
		}()
	}
}

// pruneDir deletes the least recently used files in dir until it holds at
// most 80% of limit bytes (it only starts when over the limit). Unfinished
// downloads are left alone.
func pruneDir(dir string, limit int64) (removed int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	type file struct {
		path string
		size int64
		used time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || strings.HasSuffix(e.Name(), ".part") {
			continue
		}
		files = append(files, file{filepath.Join(dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= limit {
		return 0, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].used.Before(files[j].used) })
	for _, f := range files {
		if total <= limit*8/10 {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
			removed++
		}
	}
	return removed, nil
}
