// Package daemon keeps whatsapp-tui running in the background, like tmux or
// herdr: a server runs the app on a pseudo-terminal, and each launch is a
// client that attaches its terminal window to it. Closing the window only
// detaches; the next launch picks up where you left off.
//
// Protocol (unix socket): frames of a type byte, a big-endian uint32 length
// and the payload, both ways.
package daemon

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Frame types.
const (
	frameHello  = 'H' // client → server: build stamp (an outdated server restarts)
	frameOK     = 'A' // server → client: hello accepted
	frameResize = 'R' // client → server: window size
	frameData   = 'D' // both ways: terminal input / output
	frameQuit   = 'Q' // client → server: stop the server (--stop)
	frameExit   = 'X' // server → client: why the connection ends, then it closes
)

// Exit reasons sent to clients.
const (
	ExitDetached = "detached" // q / :q: the app keeps running
	ExitTakeover = "takeover" // another window attached
	ExitUpgrade  = "upgrade"  // the server is an older build; start it again
	ExitQuit     = "quit"     // the app was quit
)

const maxFrame = 1 << 20

func writeFrame(w io.Writer, typ byte, payload []byte) error {
	buf := make([]byte, 5+len(payload))
	buf[0] = typ
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)
	_, err := w.Write(buf)
	return err
}

func readFrame(r *bufio.Reader) (byte, []byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(head[1:])
	if n > maxFrame {
		return 0, nil, fmt.Errorf("frame too large (%d bytes)", n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return head[0], payload, nil
}

// Size is a terminal window's size in cells and pixels.
type Size struct{ Rows, Cols, X, Y uint16 }

func (s Size) encode() []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint16(b[0:], s.Rows)
	binary.BigEndian.PutUint16(b[2:], s.Cols)
	binary.BigEndian.PutUint16(b[4:], s.X)
	binary.BigEndian.PutUint16(b[6:], s.Y)
	return b
}

func decodeSize(b []byte) (Size, error) {
	if len(b) != 8 {
		return Size{}, errors.New("bad size frame")
	}
	return Size{binary.BigEndian.Uint16(b[0:]), binary.BigEndian.Uint16(b[2:]),
		binary.BigEndian.Uint16(b[4:]), binary.BigEndian.Uint16(b[6:])}, nil
}

// SocketPath is where the server listens: in $XDG_RUNTIME_DIR, private to
// the user.
func SocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("whatsapp-tui-%d", os.Getuid()))
	}
	return filepath.Join(dir, "whatsapp-tui.sock")
}

// Stamp identifies the installed build, so a client can tell that the
// running server is out of date (after make install).
func Stamp() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	st, err := os.Stat(exe)
	if err != nil {
		return exe
	}
	return fmt.Sprintf("%s|%d|%d", exe, st.Size(), st.ModTime().UnixNano())
}

// Terminal modes the app uses. A newly attached window gets them turned on
// (it never saw the app start), and a client turns them off when it leaves.
const (
	modesOn    = "\x1b[?1049h\x1b[2J\x1b[H\x1b[?25l\x1b[?2004h\x1b[?1004h"
	mouseOn    = "\x1b[?1002h\x1b[?1006h"
	modesReset = "\x1b[?1002l\x1b[?1006l\x1b[?1004l\x1b[?2004l\x1b[?25h\x1b[?1049l"
)
