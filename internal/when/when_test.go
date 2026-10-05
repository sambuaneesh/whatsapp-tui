package when

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	// Wednesday 7 Oct 2026, 14:20
	now := time.Date(2026, 10, 7, 14, 20, 30, 0, time.Local)
	at := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, time.Local) }
	for in, want := range map[string]time.Time{
		"in 2h":          at(7, 16, 20),
		"2h":             at(7, 16, 20),
		"90m":            at(7, 15, 50),
		"in 1h 30m":      at(7, 15, 50),
		"in 3 days":      at(10, 14, 20),
		"in 45 minutes":  at(7, 15, 5),
		"9pm":            at(7, 21, 0),
		"9 PM":           at(7, 21, 0),
		"21:15":          at(7, 21, 15),
		"9am":            at(8, 9, 0), // passed today: tomorrow
		"9:30am":         at(8, 9, 30),
		"12am":           at(8, 0, 0),
		"12pm":           at(8, 12, 0),
		"noon":           at(8, 12, 0),
		"tonight":        at(7, 20, 0),
		"evening":        at(7, 18, 0),
		"tomorrow":       at(8, 9, 0),
		"tomorrow 9am":   at(8, 9, 0),
		"tmrw at 7:45pm": at(8, 19, 45),
		"today 5pm":      at(7, 17, 0),
		"today 9am":      at(8, 9, 0), // passed
		"fri 5pm":        at(9, 17, 0),
		"friday":         at(9, 9, 0),
		"wed 9am":        at(14, 9, 0), // today's passed: next week
		"wed 6pm":        at(7, 18, 0), // still today
		"mon":            at(12, 9, 0),
		"sunday 10:00":   at(11, 10, 0),
	} {
		got, err := Parse(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q: %v %v, want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "soon", "25:00", "13pm", "9:75", "in", "0h", "next week"} {
		if _, err := Parse(bad, now); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestDescribe(t *testing.T) {
	now := time.Date(2026, 10, 7, 14, 20, 0, 0, time.Local)
	for d, want := range map[int]string{0: "today 09:05", 1: "tomorrow 09:05", 3: "Sat 09:05", 9: "16 Oct 09:05"} {
		if got := Describe(time.Date(2026, 10, 7+d, 9, 5, 0, 0, time.Local), now); got != want {
			t.Errorf("+%d days: %q, want %q", d, got, want)
		}
	}
}
