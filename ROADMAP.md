# Roadmap

What we're building next, in order. Tick items off as they land, and add a
line under "Notes" when something changes the plan. Standard WhatsApp
features (typing indicators, polls, mute, …) are out of scope for now.

## 1. Efficiency pass

Goal: the lightest, most responsive messaging client there is. Measure with
`go test ./internal/ui -run XXX -bench . -benchmem` (400-message group chat,
180×50 window) and the background app's memory / idle CPU.

- [x] Benchmarks in the repo (`internal/ui/bench_test.go`)
- [x] Cache rendered message bubbles; rebuild only what changed
      (`internal/ui/rendercache.go`)
- [x] Own scrolling view that keeps rendered lines and touches only the
      visible ones (`internal/ui/lineview.go`)
- [x] Frames assembled line by line, not re-fitted by lipgloss
- [x] Bubble borders, alignment and reactions drawn by hand (output is byte
      for byte the same as before)
- [x] Style whole runs of text at once in the formatter
- [x] Group names from the local cache, not a server request per message;
      renames tracked
- [x] Serialise terminal writes (images vs. screen) so frames never interleave
- [x] Send kitty images as raw pixels at the size shown (no PNG encoding;
      checked against real kitty)
- [x] Bound the in-memory image cache (300) and delete evicted images, and
      replaced thumbnails, in kitty
- [x] Bound the on-disk media cache (`media_cache_mb`, default 1024; least
      recently used first)
- [x] SQLite: synchronous=NORMAL with WAL, one transaction per history sync,
      redundant index dropped, index for the sticker/GIF tray
- [x] Cursor stops blinking (and redrawing) while the window is unfocused or
      closed
- [~] Batch image-arrival redraws: not needed now, a redraw is 0.35 ms
- [ ] Measure the background app's memory and idle CPU on real use (needs
      the window reopened on the new build)
- [ ] Bubble Tea v2 (cell renderer, synchronized output): later, bigger change

Results (before → after):

| | Before | After |
|---|---|---|
| Redraw after any change | 66 ms, 15 MB | 0.35 ms, 0.14 MB |
| `j` in visual mode (with frame) | 69 ms, 16 MB | 0.84 ms, 0.43 MB |
| New message (with frame) | 70 ms, 16 MB | 0.65 ms, 0.36 MB |
| Receipts / reactions update | 69 ms, 16 MB | 0.78 ms, 0.51 MB |
| One frame | 2.06 ms | 0.26 ms |
| Scroll (with frame) | 2.06 ms | 0.29 ms |
| Opening a chat (cold) | 66 ms | 26 ms |

## 2. Things WhatsApp can't do (pick from these next)

- [x] Drafts kept per chat (survive switching chats, closing the window and
      restarts; "Draft:" in the chat list)
- [x] Triage keys: `e` done (read + archive, then next unread), `U` mark
      unread, `J` next unread chat; synced to the phone
- [ ] Send later (`:later 9am`), run by the background app
- [ ] Snooze a chat until a time
- [ ] "Nudge me if no reply in N hours"
- [ ] "Later" screen: starred, reminders, snoozed, follow-ups, with done
- [ ] One feed of mentions, replies to you and reactions to your messages
- [ ] Highlight words, per-chat notification levels, quiet hours
- [ ] Read without sending read receipts; mark read when you choose
- [ ] Plain-text chat logs and "download all media" for a chat
- [ ] Rule-based folders and saved searches (`from:… has:image after:…`)
- [ ] Local API on the background app for scripts and hooks
- [ ] Split view of two chats; multiple accounts

## 3. Local AI (all off by default, per chat)

- [ ] Voice-note transcription (whisper.cpp on the GPU; IndicConformer for
      Telugu), searchable
- [ ] Full-text + meaning search (SQLite FTS5 + sqlite-vec + EmbeddingGemma)
- [ ] "Catch me up" summaries for busy groups (local 4B model; optional
      Claude)
- [ ] Smart notifications: only what matters in muted groups
- [ ] Searchable photos: OCR (Tesseract) and captions (small vision model)
- [ ] MCP server in the background app (sending needs approval in the app)
- [ ] Todos/dates to reminders, translation, tone rewrite, scam warnings

## Known bugs

- [ ] Polls, live location and event messages are silently dropped
- [x] Forwarded messages weren't marked (also fixed for re-synced history)
- [ ] Animated stickers and GIFs render striped in the sticker tray (parked)

## Notes

- Some people report bans for using unofficial WhatsApp clients: anything
  that sends on its own must be opt-in, rate-limited and visible.
