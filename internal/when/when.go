// Package when reads times the way people write them: "9am", "tomorrow
// 9:30", "in 2h", "fri 5pm", "next friday", "12 oct 3pm", "tonight", "eod".
package when

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrUnknown means the text isn't a time this package understands.
var ErrUnknown = errors.New(`not a time I understand: try 9am, 21:30, tomorrow 9am, in 2h, fri 5pm, next week, 12 oct 3pm or tonight`)

// Default hour for a day without a time ("tomorrow", "monday", "12 oct").
const defaultHour = 9

var (
	durRe   = regexp.MustCompile(`^(?:in\s+)?((?:\d+\s*(?:w|wks?|weeks?|d|days?|h|hrs?|hours?|m|mins?|minutes?)\s*)+)$`)
	partRe  = regexp.MustCompile(`(\d+)\s*(w|wks?|weeks?|d|days?|h|hrs?|hours?|m|mins?|minutes?)`)
	clockRe = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*(am|pm|a|p)?$`)

	// dates: "12 oct", "12th october", "oct 12", "12/10" (day/month),
	// "2026-10-12"; a time may follow
	dayMonRe = regexp.MustCompile(`^(\d{1,2})(?:st|nd|rd|th)?\s+([a-z]{3,9})\b\s*(.*)$`)
	monDayRe = regexp.MustCompile(`^([a-z]{3,9})\s+(\d{1,2})(?:st|nd|rd|th)?\b\s*(.*)$`)
	slashRe  = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})\b\s*(.*)$`)
	isoRe    = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})\b\s*(.*)$`)
)

// spelled-out amounts: "in an hour", "in half an hour", "a week"
var spelled = strings.NewReplacer(
	"half an hour", "30m", "an hour", "1h", "a minute", "1m", "a day", "1d", "a week", "1w",
)

var named = map[string]int{ // hour
	"noon": 12, "midnight": 0, "morning": 9, "afternoon": 14, "evening": 18, "tonight": 20,
	"eod": 18, "end of day": 18,
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday, "mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday, "wed": time.Wednesday,
	"wednesday": time.Wednesday, "thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday,
	"thursday": time.Thursday, "fri": time.Friday, "friday": time.Friday, "sat": time.Saturday,
	"saturday": time.Saturday,
}

func isWeekday(w string) bool {
	_, ok := weekdays[w]
	return ok
}

var months = map[string]time.Month{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "sept": 9, "oct": 10, "nov": 11, "dec": 12,
	"january": 1, "february": 2, "march": 3, "april": 4, "june": 6, "july": 7, "august": 8, "september": 9,
	"october": 10, "november": 11, "december": 12,
}

// Parse reads s relative to now. The result is always in the future.
func Parse(s string, now time.Time) (time.Time, error) {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	s = spelled.Replace(s)
	if s == "" {
		return time.Time{}, ErrUnknown
	}
	// a duration: "in 2h", "90m", "1h30m", "in 3 days", "in 2 weeks"
	if durRe.MatchString(s) {
		var d time.Duration
		for _, p := range partRe.FindAllStringSubmatch(s, -1) {
			n, _ := strconv.Atoi(p[1])
			switch p[2][0] {
			case 'w':
				d += time.Duration(n) * 7 * 24 * time.Hour
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

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// a calendar date, then (optionally) a time
	if date, rest, ok := parseDate(s, today); ok {
		hour, minute, err := parseClock(rest)
		if err != nil {
			return time.Time{}, err
		}
		t := date.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
		if !t.After(now) {
			if date.Before(today) || (rest == "" && date.Equal(today)) {
				// a date already gone this year: next year's
				t = t.AddDate(1, 0, 0)
			} else {
				return time.Time{}, errors.New("that time has passed")
			}
		}
		return t, nil
	}

	// a day, then (optionally) a time
	words := strings.Fields(s)
	day, dayGiven, weekday := 0, false, false
	switch w := words[0]; {
	case w == "today":
		dayGiven, words = true, words[1:]
	case w == "tomorrow" || w == "tmrw" || w == "tom" || w == "tmr":
		day, dayGiven, words = 1, true, words[1:]
	case w == "weekend" || (w == "this" && len(words) > 1 && words[1] == "weekend"):
		day = daysUntil(now.Weekday(), time.Saturday)
		dayGiven, weekday = true, true
		words = words[1:]
		if w == "this" {
			words = words[1:]
		}
	case w == "next" && len(words) > 1 && words[1] == "week":
		// next Monday (a week away at most)
		day, dayGiven = daysUntil(now.Weekday(), time.Monday), true
		if day == 0 {
			day = 7
		}
		words = words[2:]
	case w == "next" && len(words) > 1 && isWeekday(words[1]):
		// that day of next week (Monday-based weeks)
		target := weekdays[words[1]]
		day = daysUntil(now.Weekday(), target)
		if day == 0 || mondayIndex(target) > mondayIndex(now.Weekday()) {
			day += 7
		}
		dayGiven, words = true, words[2:]
	case isWeekday(w):
		day = daysUntil(now.Weekday(), weekdays[w])
		dayGiven, weekday, words = true, true, words[1:]
	}
	if len(words) > 0 && words[0] == "at" {
		words = words[1:]
	}
	rest := strings.Join(words, " ")
	if rest == "" && !dayGiven {
		return time.Time{}, ErrUnknown
	}
	hour, minute, err := parseClock(rest)
	if err != nil {
		return time.Time{}, err
	}
	t := today.AddDate(0, 0, day).Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	if !t.After(now) {
		// passed already: a plain time or today means tomorrow, a weekday
		// next week ("tomorrow" can't have passed)
		if weekday {
			t = t.AddDate(0, 0, 7)
		} else {
			t = t.AddDate(0, 0, 1)
		}
	}
	return t, nil
}

// daysUntil is how many days from one weekday to the next given one (0-6).
func daysUntil(from, to time.Weekday) int { return (int(to) - int(from) + 7) % 7 }

// mondayIndex counts weekdays from Monday (0) to Sunday (6).
func mondayIndex(d time.Weekday) int { return (int(d) + 6) % 7 }

// parseClock reads a time of day ("9am", "21:15", "noon"); "" is the
// default hour.
func parseClock(s string) (hour, minute int, err error) {
	if s == "" {
		return defaultHour, 0, nil
	}
	if h, ok := named[s]; ok {
		return h, 0, nil
	}
	m := clockRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, ErrUnknown
	}
	hour, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		minute, _ = strconv.Atoi(m[2])
	}
	switch ampm := m[3]; {
	case hour > 23 || minute > 59 || (ampm != "" && hour > 12):
		return 0, 0, ErrUnknown
	case hour == 0 && strings.HasPrefix(ampm, "p"):
		return 0, 0, ErrUnknown // "00:30pm" means nothing
	case strings.HasPrefix(ampm, "p") && hour < 12:
		hour += 12
	case strings.HasPrefix(ampm, "a") && hour == 12:
		hour = 0
	}
	return hour, minute, nil
}

// parseDate reads a calendar date at the start of s (this year's, unless
// a year is given) and returns it with the rest of s.
func parseDate(s string, today time.Time) (date time.Time, rest string, ok bool) {
	at := func(y int, mo time.Month, d int) (time.Time, bool) {
		t := time.Date(y, mo, d, 0, 0, 0, 0, today.Location())
		return t, t.Month() == mo && d >= 1 // rejects 31 feb
	}
	if m := isoRe.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		t, ok := at(y, time.Month(mo), d)
		return t, m[4], ok
	}
	if m := dayMonRe.FindStringSubmatch(s); m != nil {
		if mo, isMonth := months[m[2]]; isMonth {
			d, _ := strconv.Atoi(m[1])
			t, ok := at(today.Year(), mo, d)
			return t, m[3], ok
		}
	}
	if m := monDayRe.FindStringSubmatch(s); m != nil {
		if mo, isMonth := months[m[1]]; isMonth {
			d, _ := strconv.Atoi(m[2])
			t, ok := at(today.Year(), mo, d)
			return t, m[3], ok
		}
	}
	if m := slashRe.FindStringSubmatch(s); m != nil {
		d, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		if mo < 1 || mo > 12 {
			return time.Time{}, "", false
		}
		t, ok := at(today.Year(), time.Month(mo), d)
		return t, m[3], ok
	}
	return time.Time{}, "", false
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
	case y1 != y2:
		return t.Format("2 Jan 2006") + " " + clock
	}
	return t.Format("2 Jan") + " " + clock
}
