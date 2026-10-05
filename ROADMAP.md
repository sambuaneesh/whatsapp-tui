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
- [x] No renderer wake-ups while no window is attached (Bubble Tea lets go
      of the terminal): detached idle CPU 11 → 3 ticks/10 s in a test copy
      (the 3 left are its login QR countdown)
- [ ] Memory: 86–94 MB measured on real use; profile it
      (`WHATSAPP_TUI_PPROF=127.0.0.1:6060`) before tuning
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
- [x] Send later (`:later 9am`), run by the background app
- [x] Snooze a chat until a time (`:snooze`)
- [x] "Nudge me if no reply" (`:nudge 3h`); `:scheduled` lists and cancels
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

## Batch 2026-10-06 (chosen: 1–5, 9, 13, 14, 17, 20, + deleted messages)

- [x] 1. More time phrases: `in an hour`, `next friday`, `next week`,
      `weekend`, `eod`, dates (`12 oct 3pm`, `12/10`, `2026-10-12`)
- [ ] 2. Profile and cut the background app's memory
- [x] 3. Show polls (question + options), live location and events, and
      several-contact cards; locations get a map link
- [ ] 4. Animated stickers/GIFs striped in the sticker tray
- [x] Deleted-for-everyone messages keep their text and media, marked
      "deleted by <name>" (gone only with delete for me)
- [ ] 5. One feed of mentions, replies to you, reactions to your messages
- [ ] 9. Read without sending blue ticks; mark read when you choose
- [ ] 17. Bulk actions in visual mode
- [ ] 14. Split view: two chats side by side
- [ ] 13. Local API for scripts and hooks (documented)
- [ ] 20. Better search: full-text index + search by meaning

## Known bugs

- [x] Polls, live location and event messages were silently dropped
- [x] Forwarded messages weren't marked (also fixed for re-synced history)
- [ ] Animated stickers and GIFs render striped in the sticker tray (parked)

## Notes

- Some people report bans for using unofficial WhatsApp clients: anything
  that sends on its own must be opt-in, rate-limited and visible.
