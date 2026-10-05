// Package when reads times the way people write them: "9am", "tomorrow
// 9:30", "in 2h", "fri 5pm", "tonight".
package when

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrUnknown means the text isn't a time this package understands.
var ErrUnknown = errors.New(`not a time I understand: try 9am, 21:30, tomorrow 9am, in 2h, fri 5pm or tonight`)

// Default hour for a day without a time ("tomorrow", "monday").
const defaultHour = 9

var (
	durRe   = regexp.MustCompile(`^(?:in\s+)?((?:\d+\s*(?:d|days?|h|hrs?|hours?|m|mins?|minutes?)\s*)+)$`)
	partRe  = regexp.MustCompile(`(\d+)\s*(d|days?|h|hrs?|hours?|m|mins?|minutes?)`)
	clockRe = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*(am|pm|a|p)?$`)
)

var named = map[string]int{ // hour
	"noon": 12, "midnight": 0, "morning": 9, "afternoon": 14, "evening": 18, "tonight": 20,
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday, "mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday, "wed": time.Wednesday,
	"wednesday": time.Wednesday, "thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday,
	"thursday": time.Thursday, "fri": time.Friday, "friday": time.Friday, "sat": time.Saturday,
	"saturday": time.Saturday,
}

// Parse reads s relative to now. The result is always in the future.
func Parse(s string, now time.Time) (time.Time, error) {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	if s == "" {
		return time.Time{}, ErrUnknown
	}
	// a duration: "in 2h", "90m", "1h30m", "in 3 days"
	if durRe.MatchString(s) {
		var d time.Duration
		for _, p := range partRe.FindAllStringSubmatch(s, -1) {
			n, _ := strconv.Atoi(p[1])
			switch p[2][0] {
			case 'd':
				d += time.Duration(n) * 24 * time.Hour
			case 'h':
				d += time.Duration(n) * time.Hour
			default:
				d += time.Duration(n) * time.Minute
			}
		}
		if d <= 0 {
			return time.Time{}, ErrUnknown
		}
		return now.Add(d).Truncate(time.Minute), nil
	}

	// a day, then (optionally) a time
	words := strings.Fields(s)
	day, dayGiven := 0, false // days from today
	switch w := words[0]; {
	case w == "today":
		dayGiven, words = true, words[1:]
	case w == "tomorrow" || w == "tmrw" || w == "tom" || w == "tmr":
		day, dayGiven, words = 1, true, words[1:]
	case weekdays[w] != 0 || w == "sun" || w == "sunday":
		day = (int(weekdays[w]) - int(now.Weekday()) + 7) % 7
		dayGiven, words = true, words[1:]
	}
	if len(words) > 0 && words[0] == "at" {
		words = words[1:]
	}
	hour, minute := defaultHour, 0
	switch rest := strings.Join(words, " "); {
	case rest == "":
		if !dayGiven {
			return time.Time{}, ErrUnknown
		}
	case named[rest] != 0 || rest == "midnight":
		hour = named[rest]
	default:
		m := clockRe.FindStringSubmatch(rest)
		if m == nil {
			return time.Time{}, ErrUnknown
		}
		hour, _ = strconv.Atoi(m[1])
		if m[2] != "" {
			minute, _ = strconv.Atoi(m[2])
		}
		switch ampm := m[3]; {
		case hour > 23 || minute > 59 || (ampm != "" && hour > 12):
			return time.Time{}, ErrUnknown
		case hour == 0 && strings.HasPrefix(ampm, "p"):
			return time.Time{}, ErrUnknown // "00:30pm" means nothing
		case strings.HasPrefix(ampm, "p") && hour < 12:
			hour += 12
		case strings.HasPrefix(ampm, "a") && hour == 12:
			hour = 0
		}
	}
	t := time.Date(now.Year(), now.Month(), now.Day()+day, hour, minute, 0, 0, now.Location())
	if !t.After(now) {
		// passed already: a plain time or today means tomorrow, a weekday
		// next week ("tomorrow" can't have passed)
		if !dayGiven || strings.HasPrefix(s, "today") {
			t = t.AddDate(0, 0, 1)
		} else {
			t = t.AddDate(0, 0, 7)
		}
	}
	return t, nil
}

// Describe writes t for people, relative to now: "today 21:00",
// "tomorrow 09:00", "Mon 09:00", "12 Oct 09:00".
func Describe(t, now time.Time) string {
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	days := int(time.Date(y2, m2, d2, 0, 0, 0, 0, t.Location()).Sub(time.Date(y1, m1, d1, 0, 0, 0, 0, t.Location())).Hours() / 24)
	clock := t.Format("15:04")
	switch {
	case days == 0:
		return "today " + clock
	case days == 1:
		return "tomorrow " + clock
	case days > 1 && days < 7:
		return t.Format("Mon") + " " + clock
	}
	return t.Format("2 Jan") + " " + clock
}
