package daemon

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// ErrRunning is returned by Listen when a server is already running.
var ErrRunning = errors.New("whatsapp-tui is already running")

// Hooks connect the server to the app; all are optional.
type Hooks struct {
	Attach  func(s Size) // a window attached (after the first one)
	Resize  func(s Size) // the attached window changed size
	Detach  func()       // the window went away
	Quit    func()       // stop the app (--stop, or an outdated server)
	Prelude func() string
	Goodbye func() string // sent to a window detaching on purpose
}

// Server runs the app on a pseudo-terminal and lends it to one attached
// window at a time.
type Server struct {
	ln    net.Listener
	lock  *os.File
	ptmx  *os.File
	tty   *os.File
	stamp string
	hooks Hooks

	mu      sync.Mutex
	client  net.Conn // attached window, nil when detached
	started bool
	first   chan Size
}

// Listen claims the socket (only one server runs) and opens the terminal
// the app will run on.
func Listen() (*Server, error) {
	path := SocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, ErrRunning
	}
	// our pid, so a newer window can stop us if we ever hang
	_ = lock.Truncate(0)
	_, _ = lock.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	_ = os.Remove(path) // left by a server that crashed
	ln, err := net.Listen("unix", path)
	if err != nil {
		lock.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	ptmx, tty, err := pty.Open()
	if err != nil {
		ln.Close()
		lock.Close()
		return nil, fmt.Errorf("open terminal: %w", err)
	}
	// make it our controlling terminal, so programs the app runs (mpv,
	// yazi) get resize signals; the server is a session leader
	_ = unix.IoctlSetInt(int(tty.Fd()), unix.TIOCSCTTY, 0)
	return &Server{ln: ln, lock: lock, ptmx: ptmx, tty: tty, stamp: Stamp(), first: make(chan Size, 1)}, nil
}

// TTY is the terminal the app should use for input and output.
func (s *Server) TTY() *os.File { return s.tty }

// Serve accepts windows and copies the app's output to the attached one.
// Call it once, before WaitFirst.
func (s *Server) Serve(h Hooks) {
	s.hooks = h
	go s.pump()
	go func() {
		for {
			c, err := s.ln.Accept()
			if err != nil {
				return
			}
			go s.handle(c)
		}
	}()
}

// WaitFirst blocks until the first window attaches and returns its size;
// the app starts then, so it sees the real window.
func (s *Server) WaitFirst() Size { return <-s.first }

// pump reads the app's output all the time (a full terminal would block
// the app) and forwards it to the attached window, if any.
func (s *Server) pump() {
	buf := make([]byte, 64<<10)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.mu.Lock()
			if c := s.client; c != nil {
				_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if writeFrame(c, frameData, buf[:n]) != nil {
					c.Close() // its reader notices and detaches
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	typ, payload, err := readFrame(r)
	if err != nil {
		return
	}
	switch typ {
	case frameQuit:
		_ = writeFrame(c, frameExit, []byte(ExitQuit))
		if s.hooks.Quit != nil {
			s.hooks.Quit()
		}
		return
	case frameHello:
	default:
		return
	}
	if string(payload) != s.stamp && s.hooks.Quit != nil {
		// a newer build was installed: make way for it
		_ = writeFrame(c, frameExit, []byte(ExitUpgrade))
		s.hooks.Quit()
		return
	}
	if writeFrame(c, frameOK, nil) != nil {
		return
	}
	typ, payload, err = readFrame(r)
	if err != nil || typ != frameResize {
		return
	}
	size, err := decodeSize(payload)
	if err != nil {
		return
	}

	s.mu.Lock()
	if old := s.client; old != nil {
		_ = writeFrame(old, frameExit, []byte(ExitTakeover))
		old.Close()
	}
	s.client = c
	prelude := modesOn
	if s.hooks.Prelude != nil {
		prelude += s.hooks.Prelude()
	}
	_ = writeFrame(c, frameData, []byte(prelude))
	first := !s.started
	s.started = true
	s.mu.Unlock()

	s.setSize(size)
	if first {
		s.first <- size
	} else if s.hooks.Attach != nil {
		s.hooks.Attach(size)
	}

	for {
		typ, payload, err := readFrame(r)
		if err != nil {
			break
		}
		switch typ {
		case frameData:
			_, _ = s.ptmx.Write(payload)
		case frameResize:
			if size, err := decodeSize(payload); err == nil {
				s.setSize(size)
				if s.hooks.Resize != nil {
					s.hooks.Resize(size)
				}
			}
		}
	}
	s.mu.Lock()
	gone := s.client == c
	if gone {
		s.client = nil
	}
	s.mu.Unlock()
	if gone && s.hooks.Detach != nil {
		s.hooks.Detach()
	}
}

func (s *Server) setSize(z Size) {
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Rows: z.Rows, Cols: z.Cols, X: z.X, Y: z.Y})
}

// Detach sends the attached window away (its terminal closes); the app
// keeps running.
func (s *Server) Detach() { s.dropClient(ExitDetached) }

func (s *Server) dropClient(reason string) {
	s.mu.Lock()
	c := s.client
	s.client = nil
	s.mu.Unlock()
	if c == nil {
		return
	}
	if s.hooks.Goodbye != nil {
		_ = writeFrame(c, frameData, []byte(s.hooks.Goodbye()))
	}
	_ = writeFrame(c, frameExit, []byte(reason))
	c.Close()
	if s.hooks.Detach != nil {
		s.hooks.Detach()
	}
}

// Close tells the attached window the app quit and stops listening.
func (s *Server) Close() {
	s.ln.Close()
	_ = os.Remove(SocketPath())
	s.dropClient(ExitQuit)
	s.lock.Close()
}
