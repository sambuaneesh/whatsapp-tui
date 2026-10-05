package daemon

import (
	"bufio"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFramesAndSizes(t *testing.T) {
	a, b := net.Pipe()
	go func() {
		_ = writeFrame(a, frameData, []byte("hi"))
		_ = writeFrame(a, frameResize, Size{40, 120, 960, 640}.encode())
	}()
	r := bufio.NewReader(b)
	if typ, p, err := readFrame(r); err != nil || typ != frameData || string(p) != "hi" {
		t.Fatalf("%c %q %v", typ, p, err)
	}
	_, p, _ := readFrame(r)
	if z, err := decodeSize(p); err != nil || z != (Size{40, 120, 960, 640}) {
		t.Fatalf("%+v %v", z, err)
	}
}

// testClient is a window attached by hand.
type testClient struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func dial(t *testing.T, stamp string) *testClient {
	t.Helper()
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	c := &testClient{t, conn, bufio.NewReader(conn)}
	_ = writeFrame(conn, frameHello, []byte(stamp))
	return c
}

// next returns the next frame, skipping data that doesn't contain want.
func (c *testClient) next(typ byte, want string) string {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		got, p, err := readFrame(c.r)
		if err != nil {
			c.t.Fatalf("waiting for %c %q: %v", typ, want, err)
		}
		if got == typ && strings.Contains(string(p), want) {
			return string(p)
		}
	}
}

func TestServerAttachTakeoverDetach(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "wtd")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)

	srv, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if _, err := Listen(); err != ErrRunning {
		t.Fatalf("second server: %v", err)
	}
	var mu sync.Mutex
	var events []string
	note := func(e string) { mu.Lock(); events = append(events, e); mu.Unlock() }
	srv.Serve(Hooks{
		Prelude: func() string { return "<hello>" },
		Goodbye: func() string { return "<bye>" },
		Attach:  func(z Size) { note("attach") },
		Detach:  func() { note("detach") },
		Quit:    func() { note("quit") },
	})

	// the first window starts the app with its size and gets the modes
	a := dial(t, Stamp())
	a.next(frameOK, "")
	_ = writeFrame(a.conn, frameResize, Size{Rows: 30, Cols: 100}.encode())
	if z := srv.WaitFirst(); z.Cols != 100 {
		t.Fatalf("first size %+v", z)
	}
	a.next(frameData, "<hello>")
	// app output reaches it, and its keys reach the app
	_, _ = srv.TTY().WriteString("screen")
	a.next(frameData, "screen")
	_ = writeFrame(a.conn, frameData, []byte("k"))

	// a second window takes over
	b := dial(t, Stamp())
	b.next(frameOK, "")
	_ = writeFrame(b.conn, frameResize, Size{Rows: 40, Cols: 120}.encode())
	a.next(frameExit, ExitTakeover)
	b.next(frameData, "<hello>")

	// q in the app: it leaves with the goodbye codes
	srv.Detach()
	b.next(frameData, "<bye>")
	b.next(frameExit, ExitDetached)

	// an older server makes way for a new build
	c := dial(t, "another build")
	c.next(frameExit, ExitUpgrade)

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(events, ","); got != "attach,detach,quit" {
		t.Fatalf("events %s", got)
	}
}
