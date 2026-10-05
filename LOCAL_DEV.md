# Local Omarchy workflow

This checkout is the source for the **WhatsApp TUI** entry in the Omarchy app launcher (Super+Space). The launcher runs `~/.local/bin/whatsapp-tui` in the configured terminal. The separate WhatsApp web app entry is unaffected.

The workflow needs Omarchy's `omarchy tui install` command, `mise`, a C compiler, and `desktop-file-utils`. `make omarchy-watch` also needs `inotifywait` from `inotify-tools`. The installer uses the Go version in `go.mod`; install it with `mise install go@<version>` if it is not already available.

After changing the source, run:

```bash
make omarchy-install
```

This builds with the Go version declared in `go.mod` through `mise`, replaces the installed binary, and refreshes the launcher icon. Close any running WhatsApp TUI window and open it again to use the new build. A failed build leaves the previous binary installed.

For automatic local rebuilds while editing, run `make omarchy-watch` in a terminal and leave it open. It installs once, then rebuilds after Go source, module files, or the SVG icon changes. Stop it with Ctrl-C.

The watcher runs only while that terminal is open. It does not restart an already running client or alter the WhatsApp session files in `~/.config/whatsapp-tui/`.

`make install` remains the project's generic, cross-platform install target. Use the Omarchy targets on this machine.
