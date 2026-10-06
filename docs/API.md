# Local API and hooks

whatsapp-tui exposes what it knows to your own scripts and tools in two
ways:

- **The API**: a Unix socket speaking JSON lines. Ask for chats, messages
  and search results, send messages (if you allow it), schedule things, and
  subscribe to a live stream of events.
- **Hooks**: executables the app runs when something happens (a message, a
  mention, a reaction, a reminder), with the event as JSON on stdin.

Both are served by the running app (the background app, see
[USAGE.md](../USAGE.md#running-in-the-background)), so they work with the
window closed. Nothing leaves your machine: the socket is only readable by
you (mode `0600`) and there's no network port.

## Quick start

```sh
whatsapp-tui api status
whatsapp-tui api chats '{"limit": 5}'
whatsapp-tui api messages '{"chat": "919876543210@s.whatsapp.net", "limit": 20}'
whatsapp-tui api search '{"query": "flat address"}'
whatsapp-tui api subscribe '{"events": ["mention"]}'     # streams until ctrl+c
```

`whatsapp-tui api <method> [params]` prints the result as JSON (pipe it to
`jq`), or an error on stderr with exit status 1.

## The socket

Path: `$XDG_RUNTIME_DIR/whatsapp-tui-api.sock` (usually
`/run/user/1000/whatsapp-tui-api.sock`), created when the app starts and
removed when it quits.

Protocol: newline-delimited JSON, both ways. Send one request per line:

```json
{"id": 1, "method": "messages", "params": {"chat": "120363…@g.us", "limit": 2}}
```

and read one response per request, with the same `id`, holding `result` or
`error`:

```json
{"id": 1, "result": [{"id": "3EB0…", "chat": "120363…@g.us", "text": "hi", …}]}
{"id": 2, "error": "messages needs \"chat\""}
```

Requests on one connection are answered in order. Keep the connection open
for as many requests as you like. After a `subscribe`, events arrive on the
same connection as lines without an `id` (see [Events](#events)).

From the shell without the CLI:

```sh
echo '{"id":1,"method":"chats","params":{"limit":3}}' |
  socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/whatsapp-tui-api.sock
```

From Python:

```python
import json, os, socket
s = socket.socket(socket.AF_UNIX)
s.connect(os.path.join(os.environ["XDG_RUNTIME_DIR"], "whatsapp-tui-api.sock"))
f = s.makefile("rw")
f.write(json.dumps({"id": 1, "method": "subscribe", "params": {"events": ["mention"]}}) + "\n"); f.flush()
for line in f:
    event = json.loads(line)
    if event.get("event") == "mention":
        print(event["message"]["chat_name"], event["message"]["text"])
```

## Identifiers

- **Chats** and **senders** are WhatsApp JIDs: `919876543210@s.whatsapp.net`
  for a person (country code and number), `…@g.us` for a group,
  `…@lid` for some group members. `chats` gives you each chat's JID.
- **Message IDs** are WhatsApp's (`3EB0…`), as in `messages` results.
- **Times** are unix seconds.
- **Natural times** (`at` in `schedule`) take what the app's `:later` takes:
  `9am`, `21:30`, `tomorrow 9am`, `fri 5pm`, `next week`, `12 oct 3pm`,
  `in 2h`, `in an hour` …

## Methods

| Method | Params | Result |
|---|---|---|
| `status` | – | `{"connected": bool, "send_allowed": bool}` |
| `chats` | `limit` (optional) | chats, pinned first, then newest ([Chat](#chat)) |
| `messages` | `chat`; `limit` (default 50, max 1000); `before` (unix seconds, for older pages) | messages, oldest first ([Message](#message)) |
| `search` | `query`; `chat` (optional, one chat only); `limit` (optional) | messages with every word (any order), newest first; then, if [search by meaning](../USAGE.md#how-search-matches) is set up, up to 20 close in meaning, marked `"similar": true` |
| `send` | `chat`, `text`; `reply_to` (optional message ID) | `"sent"` — needs [sending allowed](#sending) |
| `reply` | `chat`, `text`, `reply_to` | `"sent"` — needs [sending allowed](#sending) |
| `mark_read` | `chat` | `"ok"`: sends read receipts, clears the unread count everywhere |
| `mark_unread` | `chat` | `"ok"`: marks the chat unread everywhere |
| `schedule` | `chat`, `at`; `kind`: `send` (default; needs `text` and [sending allowed](#sending)), `snooze` or `nudge` | `{"id", "due", "when"}` — see `:later`, `:snooze`, `:nudge` in USAGE.md |
| `subscribe` | `events` (optional list; all when left out): `message`, `mention`, `reaction`, `reminder` | `"subscribed"`, then events |

Paging back through a chat: call `messages` with no `before`, then again
with `before` set to the oldest `time` you got.

### Chat

```json
{"chat": "120363…@g.us", "name": "Hostel", "group": true, "unread": 2,
 "mentioned": false, "pinned": false, "archived": false,
 "last_time": 1791224018, "preview": "see you at 8"}
```

### Message

```json
{"id": "3EB0…", "chat": "120363…@g.us", "chat_name": "Hostel",
 "sender": "919876543210@s.whatsapp.net", "sender_name": "Priya",
 "from_me": false, "time": 1791224018, "text": "[IMAGE] look at this",
 "media": "image", "reply_to": "3EB0…", "forwarded": false, "edited": false,
 "deleted": false, "mentions": {"919812345678": "Ravi"},
 "reactions": [{"sender": "", "emoji": "👍"}]}
```

- `sender` is `""` and `sender_name` `"You"` for your own messages.
- `text` starts with a tag for media and special messages: `[IMAGE]`,
  `[VIDEO]`, `[GIF]`, `[STICKER]`, `[VOICE NOTE] 12s`, `[AUDIO]`,
  `[DOCUMENT] name`, `[LOCATION]`, `[LIVE LOCATION]`, `[POLL]`, `[EVENT]`,
  `[CONTACT]`, `[CONTACTS]`; any caption follows the tag.
- Mentions appear in `text` as `@number`; `mentions` maps those numbers to
  names.
- `deleted` is true for a message its sender deleted for everyone (the text
  is what it said before).

## Events

After `subscribe`, each event is one line:

```json
{"event": "message",  "message": {…Message…}}
{"event": "mention",  "message": {…Message…}}
{"event": "reaction", "reaction": {"chat": "…", "chat_name": "Hostel", "message_id": "3EB0…",
                                   "sender": "91…@s.whatsapp.net", "sender_name": "Priya",
                                   "emoji": "❤️", "from_me": false}}
{"event": "reminder", "reminder": {"chat": "…", "chat_name": "Priya",
                                   "text": "⏰ No reply from Priya since Mon 14:05"}}
```

- `message`: every message as it arrives, **including yours** (sent here or
  on your phone; check `from_me`).
- `mention`: a message that @mentions you or @all. It's also sent as a
  `message`.
- `reaction`: someone (or you: `sender` `""`, `from_me` true) reacted; an
  empty `emoji` means the reaction was taken back.
- `reminder`: a snoozed chat came back, or a `:nudge` went off.

Events only cover what happens while the app runs; messages that arrive in
a history sync (after being offline) aren't events. Use `messages` to catch
up.

## Hooks

Put an executable named after an event in `~/.config/whatsapp-tui/hooks/`:

| File | Runs for |
|---|---|
| `on-message` | every message (yours too: check `from_me`) |
| `on-mention` | messages that mention you (or @all) |
| `on-reaction` | reactions (yours too) |
| `on-reminder` | snoozed chats coming back, nudges |

Each run gets the [event](#events) JSON on stdin, and in the environment
`WT_EVENT` (the event name) and `WT_CHAT` (the chat's JID). A hook may run
for 30 seconds; at most 4 run at once (others wait). Files that aren't
executable are ignored. If a hook fails, its output goes to the background
app's log, `~/.cache/whatsapp-tui/server.log`.

Example, `~/.config/whatsapp-tui/hooks/on-mention`:

```sh
#!/bin/sh
# flash a notification with the text when someone mentions you
jq -r '"\(.message.sender_name) in \(.message.chat_name): \(.message.text)"' |
  xargs -0 notify-send -u critical "You were mentioned"
```

Example, log every message to one file per chat
(`~/.config/whatsapp-tui/hooks/on-message`):

```sh
#!/bin/sh
dir=~/whatsapp-logs; mkdir -p "$dir"
jq -c . >> "$dir/$WT_CHAT.jsonl"
```

Remember to `chmod +x` the file.

## Sending

Sending through the API (`send`, `reply`, and `schedule` with kind `send`)
is **off** until you allow it in `~/.config/whatsapp-tui/config.ini`:

```ini
[general]
api_allow_send = true
```

then restart the app (`whatsapp-tui --stop`, then open it again). It's off
by default because WhatsApp bans some accounts for automated sending, and
because a script, or an AI agent reading your messages, could be tricked by
a message's contents into sending something. Even when on, the API sends at
most 20 messages a minute. Messages sent through the API show up in the
app like any other.

## Developing against it

- The protocol is implemented in `internal/api` (`api.go`: methods and
  events; `json.go`: the shapes above; `hooks.go`; `client.go`: the CLI).
  The app's side is `api.Backend`, implemented by
  `*messages.SessionManager`.
- Events come from `SessionManager.Subscribe` (`internal/messages/events.go`);
  add new event kinds there, emit them where they happen, and map them in
  `Server.eventsFor`.
- `internal/api/api_test.go` runs the server on a real socket against a fake
  backend; new methods should get a case there.
