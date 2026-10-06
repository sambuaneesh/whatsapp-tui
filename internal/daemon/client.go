package daemon

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

// Attach connects this terminal window to the background app, starting it
// first if it isn't running (with serverArgs, e.g. --debug). It returns
// when the window detaches or the app quits.
func Attach(serverArgs []string, logPath string) error {
	for attempt := 0; ; attempt++ {
		conn, err := connect(serverArgs, logPath)
		if err != nil {
			return err
		}
		reason, err := session(conn)
		if err != nil {
			return err
		}
		switch reason {
		case ExitUpgrade:
			if attempt > 0 {
				return errors.New("the background app keeps restarting; see " + logPath)
			}
			// the old server is quitting; wait for it, then start the new one
			if err := waitGone(10 * time.Second); err != nil {
				// it's stuck: stop it, so a hung build can't lock you out
				if !forceStop() || waitGone(5*time.Second) != nil {
					return err
				}
			}
			continue
		case ExitDetached:
			fmt.Println("whatsapp-tui is still running in the background (notifications keep coming).")
			fmt.Println("Run whatsapp-tui to come back, or whatsapp-tui --stop to quit it.")
		case ExitTakeover:
			fmt.Println("whatsapp-tui moved to another window.")
		}
		return nil
	}
}

// Stop asks the background app to quit.
func Stop() error {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return errors.New("whatsapp-tui isn't running in the background")
	}
	defer conn.Close()
	if err := writeFrame(conn, frameQuit, nil); err != nil {
		return err
	}
	_, _, _ = readFrame(bufio.NewReader(conn))
	return waitGone(15 * time.Second)
}

// Running reports whether a background app is listening.
func Running() bool {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// connect dials the server, starting one when none is running.
func connect(serverArgs []string, logPath string) (net.Conn, error) {
	if conn, err := net.Dial("unix", SocketPath()); err == nil {
		return conn, nil
	}
	if err := spawn(serverArgs, logPath); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("unix", SocketPath()); err == nil {
			return conn, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, errors.New("the background app didn't start; see " + logPath)
}

// spawn starts the server detached from this terminal: its own session, so
// closing the window doesn't take it down.
func spawn(args []string, logPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(exe, append([]string{"--server"}, args...)...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start background app: %w", err)
	}
	return cmd.Process.Release()
}

// waitGone waits for a quitting server to let go of its lock.
func waitGone(timeout time.Duration) error {
	path := SocketPath() + ".lock"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return nil // no lock file: nothing running
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		f.Close() // releases it again
		if err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("the background app didn't quit; try whatsapp-tui --stop")
}

func windowSize() Size {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return Size{Rows: 24, Cols: 80}
	}
	return Size{Rows: ws.Row, Cols: ws.Col, X: ws.Xpixel, Y: ws.Ypixel}
}

// session lends this terminal to the server until it says goodbye (or the
// connection drops), and returns the reason.
func session(conn net.Conn) (string, error) {
	defer conn.Close()
	r := bufio.NewReaderSize(conn, 64<<10)
	if err := writeFrame(conn, frameHello, []byte(Stamp())); err != nil {
		return "", err
	}
	// the server accepts us, or says it's outdated, before we take over
	// the terminal
	switch typ, payload, err := readFrame(r); {
	case err != nil:
		return "", fmt.Errorf("background app: %w", err)
	case typ == frameExit:
		return string(payload), nil
	}
	if err := writeFrame(conn, frameResize, windowSize().encode()); err != nil {
		return "", err
	}

	in := int(os.Stdin.Fd())
	state, err := term.MakeRaw(uintptr(in))
	if err != nil {
		return "", fmt.Errorf("terminal: %w", err)
	}
	defer func() {
		os.Stdout.WriteString(modesReset)
		_ = term.Restore(uintptr(in), state)
	}()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			if writeFrame(conn, frameResize, windowSize().encode()) != nil {
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 && writeFrame(conn, frameData, buf[:n]) != nil {
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		typ, payload, err := readFrame(r)
		if err != nil {
			return ExitQuit, nil // the server went away
		}
		switch typ {
		case frameData:
			if _, err := os.Stdout.Write(payload); err != nil {
				return "", err
			}
		case frameExit:
			return string(payload), nil
		}
	}
}

// forceStop stops a background app that won't quit: SIGTERM, then
// SIGKILL. Only a process that is whatsapp-tui's server (by its pid in
// the lock file and its command line) is touched.
func forceStop() bool {
	data, err := os.ReadFile(SocketPath() + ".lock")
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 || pid == os.Getpid() {
		return false
	}
	cmd, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || !strings.Contains(string(cmd), "whatsapp-tui") || !strings.Contains(string(cmd), "--server") {
		return false
	}
	fmt.Println("The background app is stuck; restarting it…")
	_ = unix.Kill(pid, unix.SIGTERM)
	if waitGone(3*time.Second) == nil {
		return true
	}
	_ = unix.Kill(pid, unix.SIGKILL)
	return true
}
