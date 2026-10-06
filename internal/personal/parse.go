package personal

import (
	"regexp"
	"strings"
	"time"

	"github.com/Srindot/whatsapp-tui/internal/when"
)

// Typing a task the way you'd say it:
//
//	send report to Arjun by 5pm      due today 17:00
//	dentist fri 10am                 due Friday 10:00
//	pay rent 1st oct                 due 1 Oct (the day; no reminder)
//	buy milk tomorrow #home          due tomorrow, tagged home
//	call the bank !                  important
//	shopping: eggs                   in the Shopping list
//
// Several lines make a checklist: the first is its title, the rest its
// items ("- ", "* ", "[ ]" or "- [x]" in front are fine).

// Parsed is a task read from text.
type Parsed struct {
	Text      string
	Due       time.Time // zero: no date
	DueTime   bool      // Due has a time of day
	Important bool
	Tags      []string
	List      string // "shopping: eggs" → "shopping" (only if asked to look for one)
	Items     []Parsed
	Done      bool // "- [x] …" in a checklist
}

var (
	tagRe = regexp.MustCompile(`(?:^|\s)#([\p{L}\p{N}_\-/]+)`)
	// a time of day in the phrase: 5pm, 17:30, at 9, noon, tonight, in 2h…
	clockRe  = regexp.MustCompile(`\d\s*(am|pm|a|p)\b|\d:\d\d|\b(noon|midnight|morning|afternoon|evening|tonight|eod|end of day)\b|^(in\s+)?\d+\s*(m|min|mins|minutes?|h|hrs?|hours?)\b`)
	bulletRe = regexp.MustCompile(`^\s*(?:[-*•]\s+)?(?:\[( |x|X)\]\s*|[☐☑✅]\s*)?`)
	digitsRe = regexp.MustCompile(`^\d+$`)
)

// Words that are weekdays but also everyday words: only dates after
// "on", "by", "next", "this", "till" or "until".
var ambiguousDays = map[string]bool{"sun": true, "sat": true, "wed": true, "mon": true, "thu": true, "tue": true,
	"tues": true, "thur": true, "thurs": true, "may": true, "march": true}

// Words that may come before a date and go with it.
var dateLead = map[string]bool{"by": true, "on": true, "at": true, "due": true, "till": true, "until": true, "before": true}

// ParseTask reads a task (or checklist, over several lines) from text.
// withList looks for a "list: " prefix (lists names it may use).
func ParseTask(text string, now time.Time, lists []string) Parsed {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	head := parseLine(lines[0], now)
	if len(lists) > 0 {
		if name, rest, ok := strings.Cut(head.Text, ":"); ok && rest != "" {
			for _, l := range lists {
				if strings.EqualFold(strings.TrimSpace(name), l) {
					head.List = l
					head.Text = strings.TrimSpace(rest)
					break
				}
			}
		}
	}
	if len(lines) > 1 {
		head.Text = strings.TrimSuffix(strings.TrimSpace(head.Text), ":")
		for _, l := range lines[1:] {
			if strings.TrimSpace(l) == "" {
				continue
			}
			m := bulletRe.FindStringSubmatch(l)
			item := parseLine(l[len(m[0]):], now)
			item.Done = m[1] == "x" || m[1] == "X" || strings.ContainsAny(m[0], "☑✅")
			if item.Text != "" {
				head.Items = append(head.Items, item)
			}
		}
	}
	return head
}

func parseLine(line string, now time.Time) Parsed {
	var p Parsed
	line = strings.TrimSpace(line)
	// tags
	for _, m := range tagRe.FindAllStringSubmatch(line, -1) {
		p.Tags = append(p.Tags, strings.ToLower(m[1]))
	}
	line = strings.TrimSpace(tagRe.ReplaceAllString(line, " "))
	// "!" alone (or "!!") marks it important
	words := strings.Fields(line)
	kept := words[:0]
	for _, w := range words {
		if strings.Trim(w, "!") == "" {
			p.Important = true
			continue
		}
		kept = append(kept, w)
	}
	words = kept
	// a date at the end, else at the start (the longest that reads)
	if t, hasTime, n, ok := dateAtEnd(words, now); ok {
		p.Due, p.DueTime = t, hasTime
		words = words[:len(words)-n]
	} else if t, hasTime, n, ok := dateAtStart(words, now); ok {
		p.Due, p.DueTime = t, hasTime
		words = words[n:]
	}
	p.Text = strings.Join(words, " ")
	return p
}

// FindDate reads a date or time from all of s (for "t tomorrow 9am").
func FindDate(s string, now time.Time) (t time.Time, hasTime, ok bool) {
	words := strings.Fields(strings.ToLower(s))
	if len(words) == 0 {
		return time.Time{}, false, false
	}
	if dateLead[words[0]] {
		words = words[1:]
	}
	return readDate(words, now, true)
}

const maxDateWords = 5

func dateAtEnd(words []string, now time.Time) (time.Time, bool, int, bool) {
	for n := min(maxDateWords, len(words)); n >= 1; n-- {
		if n == len(words) {
			continue // the whole thing is a date: nothing left to do
		}
		phrase := lower(words[len(words)-n:])
		before := strings.ToLower(words[len(words)-n-1])
		if t, hasTime, ok := readDate(phrase, now, dateLead[before] || before == "next" || before == "this"); ok {
			if dateLead[before] && len(words)-n-1 > 0 {
				n++ // "by 5pm": the "by" goes too
			}
			return t, hasTime, n, true
		}
	}
	return time.Time{}, false, 0, false
}

func dateAtStart(words []string, now time.Time) (time.Time, bool, int, bool) {
	for n := min(maxDateWords, len(words)-1); n >= 1; n-- {
		lead := 0
		if dateLead[strings.ToLower(words[0])] {
			lead = 1
		}
		if n+lead >= len(words) {
			continue
		}
		if t, hasTime, ok := readDate(lower(words[lead:lead+n]), now, lead == 1); ok {
			return t, hasTime, n + lead, true
		}
	}
	return time.Time{}, false, 0, false
}

func lower(ws []string) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = strings.ToLower(strings.Trim(w, ",.?;"))
	}
	return out
}

// readDate reads words as a date: sure is true when they followed "by",
// "on"…, so bare numbers ("at 5") and everyday words ("sun") count.
func readDate(words []string, now time.Time, sure bool) (time.Time, bool, bool) {
	if len(words) > 1 && dateLead[words[0]] {
		words, sure = words[1:], true // "at 5", "by friday"
	}
	phrase := strings.Join(words, " ")
	if !sure {
		if len(words) == 1 && (digitsRe.MatchString(words[0]) || ambiguousDays[words[0]]) {
			return time.Time{}, false, false // "buy 2", "enjoy the sun"
		}
		if len(words) > 1 && ambiguousDays[words[0]] && !clockRe.MatchString(phrase) {
			return time.Time{}, false, false
		}
	}
	last := words[len(words)-1]
	hasTime := clockRe.MatchString(phrase) || (sure && digitsRe.MatchString(last))
	if sure && digitsRe.MatchString(last) && len(last) == 1 && last >= "1" && last <= "7" {
		phrase += "pm" // "at 5" means the evening, not 5 in the morning
	}
	from := now
	if !hasTime {
		// a day ("today", "fri"): read it from midnight, so today stays today
		y, m, d := now.Date()
		from = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	}
	t, err := when.Parse(phrase, from)
	if err != nil {
		return time.Time{}, false, false
	}
	if !hasTime {
		y, m, d := t.Date()
		t = time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	}
	return t, hasTime, true
}
