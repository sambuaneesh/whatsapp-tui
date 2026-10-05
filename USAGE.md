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
| `R` | retry a message that failed to send |
| `esc` | done |

`ctrl+x` cancels a reply or an edit (or drops an attachment) in any mode.

Edited messages show "edited" next to their time, whether you edited them here,
on your phone, or someone else did.

## Searching

| Where | Key | Finds |
|---|---|---|
| chat list | `/` | chats and contacts, by name or number |
| in a chat | `/` | messages in this chat (whole history) |
| anywhere | `S` or `:search <text>` | messages in every chat |

While typing a chat search, `ctrl+n`/`ctrl+p` jump between matches. `enter`
selects the match (visual mode, ready for `r` `e` `f` …); then `n` goes to the
older match and `N` to the newer one. `esc` twice clears it. Searches are
smartcase: lowercase matches any case, an uppercase letter makes it exact.

In the chat list, `/` also finds contacts you've never written to and
archived chats (by name or number). `enter` stops typing and keeps the
results: browse them with `j` `k`, open one with `enter`, or use `K` `V` `d`
on them. They stay in the sidebar while you're in a chat, so you can go
through several. `/` edits the search, `esc` (or `backspace`) clears it.

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
| `■■` gold | delivered |
| `■■` rose | read (iris: voice note played) |
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
```

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

## Two chats side by side

In a chat, go to the list (`h`), pick another chat and press `v`: it opens on
the right, beside the one you're in, and stays current as messages arrive.
`W` (or a click on it) swaps them, so you write in the other one; `:only`
closes the split.

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
open while the window has focus. Popups use `notify-send` (any notification
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

- **Names show as numbers** or **chats are missing history:** give it a minute
  after connecting; contact names and history sync from your phone, which
  must be online.
- **Old photos show as "📷 Photo":** they arrived before this device was
  linked with media support; recent ones load when you open the chat.
- **Anything else:** run `whatsapp-tui --debug` and check
  `~/.cache/whatsapp-tui/debug.log`.
- **Start over:** `:logout` unlinks this device; delete
  `~/.config/whatsapp-tui/session.db` to force a fresh QR login.
