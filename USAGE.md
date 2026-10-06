# Using whatsapp-tui

whatsapp-tui works like vim: you're in **normal mode** to move around, **insert
mode** to type, and **visual mode** to act on messages. Press `?` any time for
the full key list.

## Run it from anywhere

```bash
make install            # copies the binary to ~/.local/bin
whatsapp-tui            # now works from any directory
```

`~/.local/bin` must be on your `PATH` (it is on most Linux setups; `make
install` warns if not). If it isn't, add this to `~/.bashrc` or `~/.zshrc`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

It also adds **WhatsApp TUI** to rofi and your app menu, opening in its own
kitty window (another terminal: `make install TERMINAL=alacritty`).

To install for every user instead: `sudo make install PREFIX=/usr/local`.
Without make: `install -m 755 whatsapp-tui ~/.local/bin/`. Remove it with
`make uninstall`. Rebuild after pulling changes with `make install` again.

## First start

Run `whatsapp-tui` and scan the QR code with your phone: **WhatsApp →
Settings → Linked devices → Link a device**. The login is remembered; your
chats and recent history load in the background.

## The palette (like VS Code)

The fastest way to get anywhere:

| Key | |
|---|---|
| `ctrl+p` | **Go to chat:** type any part of a name, letters in order (`hrsh` finds *Hari Shankar*), or 4+ digits of a number. Chats you opened recently come first; contacts you've never written to and archived chats are found too. `enter` opens it at once, `alt+enter` opens it beside the current chat (split view). |
| `ctrl+p` `enter` | back to the chat you were in before (like alt+tab) |
| `F1` or `ctrl+shift+p` | **Commands:** every feature by name: "snooze", "only unread", "notifications off", "send later", "mute", "pin"… Only what works where you are is listed: with a message selected (`v`), its actions come first (reply, react, pin, delete…), and with several selected (`V`), what works on all of them. The ones you used last come next. Commands that need more (a time, a folder, a number) start the `:` line for you to finish. |
| `>` in `ctrl+p` | switches to commands (`backspace` goes back to chats) |
| `#` in `ctrl+p` | search messages in all chats: `#pizza` then `enter` |
| `ctrl+f` | find in this chat (in the list: go to chat) |
| `F3` `shift+F3` | next / previous match |
| `ctrl+shift+f` | search messages in all chats |

In the palette: `↑` `↓` (or `ctrl+n`/`ctrl+p`, `tab`) move, `ctrl+u` clears,
`ctrl+w` deletes a word, `esc` closes. The mouse works too: click a result,
scroll with the wheel, click outside to close.

The chat that's highlighted (in the palette or the list) loads in the
background, and the chats you've looked at recently stay loaded, so opening
one draws it straight away.

`ctrl+shift+p` and `ctrl+shift+f` need two lines in kitty (see
[kitty setup](#kitty-setup)): terminals send them the same as `ctrl+p` and
`ctrl+f`. `F1` works everywhere without setup.

## Moving around

The chat list opens first. `enter` (or `l`) opens a chat; the list moves to
the left as a sidebar and `backspace` (or `q`) goes back. A chat with unread
messages opens with the "N unread messages" line in the middle of the screen
(the unread ones below it);
otherwise (or when the phone reports more unread than have loaded) at the
newest message. Older history loading in doesn't move what you're reading.

A chat counts as read once it's open while the terminal window has focus
(messages that arrive while you watch too). That clears it on your phone as
well, and reading it on the phone clears it here.

| Key | |
|---|---|
| `j` `k` | down / up |
| `gg` `G` | top / bottom |
| `ctrl+d` `ctrl+u` | half a page |
| `h` `l` | jump between the sidebar and the messages |
| `u` | only unread chats |
| `A` | archived chats, from anywhere (or `enter` on the **Archived** row at the top) |
| `K` | chat info: description / about, members, picture |
| `V` | the profile picture, full screen |
| `d` then `enter` | delete the chat (on all your devices) |
| `:q` | close the window; the app keeps running (see below) |
| `:q!` | quit for real |

With the mouse: click a chat to open it, click a link to open it, click a
photo, sticker, GIF, video or voice note (`▶ 🎤`) to view or play it, click the quote in a reply to jump to
the message it replies to (selected, as in visual mode), double-click a message
to reply to it, right-click a message to react (click an emoji on the bar),
click the reactions under a message to see who reacted (click yours to take
it back), and scroll with the wheel.

**Reactions** to your messages notify you like messages do ("Priya: Reacted ❤️
to: dinner at 8?"). The emoji grid (`r` then `+`, or type a name) has every
emoji: type to search by name, arrows or the mouse to pick.

## Writing

`i` (or `enter`) starts typing, `enter` sends, `esc` goes back to normal mode.
What you write is kept per chat: switch to another chat (or close the window)
and it's still there when you come back, shown as "Draft:" in the chat list.
The input box works like other text boxes: `ctrl+a` selects everything (then
typing replaces it, `backspace` deletes it, `ctrl+c` copies and `ctrl+x` cuts
it), `ctrl+←` / `ctrl+→` jump a word, `ctrl+backspace` deletes one, `home` /
`end` go to the line's start / end. Attach files with `a` in normal mode.

- **New line:** `shift+enter` in kitty (see [kitty setup](#kitty-setup)), or
  `alt+enter` / `ctrl+j` anywhere.
- **Formatting** shows like on the phone: `*bold*`, `_italic_`, `~strike~`,
  `` `code` ``, `> quote`, `- lists`.
- **Mentions:** in a group, type `@` and a member list pops up; `tab` picks,
  `ctrl+n`/`ctrl+p` move. The person gets notified.
- **@all** mentions everyone in the group (it's first in the list, or just
  type `@all`). Like on the phone, groups over 32 members allow it only for
  admins.
- **Mentions of you** stand out in red ("@ mentioned you"), chats with one show
  a red `@` badge, and `@` (in normal or visual mode) jumps between them.

## Acting on messages (visual mode)

Press `v`: the message you're looking at is selected (the newest one when
you're at the bottom). Move with `j` `k`, `gg` (oldest) and `G` (newest), then:

| Key | |
|---|---|
| `enter` | reply |
| `p` | reply privately to a group member |
| `r` | react: `1`–`6` quick emoji, `x` removes yours, `+` (or start typing a name like `fire`) opens every emoji |
| `w` | who reacted; `x` removes your reaction |
| `e` | edit your message: it opens in the input box; `enter` saves, `esc` cancels (text messages, first 15 minutes) |
| `f` | forward: type to filter chats, `space` picks several, `enter` sends |
| `y` | copy the text and/or image |
| `s` | save (download) to your download folder |
| `d` | delete: `enter` for you, `e` for everyone (your messages, up to ~2½ days old) |
| `space` | view the photo, sticker or GIF full screen; play a voice note; open a video in mpv |
| `o` | open the photo/file in its app, or the message's link |
| `V` | select several: `j`/`k` extend, then `y` copies them as a transcript, `f` forwards them all, `d` deletes them, `s` saves their media; `V` or `esc` ends |
| `P` | pin it for everyone in the chat for 7 days; `P` again unpins (24 hours or 30 days: from `F1`) |
| `R` | retry a message that failed to send |
| `F1` | every action above by name, only those that fit the selected message (or messages) |
| `esc` | done |

`ctrl+x` cancels a reply or an edit (or drops an attachment) in any mode.

Edited messages show "edited" next to their time, whether you edited them here,
on your phone, or someone else did.

**Pinned messages** show 📌 next to their time, and the newest pin sits in a
bar under the chat's name. Click the bar (or `F1` → "pinned message", or
`:pinned`) to jump to it; with several pins each click goes to the next.
Pins from your phone or from others show up the same way, and end when
their time (24 hours, 7 days or 30 days) is up.

## Searching

| Where | Key | Finds |
|---|---|---|
| anywhere | `ctrl+p` | chats and contacts, fuzzy, by name or number (see [the palette](#the-palette-like-vs-code)) |
| chat list | `/` | chats and contacts, filtering the list |
| in a chat | `/` or `ctrl+f` | messages in this chat (whole history) |
| anywhere | `S`, `ctrl+shift+f` or `:search <text>` | messages in every chat |

While typing a chat search, `ctrl+n`/`ctrl+p` jump between matches. `enter`
selects the match (visual mode, ready for `r` `e` `f` …); then `n` (or `F3`)
goes to the older match and `N` (or `shift+F3`) to the newer one. `esc` twice clears it. Searches are
smartcase: lowercase matches any case, an uppercase letter makes it exact.

In the chat list, `/` filters the list, and also finds contacts you've
never written to and archived chats (by name or number). `ctrl+n`/`ctrl+p`
move while typing; `enter` opens the highlighted one. The results stay in
the sidebar while you're in a chat, so you can go through several (`j` `k`,
or `K` `V` `d` on them). `/` edits the filter, `esc` (or `backspace`) clears
it.

In the all-chats search, `enter` opens the result in its chat with the message
selected.

## Photos, files, stickers, GIFs

- **Paste a screenshot:** `ctrl+v` while typing (or `p` in a chat). It waits
  above the input box; type a caption and `enter`.
- **Attach files:** `a` in a chat (in normal mode) opens
  [yazi](https://yazi-rs.github.io): `space` picks several files, `enter`
  attaches them. Images are sent as photos, the rest as documents. Without
  yazi: `:attach ~/file.pdf`.
- **Drag and drop:** drag files from your file manager onto the window while
  a chat is open; they're attached the same way. (The terminal pastes their
  paths, so this works in kitty and most other terminals. An image dragged
  straight from a browser is a link, not a file: save it first, or copy it
  and `ctrl+v`.)
- **Stickers & GIFs:** `s` opens a tray of the ones you've received or sent.
  `hjkl` move, `tab` switches between stickers and GIFs, `enter` sends. In
  the tray, `n` makes a new one from a file and `p` turns the clipboard image
  into a sticker (needs ffmpeg).
- **Viewing:** `v`, select a photo, sticker or GIF, `space` shows it full
  screen (GIFs and animated stickers play in kitty); `esc` closes. Videos play
  in their own mpv window (or your default video app). Downloads show their
  progress on the bottom line.
  Voice notes and audio play in the terminal with mpv (or ffplay): `space`
  pauses, `←`/`→` seek, `q` stops and goes back to the chat.
- **Copying:** `v`, select the message, `y` copies its text (or its picture).
  To select text with the mouse instead, hold `shift` while dragging.
- **Downloads** go to `~/Downloads`. `:download-dir` shows the folder,
  `:download-dir ~/Pictures/wa` changes it.

## Message status

Your messages show two blocks instead of ticks:

| | |
|---|---|
| `□□` | sending |
| `■□` | sent |
| `■■` grey | delivered |
| `■■` blue | read (purple: voice note or video played) |
| `✕` | not sent — `v`, select it, `R` to retry |

If you turned read receipts off in WhatsApp, WhatsApp doesn't send you
anyone's either, so one-to-one chats stop at delivered.

## kitty setup

kitty sends the same key for `enter` and `shift+enter`, and keeps `ctrl+v` for
its own paste. These lines in `~/.config/kitty/kitty.conf` change that only
inside whatsapp-tui:

```conf
map --when-focus-on var:whatsapp_tui shift+enter send_text all \x1b\r
map --when-focus-on var:whatsapp_tui ctrl+v
# VS Code's ctrl+shift+p (commands) and ctrl+shift+f (search all chats)
map --when-focus-on var:whatsapp_tui ctrl+shift+p send_text all \e[25~
map --when-focus-on var:whatsapp_tui ctrl+shift+f send_text all \e[26~
```

The last two send `ctrl+shift+p` and `ctrl+shift+f` as F13 and F14, which
whatsapp-tui reads as those keys (terminals can't tell them apart from
`ctrl+p` and `ctrl+f`). Other terminals: map them to the same sequences, or
use `F1` and `S`.

Reload with `ctrl+shift+f5` and restart whatsapp-tui. To select text with the
mouse while it runs, hold `shift` while dragging.

## Private reading (no blue ticks)

`:private` (or `private_reading = true` in the config) lets you open chats
without sending read receipts: no blue ticks for the sender, and the chat
stays unread for you. Mark a chat read when you choose: `U` in the list or
in the chat, or `:read`. The status bar shows 🙈 while it's on; click it (or
`:private off`) to turn it off, which marks the open chat read.

## Deleted messages

When someone deletes a message for everyone after it reached you, it stays
readable here, marked "🚫 deleted by <name>" with a faded border (your own
show "🚫 you deleted this for everyone"). Only **delete for me** (`d` then
`enter`) removes a message from this app. Messages deleted before this
version only kept the "This message was deleted" note.

## Going through your chats

Like an inbox: `J` opens the next unread chat; in it, `e` marks it read,
archives it and opens the next unread one (back to the list when there are
none left). `e` in the list does the same for the selected chat (in the
archive, `e` brings it back), and `U` marks a chat unread, or read. These
change the chat on your phone too.

To **unarchive** a chat: `A` (or click **Archived**), select it, `e`; or
`F1` → "unarchive", or `:unarchive` in it. `:archive-chat` (or `F1` →
"archive") archives a chat without marking it read, unlike `e`.

`P` in the list pins the selected chat to the top (WhatsApp allows three) or
unpins it; `:pin` / `:unpin` do it for the open chat. `:mute 8h`, `:mute 1w`
or `:mute` (always) mutes a chat, `:unmute` unmutes it; muted chats show 🔕
and don't notify, except when you're mentioned. These sync with your phone.

In groups, the palette (`F1`) also has: rename the group (`:subject`), add
or remove a member and make or remove an admin (`:add`, `:remove`, `:admin`,
`:removeadmin`, with a phone number), leave (`:leave`), and a new group
(`:create <numbers,comma-separated> <name>`).

## How search matches

`/` (this chat) and `S` (all chats) find messages with **every word** you
type, in any order, word beginnings included: `flat addr` finds "the
address of the flat". Accents don't matter (`cafe` finds "Café"); case
doesn't either, unless you type a capital. When that finds nothing, the text
as typed is looked for anywhere (inside words, emoji). A full-text index
makes it fast (0.3 ms over 50,000 messages); it's built automatically.

**By meaning** (optional): with a local model running, `S` also lists up to
20 messages close in meaning to what you typed, marked **≈ similar in
meaning**: `flat address` finds "send me the location of the apartment".
Everything stays on your machine. Set it up once:

```sh
sudo pacman -S ollama-cuda         # or ollama (CPU); other distros: ollama.com
sudo systemctl enable --now ollama
ollama pull embeddinggemma         # ~620 MB
```

The app indexes your messages in the background (newest first; a few
minutes for tens of thousands), then new ones every 15 minutes. It's gentle
on a gaming machine: Ollama gets 2 CPU threads, unloads the model from the
GPU 10 seconds after indexing (2 minutes after a search), and indexing waits
while the GPU is over 50% busy (`semantic_pause_gaming = false` to index
anyway). Without Ollama it does nothing. `semantic_search = false` turns it
off.

## Two chats side by side

With a chat open, open a second one beside it, on the right:

- **Keyboard:** `ctrl+p`, type the other chat's name, `alt+enter`. Or `h` to
  the chat list, pick it with `j`/`k`, press `v`; or type `:split <name>`
  (e.g. `:split priya`).
- **Mouse:** middle-click (or `ctrl`+click) the other chat in the sidebar.

It stays current as messages arrive. `W` (or a click on it) swaps the two,
so you write in the other one. To close it: **`X`**, the **✕** at the right
end of its header, or `:close` / `:only`.

## Activity

`I` (or `:activity`) lists what happened to you across all chats, newest
first: messages that @mention you (or @all), replies to your messages, and
reactions to your messages. `enter` or a click opens the message in its
chat, loading older history if needed.

## Later: send, snooze, nudge

The background app can do things later, even with the window closed
(WhatsApp itself can't):

- `:later <when>` sends what's in the input box then: write it, `esc`, then
  e.g. `:later tomorrow 9am`.
- `:snooze <when>` archives the chat; it comes back, unread, with a
  notification.
- `:nudge <when>` reminds you then if they haven't replied by then.
- `:scheduled` lists what's pending (`x` cancels); the status bar shows a ⏰
  count you can click.

Times:

| Kind | Examples |
|---|---|
| Clock | `9am`, `9:30pm`, `21:30`, `00:35am`, `noon`, `tonight`, `eod` |
| Day | `today 5pm`, `tomorrow`, `fri 5pm`, `next friday`, `next week`, `weekend` |
| Date | `12 oct`, `oct 12 3pm`, `12/10` (day/month), `2026-10-12 9:00` |
| From now | `in 2h`, `90m`, `in an hour`, `in half an hour`, `in 3 days`, `in 2 weeks` |

A day without a time means 9:00; a time that's passed today means tomorrow.
A message due while you're offline is sent as soon as you're connected again.

## Running in the background

Like tmux or herdr, there's only ever one whatsapp-tui. The first launch
starts it in the background and every launch is a window onto it:

- **Closing the window** (or `q`, `:q`, `ctrl+c`) leaves it running, still
  connected and still notifying. Launch it again and you're back exactly where
  you were: same chat, scroll position, half-written message.
- **Opening a second window** moves it there; the first one closes.
- **Quitting for real:** `:q!` inside it, or `whatsapp-tui --stop`.
- **After installing a new version**, the next launch restarts it on the new
  build by itself.
- `whatsapp-tui --foreground` runs it the old way, in this terminal only
  (`background = false` in the config makes that the default). The background
  app's own log is `~/.cache/whatsapp-tui/server.log`.

## Scripts, hooks and the API

The running app answers scripts on a local socket (`whatsapp-tui api chats`,
`whatsapp-tui api subscribe`, …) and runs your hooks
(`~/.config/whatsapp-tui/hooks/on-message`, `on-mention`, `on-reaction`,
`on-reminder`) with each event as JSON. Sending through it is off unless
`api_allow_send = true`. Everything is in [docs/API.md](docs/API.md).

## Notifications

The status bar shows what a new message does; click it, press `M`, or use
`:notify <mode>` to change it (the choice is saved):

| Badge | Mode | New message |
|---|---|---|
| `🔔 all` | `all` | a popup in the notification drawer, and a sound |
| `💬 popup` | `popup` | a silent popup |
| `🔊 sound` | `sound` | only a sound |
| `🔕 off` | `off` | nothing |

Switching plays a sample. There's no notification for the chat you have
open while the window has focus, nor for chats you've muted on your phone
(they show 🔕 and a grey unread count), except when someone @mentions you. Popups use `notify-send` (any notification
daemon: Omarchy's, mako, dunst…); the sound is the desktop's "new message"
sound, played with `canberra-gtk-play`, `pw-play` or `paplay`.

## Settings

`~/.config/whatsapp-tui/config.ini` (macOS: `~/Library/Application
Support/whatsapp-tui/config.ini`):

```ini
[general]
download_path = ~/Downloads
notifications = all            ; all, popup, sound, off (M in the app)
background    = true           ; keep running when the window closes
private_reading = false        ; true: no read receipts until you mark a chat read
media_cache_mb  = 1024         ; downloaded media kept on disk
api_allow_send  = false        ; true: scripts may send through the local API
semantic_search = true         ; search by meaning when Ollama runs (see "How search matches")
ollama_url      = http://127.0.0.1:11434
embed_model     = embeddinggemma
semantic_pause_gaming = true   ; don't index while the GPU is busy (games)

[ui]
theme            = rose-pine   ; rose-pine, rose-pine-moon, rose-pine-dawn
paint_background = false       ; true paints the theme background
images           = auto        ; auto, kitty, blocks, off
avatars          = true
mouse            = true
highlight_opacity = 0.8        ; kitty: see-through highlights, bubbles and status bar (1 = solid)
chat_sidebar_width = 38
qr_compact       = false       ; smaller login QR code
```

## When something's off

- **A chat is archived (or pinned, or muted) here but not on your phone:**
  `:resync` (or `F1` → "resync") fetches those settings from WhatsApp again
  and makes this device match the phone. It also happens on every start,
  and by itself when WhatsApp rejects a change with a "conflict".
- **Names show as numbers** or **chats are missing history:** give it a minute
  after connecting; contact names and history sync from your phone, which
  must be online.
- **Old photos show as "📷 Photo":** they arrived before this device was
  linked with media support; recent ones load when you open the chat.
- **Anything else:** run `whatsapp-tui --debug` and check
  `~/.cache/whatsapp-tui/debug.log`.
- **Start over:** `:logout` unlinks this device; delete
  `~/.config/whatsapp-tui/session.db` to force a fresh QR login.
