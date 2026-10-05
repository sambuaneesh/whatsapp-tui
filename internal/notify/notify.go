// Package notify shows desktop notifications and plays the new-message
// sound.
package notify

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/gen2brain/beeep"
)

// App is the application name and icon notifications are shown with.
const App = "whatsapp-tui"

// System notifies through the desktop.
type System struct{}

// Popup shows a notification in the desktop's notification drawer. silent
// asks the notification server not to play a sound of its own.
func (System) Popup(title, body string, silent bool) error {
	if runtime.GOOS == "linux" {
		if bin, err := exec.LookPath("notify-send"); err == nil {
			args := []string{"--app-name=WhatsApp", "--icon=" + App, "--category=im.received"}
			if silent {
				args = append(args, "--hint=boolean:suppress-sound:true")
			}
			return exec.Command(bin, append(args, "--", title, body)...).Run()
		}
	}
	return beeep.Notify(title, body, "")
}

// soundFiles are tried in order for the new-message sound on Linux.
var soundFiles = []string{
	"/usr/share/sounds/freedesktop/stereo/message-new-instant.oga",
	"/usr/share/sounds/freedesktop/stereo/message.oga",
	"/usr/share/sounds/freedesktop/stereo/bell.oga",
}

// Sound plays the new-message sound: the sound file straight to the audio
// server first (canberra-gtk-play can succeed without a sound, depending on
// the desktop's event-sound settings), canberra as a fallback.
func (System) Sound() error {
	if runtime.GOOS == "darwin" {
		return exec.Command("afplay", "/System/Library/Sounds/Glass.aiff").Run()
	}
	for _, f := range soundFiles {
		if _, err := os.Stat(f); err != nil {
			continue
		}
		for _, player := range []string{"pw-play", "paplay"} {
			if bin, err := exec.LookPath(player); err == nil {
				if out, err := exec.Command(bin, f).CombinedOutput(); err != nil {
					return fmt.Errorf("%s: %v %s", player, err, out)
				}
				return nil
			}
		}
	}
	if bin, err := exec.LookPath("canberra-gtk-play"); err == nil {
		return exec.Command(bin, "--id=message-new-instant", "--description="+App).Run()
	}
	return errors.New("no sound player found (install pipewire, pulseaudio or libcanberra)")
}
