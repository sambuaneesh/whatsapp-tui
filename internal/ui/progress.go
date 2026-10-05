package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// progressSource reports media download progress; *messages.SessionManager
// implements it.
type progressSource interface {
	DownloadProgress(msgID string) (done, total int64, ok bool)
}

// download is a media download you started (view, open, save, play, copy),
// shown with its progress until it finishes.
type download struct {
	id, label string
	started   time.Time
	seen      bool // the backend reported it at least once
	text      string
}

type downloadTickMsg struct{ id string }

const downloadTickEvery = 200 * time.Millisecond

// trackDownload shows progress for msg's media until it's ready.
func (m *Model) trackDownload(msg messages.Message) tea.Cmd {
	if len(msg.Media) == 0 {
		return nil
	}
	m.dl = &download{id: msg.Id, label: "Downloading " + mediaNoun(msg), started: clickNow()}
	m.dl.text = m.dl.label + "…"
	m.notice, m.noticeErr = m.dl.text, false
	return downloadTick(msg.Id)
}

func downloadTick(id string) tea.Cmd {
	return tea.Tick(downloadTickEvery, func(time.Time) tea.Msg { return downloadTickMsg{id} })
}

// downloadDone stops showing progress (the media is ready, or failed).
func (m *Model) downloadDone() {
	if m.dl != nil && m.notice == m.dl.text {
		m.notice = ""
	}
	m.dl = nil
}

func (m Model) applyDownloadTick(t downloadTickMsg) (tea.Model, tea.Cmd) {
	d := m.dl
	if d == nil || d.id != t.id {
		return m, nil
	}
	p, ok := m.actions.(progressSource)
	if !ok {
		return m, nil
	}
	done, total, ok := p.DownloadProgress(d.id)
	switch {
	case !ok && d.seen: // finished; the result message follows
		return m, nil
	case !ok && clickNow().Sub(d.started) > time.Minute: // never started
		m.downloadDone()
		return m, nil
	}
	text := d.label + "…"
	switch {
	case ok && done < 0:
		text = d.label + ": waiting for other downloads…"
	case ok:
		text = d.label + "  " + progressText(done, total)
	}
	d.seen = d.seen || ok
	if m.notice == d.text || m.notice == "" { // don't hide other messages
		m.notice, m.noticeErr = text, false
	}
	d.text = text
	return m, downloadTick(d.id)
}

// progressText is "▓▓▓▓░░░░ 45%  3.2 / 7.1 MB", or just the size so far
// when the total is unknown.
func progressText(done, total int64) string {
	if total <= 0 {
		return mb(done)
	}
	frac := min(float64(done)/float64(total), 1)
	const width = 16
	full := int(frac * width)
	return fmt.Sprintf("%s%s %d%%  %s / %s", strings.Repeat("▓", full), strings.Repeat("░", width-full),
		int(frac*100), strings.TrimSuffix(mb(done), " MB"), mb(total))
}

func mb(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

// mediaNoun names a message's media for messages like "Downloading video".
func mediaNoun(msg messages.Message) string {
	switch msg.MediaType {
	case messages.MediaImage:
		return "photo"
	case messages.MediaVideo:
		return "video"
	case messages.MediaGIF:
		return "GIF"
	case messages.MediaSticker:
		return "sticker"
	case messages.MediaAudio:
		if strings.HasPrefix(msg.Text, "[VOICE NOTE]") {
			return "voice note"
		}
		return "audio"
	}
	return "file"
}
