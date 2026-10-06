# whatsapp-tui

A vim-style WhatsApp client for the terminal: modal keys, inline images,
animated stickers and GIFs, replies, reactions, search and file sending, in
Rosé Pine colours. Built on [whatsmeow](https://github.com/tulir/whatsmeow).

Tour of every feature, with a playable demo: [the website](https://sambuaneesh.github.io/whatsapp-tui/)
(source in `site/`; `make site` previews it locally).

## Install

You need the **Go version declared in `go.mod`** (Go can fetch the required
toolchain) and a **C compiler** (for SQLite).

```bash
git clone git@github.com:sambuaneesh/whatsapp-tui.git
cd whatsapp-tui
make install            # installs to ~/.local/bin + a launcher entry (rofi, app menus)
whatsapp-tui            # scan the QR code: WhatsApp → Settings → Linked devices
```

`~/.local/bin` needs to be on your `PATH` (`make install` tells you if it
isn't). For all users: `sudo make install PREFIX=/usr/local`.

On Omarchy, use the fork's [local development workflow](LOCAL_DEV.md) to keep
the app launcher updated as you change the source.

### Dependencies

| Package | Needed for | |
|---|---|---|
| Go, gcc/clang | building | required |
| [kitty](https://sw.kovidgoyal.net/kitty/) (or another kitty-graphics terminal) | sharp images, animations, profile pictures | recommended; other terminals get block-character images |
| [yazi](https://yazi-rs.github.io) | attaching files (`a`) | optional |
| ffmpeg | making stickers/GIFs, playing GIFs | optional |
| mpv | playing videos (`space` in visual mode) | optional; otherwise your default player |
| wl-clipboard | clipboard on Wayland (X11 works without it) | optional |

**Debian / Ubuntu**
```bash
sudo apt install golang gcc ffmpeg mpv wl-clipboard
# yazi: https://yazi-rs.github.io/docs/installation
```

**Arch**
```bash
sudo pacman -S go gcc ffmpeg mpv yazi wl-clipboard
```

**Fedora**
```bash
sudo dnf install golang gcc ffmpeg-free wl-clipboard   # yazi: see its docs
```

**macOS**
```bash
xcode-select --install          # C compiler
brew install go ffmpeg mpv yazi
```
Pasting images from the clipboard isn't supported on macOS yet.

**Windows**: use WSL with the Ubuntu steps above (images fall back to block
characters outside kitty). Native Windows is untested.

## Use

The full guide is in [USAGE.md](USAGE.md); press `?` in the app for every key. Like VS Code, `ctrl+p` goes to any
chat or contact (fuzzy: `hrsh` finds Hari Shankar) and `F1` /
`ctrl+shift+p` runs any feature by name. The basics: `j`/`k` move, `enter` opens a
chat, `i` writes, `esc` goes back to normal mode, `v` selects messages
(`enter` reply, `r` react, `e` edit, `f` forward, `y` copy, `s` save, `d` delete, `P` pin), `/` searches,
`S` searches all chats, `:q` closes the window (it keeps running in the
background, like tmux; the next launch picks up where you were) and `:q!` quits.

**kitty:** so `shift+enter` makes a new line and `ctrl+v` pastes images, add
to `kitty.conf` (only affects whatsapp-tui):

```conf
map --when-focus-on var:whatsapp_tui shift+enter send_text all \x1b\r
map --when-focus-on var:whatsapp_tui ctrl+v
map --when-focus-on var:whatsapp_tui ctrl+shift+p send_text all \e[25~
map --when-focus-on var:whatsapp_tui ctrl+shift+f send_text all \e[26~
map --when-focus-on var:whatsapp_tui ctrl+comma send_text all \e[28~
```

**Your own space:** 📋 Today is pinned on top of your chats, and your task
lists, notes and saved messages are chats of their own: type `call mom
6pm #family !` and it's a task due at 18:00; `x` ticks it off. They can
mirror to an Obsidian vault both ways, and a small local model (Ollama,
`qwen3:4b`) can turn messages into tasks, find the to-dos in a chat, plan
your day and catch you up. See [USAGE.md](USAGE.md#your-own-space-lists-notes-saved).

Scripts and tools can use it through a local API and hooks: see
[docs/API.md](docs/API.md).

`ctrl+,` (or `:settings`) opens the settings: theme, the space between
messages, bubble borders, notifications, AI and more, changed with the
arrow keys. They live in `~/.config/whatsapp-tui/config.ini` (on macOS:
`~/Library/Application Support/whatsapp-tui/`): theme, images, mouse,
download folder. `--debug` writes logs to `~/.cache/whatsapp-tui/debug.log`.

## Credits

Based on [whatscli](https://github.com/normen/whatscli) (MIT). Colours by
[Rosé Pine](https://rosepinetheme.com) (MIT). Unofficial client: using it may
break WhatsApp's terms of service.
