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
- [x] Memory: profiled (see batch item 2 below)
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
- [x] One feed of mentions, replies to you and reactions to your messages
- [ ] Highlight words, per-chat notification levels, quiet hours
- [x] Read without sending read receipts; mark read when you choose
- [ ] Plain-text chat logs and "download all media" for a chat
- [ ] Rule-based folders and saved searches (`from:… has:image after:…`)
- [x] Local API on the background app for scripts and hooks
- [x] Split view of two chats (`v`, `W`, `:only`)
- [ ] Multiple WhatsApp accounts in one app
- [ ] Undo send: a few seconds' grace before a message goes out
- [ ] Message templates / snippets
- [ ] Natural-language times through the local model ("saturday morning
      before the match"), shown for confirmation

## 3. Local AI (all off by default, per chat)

- [ ] Voice-note transcription (whisper.cpp on the GPU; IndicConformer for
      Telugu), searchable
- [x] Full-text + meaning search (SQLite FTS5 + EmbeddingGemma via Ollama)
- [ ] "Catch me up" summaries for busy groups (local 4B model; optional
      Claude)
- [ ] Smart notifications: only what matters in muted groups
- [ ] Searchable photos: OCR (Tesseract) and captions (small vision model)
- [ ] MCP server in the background app (sending needs approval in the app)
- [ ] Todos/dates to reminders, translation, tone rewrite, scam warnings

## Batch 2026-10-06 (chosen: 1–5, 9, 13, 14, 17, 20, + deleted messages)

- [x] 1. More time phrases: `in an hour`, `next friday`, `next week`,
      `weekend`, `eod`, dates (`12 oct 3pm`, `12/10`, `2026-10-12`)
- [x] 2. Memory: profiled with a copy of the real data (no login): the
      app's own heap is 5.4 MB, the process 38 MB; the rest of the real
      94 MB is the logged-in WhatsApp session and history-sync bursts. Now
      returns sync memory to the system and collects garbage sooner
      (GOGC 50). Re-measure on the live app after the next restart.
- [x] 3. Show polls (question + options), live location and events, and
      several-contact cards; locations get a map link
- [x] 4. Animated stickers/GIFs striped: rendered 12 real animated
      stickers concurrently in kitty with the current image path (raw
      pixels at display size, serialised writes): no stripes. Confirm in
      the real tray (`s`).
- [x] Deleted-for-everyone messages keep their text and media, marked
      "deleted by <name>" (gone only with delete for me)
- [x] 5. Activity feed (`I`, `:activity`): mentions, replies to you,
      reactions to your messages, across all chats
- [x] 9. Private reading (`:private`, 🙈 badge): no read receipts until you
      mark a chat read (`U`, `:read`)
- [x] 17. Bulk actions in visual mode: `V` range, then copy as transcript,
      forward all, delete all, save all media
- [x] 14. Split view: `v` in the list opens a chat beside the open one
      (live), `W` swaps, `:only` closes
- [x] 13. Local API (`whatsapp-tui api …`, JSON-lines socket) and hooks
      (`~/.config/whatsapp-tui/hooks/on-*`), documented in docs/API.md
- [x] 20a. Full-text search (SQLite FTS5, built with `-tags sqlite_fts5`):
      all words, any order, prefixes, accents ignored; 0.3 ms vs 6.7 ms;
      index +0.7 MB; safe fallback without FTS5
- [x] 20b. Search by meaning: local EmbeddingGemma via Ollama, 256-dim
      int8 vectors (~12 MB for 50k messages), indexed in the background;
      `S` adds "≈ similar in meaning" hits. Inactive until Ollama is
      installed (`ollama pull embeddinggemma`).

## Palette, like VS Code (2026-10-06)

- [x] `ctrl+p` Quick Open: every chat and contact, fuzzy (fzf-style
      scoring, matched letters lit), recent chats first, `ctrl+p enter`
      back to the previous chat, `alt+enter` opens beside
- [x] `F1` / `ctrl+shift+p` Command Palette: every feature by name, only
      those that work where you are, recently used first; `>` and `#`
      (search messages) inside Quick Open
- [x] VS Code search keys: `ctrl+f` find in chat, `F3`/`shift+F3`,
      `ctrl+shift+f` all chats (kitty maps for the shift ones)
- [x] Instant chat open: the highlighted chat preloads, the last 24
      chats stay loaded; `/` filter's `enter` opens at once
- [x] Chat list with a real account (2,045 chats, 6,126 contacts): filter
      keystroke 17 ms → 0.8 ms, list frame 2.1 → 0.5 ms; palette
      keystroke over all 8k names 1.5 ms, open 1 ms

## Pins, mutes, and the palette for everything (2026-10-06)

- [x] Pin messages for everyone (`P` in visual mode, 7 days; 24 h / 30 d
      from `F1`), unpin; pins from the phone and others synced (live and
      history), 📌 on the bubble, a pinned bar under the header (click /
      `:pinned` jumps, cycling)
- [x] Pin chats to the top (`P` in the list, `:pin`, max 3) and mute /
      unmute them (`:mute 8h|1w`, `:unmute`), synced to the phone
- [x] Palette: every visual-mode action for the selected message (only
      those that apply, listed first), the several-selected actions, pin /
      mute, and group admin (rename, add, remove, admin, leave, create)

## Your own space (2026-10-06)

- [x] 📋 Today pinned on top (due from every list, badge), lists as chats
      (Inbox, Notes, Saved, and your own), typed tasks with dates, tags,
      `!`, `list:` prefix, checklists; x/e/t/!/d/u/J/K/>/</m/f/Y/c keys;
      sections; reminders; organising (pin, archive, delete, rename,
      icon); ctrl+p finds lists, `@` finds items; F1 has it all
- [x] Notes (pages written whole, Markdown light) and 🔖 Saved messages
      (`b` in visual mode); `T` makes a task from a message with a link back
- [x] Sharing a list with a chat: "done: eggs" there ticks it off here
- [x] Obsidian mirror, both ways (Tasks plugin marks, `^t` block ids)
- [x] `whatsapp-tui todo/note/capture`, API methods, Super+Shift+T capture
- [x] Local model (qwen3:4b): tasks from messages, to-dos in a chat,
      catch up, plan my day; the parser reads many more time phrases
      ("by the 12th", "a week from friday", "first monday of november").
      AI date guessing was tried and dropped: qwen3:4b without thinking
      gets calendar lookups wrong most of the time

## Search indexing per chat (2026-10-06)

- [x] `:noindex` / `:index` per chat (F1 too), `:noindex list`: out of the
      word index (triggers skip it; the FTS index stays consistent), no
      meaning vectors, skipped by all-chats search and the API; `/` in the
      chat still works by plain matching. Everything indexed by default

## Settings (2026-10-06)

- [x] `ctrl+,` (kitty map → F15), `:settings`, F1: a settings screen with
      every option, changed with ←/→ and saved at once; theme, message
      spacing (compact/normal/roomy), bubble borders (rounded, square,
      thick, double, none), list width, background, mouse, notifications,
      private reading, media cache and AI apply live; the rest after a
      restart (marked ↻)

## Known bugs

- [x] View-once messages never showed (WhatsApp sends linked devices an
      "unavailable" notice instead): now shown as "👁 View once photo",
      opens only on the phone

- [x] The app froze, and new windows closed blank: two chat-settings
      resyncs overlapped (start + a 409 on mark-read), one hit a nil
      pointer while holding the session's read lock and its cleanup
      waited on that same lock forever. Resyncs now run one at a time
      with their own state, nothing that can fail runs under the lock,
      and a window that finds the background app stuck stops it (pid in
      the lock file) instead of giving up

- [x] A chat stayed archived here after the phone unarchived it: a full
      app state sync doesn't mention chats with no archive/pin/mute, so
      stale ones were never cleared. Full syncs now clear them (on start,
      on a 409 conflict, and `:resync`)
- [x] Archive failed with 409 "conflict": resync in full and retry

- [x] Polls, live location and event messages were silently dropped
- [x] Forwarded messages weren't marked (also fixed for re-synced history)
- [x] Animated stickers and GIFs rendered striped (fixed by the image
      changes in the efficiency pass; confirm in the tray)

- [x] Pinned chats weren't kept on top, and pins/archives/mutes from the
      phone were never applied (the initial app-state sync emitted no
      events; on-demand history also unpinned chats)
- [x] Muted chats notified: mutes are now synced, shown (🔕, grey count)
      and respected, except mentions of you

## Housekeeping

- [ ] Confirm animated stickers look right in the real tray (`s`)
- [ ] Re-measure the background app's memory on the live session
- [ ] Bump the Pages workflow actions off Node 20 (GitHub deprecation)

## Notes

- Some people report bans for using unofficial WhatsApp clients: anything
  that sends on its own must be opt-in, rate-limited and visible.
