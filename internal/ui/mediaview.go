package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// mediaView is the full-screen viewer (space in visual mode) for photos,
// stickers and GIFs. Videos go to a player instead.
type mediaView struct {
	msg     messages.Message
	loading bool
	image   string // rendered, fitted to the screen
	w, h    int    // original size in pixels
	note    string // e.g. "showing the preview: the full image expired"
	err     error
}

type mediaViewMsg struct {
	id         string
	text, note string
	w, h       int
	err        error
}

// viewMedia opens the selected message's media full screen, or plays it.
func (m *Model) viewMedia(sel messages.Message) tea.Cmd {
	if playableAudio(sel) && m.actions != nil {
		return tea.Batch(m.playVideo(sel, true), m.trackDownload(sel))
	}
	meta, ok := sel.MediaMeta()
	if !ok || m.actions == nil {
		m.notice, m.noticeErr = "nothing to view: select a photo, sticker, GIF, video or voice note", true
		return nil
	}
	if meta.Type == messages.MediaVideo {
		return tea.Batch(m.playVideo(sel, false), m.trackDownload(sel))
	}
	m.view = &mediaView{msg: sel, loading: true}
	track := m.trackDownload(sel)
	a, im := m.actions, m.img
	cols, rows := m.width-4, m.mainHeight()-4 // room for the caption lines
	load := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		out := mediaViewMsg{id: sel.Id, w: meta.Width, h: meta.Height}
		frames, err := func() ([]termimg.Frame, error) {
			path, err := a.MediaPath(ctx, sel.Id)
			if err != nil {
				return nil, err
			}
			maxSide := 2048
			if im != nil {
				maxSide = min(max(cols*im.cellW, rows*im.cellH), 2048)
			}
			if meta.Type == messages.MediaGIF {
				if im == nil || im.mode != termimg.ModeKitty {
					maxSide = 640 // only the first frame is shown
				}
				return termimg.VideoFrames(ctx, path, maxSide)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			return termimg.DecodeFrames(data, maxSide)
		}()
		if err != nil && len(meta.Thumbnail) > 0 {
			// the full file is gone (old media expires): show the preview
			if img, terr := termimg.Decode(meta.Thumbnail); terr == nil {
				frames, out.note = []termimg.Frame{{Img: img}}, "showing the small preview: the full file couldn't be downloaded"
				err = nil
			}
		}
		if err != nil {
			out.err = err
			return out
		}
		b := frames[0].Img.Bounds()
		if out.w == 0 || out.h == 0 {
			out.w, out.h = b.Dx(), b.Dy()
		}
		if im == nil || !im.enabled() {
			out.err = errors.New("images are off; press o to open it in its app")
			return out
		}
		c, r := termimg.FitCells(b.Dx(), b.Dy(), cols, rows, im.cellW, im.cellH)
		out.text, out.err = im.renderFrames(frames, c, r)
		return out
	}
	return tea.Batch(load, track)
}

func (m Model) applyMediaView(r mediaViewMsg) (tea.Model, tea.Cmd) {
	if m.view == nil || m.view.msg.Id != r.id {
		return m, nil
	}
	v := m.view
	v.loading, v.image, v.w, v.h, v.note, v.err = false, r.text, r.w, r.h, r.note, r.err
	return m, nil
}

func (m Model) handleMediaView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", " ", "space", "backspace":
		m.view = nil
	case "o":
		sel := m.view.msg
		m.view = nil
		return m, m.openMessage(sel)
	case "s":
		return m, m.downloadMessage(m.view.msg)
	}
	return m, nil
}

func (m Model) renderMediaView(width, height int) string {
	v := m.view
	var body string
	switch {
	case v.loading && m.dl != nil:
		body = styleDim.Render(m.dl.text)
	case v.loading:
		body = styleDim.Render("Loading…")
	case v.err != nil:
		body = styleErr.Render(v.err.Error())
	default:
		body = v.image
	}
	var info []string
	if v.w > 0 {
		info = append(info, fmt.Sprintf("%d×%d", v.w, v.h))
	}
	if who := m.senderName(v.msg); who != "" {
		info = append(info, who+" · "+toTime(int64(v.msg.Timestamp)).Format("2 Jan 15:04"))
	}
	caption := strings.TrimSpace(messageText(v.msg))
	lines := []string{body, ""}
	if caption != "" {
		lines = append(lines, styleBase.Render(truncateWidth(caption, width-4)))
	}
	if v.note != "" {
		lines = append(lines, styleMuted.Render(v.note))
	}
	lines = append(lines, styleDim.Render(strings.Join(info, "  ·  ")),
		styleMuted.Render("o open in its app · s save · esc close"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, lines...))
}

func truncateWidth(s string, w int) string {
	s = strings.Join(strings.Fields(s), " ")
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// ---------- video ----------

type videoReadyMsg struct {
	path  string
	audio bool // a voice note or audio file
	err   error
}

// playableAudio reports a voice note or audio message with stored media.
func playableAudio(msg messages.Message) bool {
	return msg.MediaType == messages.MediaAudio && len(msg.Media) > 0
}

// playVideo downloads the video (or audio), then plays it: video in an mpv
// window (or the default app), audio in the terminal.
func (m Model) playVideo(sel messages.Message, audio bool) tea.Cmd {
	a := m.actions
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		// saved with an extension so players recognise it
		path, err := a.SaveMedia(ctx, sel.Id, os.TempDir())
		return videoReadyMsg{path: path, audio: audio, err: err}
	}
}

func (m Model) startVideo(r videoReadyMsg) (tea.Model, tea.Cmd) {
	if r.err != nil {
		m.notice, m.noticeErr = r.err.Error(), true
		return m, nil
	}
	if r.audio {
		return m.startAudio(r.path)
	}
	// in its own window: drawing video inside the terminal hangs when the
	// app runs in the background (and blocks it while playing)
	if mpv, err := exec.LookPath("mpv"); err == nil {
		m.notice, m.noticeErr = "Playing in mpv", false
		return m, func() tea.Msg {
			cmd := exec.Command(mpv, "--force-window=yes", "--really-quiet", r.path)
			cmd.SysProcAttr = detached()
			if err := cmd.Start(); err != nil {
				return actionDoneMsg{err: err}
			}
			go cmd.Wait() //nolint:errcheck // reap it when it closes
			return nil
		}
	}
	return m, func() tea.Msg {
		return actionDoneMsg{ok: "Opened the video", err: openURL(r.path)}
	}
}

type videoDoneMsg struct{ err error }

// audioHint is printed above the player's progress line.
const audioHint = "▶ Playing audio · space pause · ←/→ seek · q stop and go back"

// startAudio plays audio in the terminal, mpv's (or ffplay's) progress line
// in place of the app until it ends; without either, in the default app.
func (m Model) startAudio(path string) (tea.Model, tea.Cmd) {
	var player []string
	if mpv, err := exec.LookPath("mpv"); err == nil {
		player = []string{mpv, "--no-video", "--keep-open=no", "--msg-level=all=error,statusline=status", path}
	} else if ffplay, err := exec.LookPath("ffplay"); err == nil {
		player = []string{ffplay, "-nodisp", "-autoexit", "-hide_banner", "-loglevel", "error", "-stats", path}
	} else {
		return m, func() tea.Msg { return actionDoneMsg{ok: "Opened the audio", err: openURL(path)} }
	}
	m.notice, m.noticeErr = "", false
	// the hint is printed once the app has handed over the terminal
	cmd := exec.Command("sh", append([]string{"-c", `printf '\n  %s\n\n' "$0"; exec "$@"`, audioHint}, player...)...)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return videoDoneMsg{err: err} // redraws the screen and images
	})
}
