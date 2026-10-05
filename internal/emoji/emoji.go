// Package emoji lists the emoji for the picker, with names to search by.
package emoji

import "strings"

// Emoji is one emoji with its Unicode name and group.
type Emoji struct {
	Char, Name, Group string
}

// All returns every emoji, in the usual keyboard order.
func All() []Emoji { return all }

// Search returns the emoji whose name matches query, best first: the exact
// name, then names with it as a whole word, then names starting with it,
// then words starting with it, then names containing it. Every word of a
// multi-word query must match.
func Search(query string) []Emoji {
	q := strings.ToLower(strings.TrimSpace(query))
	words := strings.Fields(q)
	if len(words) == 0 {
		return all
	}
	var tiers [5][]Emoji
	for _, e := range all {
		if e.Char == strings.TrimSpace(query) {
			return []Emoji{e}
		}
		name := strings.ToLower(e.Name)
		ok := true
		for _, w := range words {
			if !strings.Contains(name, w) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		padded := " " + strings.NewReplacer(":", " ", ",", " ", "-", " ").Replace(name) + " "
		switch {
		case name == q:
			tiers[0] = append(tiers[0], e)
		case strings.Contains(padded, " "+q+" "):
			tiers[1] = append(tiers[1], e)
		case strings.HasPrefix(name, q):
			tiers[2] = append(tiers[2], e)
		case strings.Contains(padded, " "+words[0]):
			tiers[3] = append(tiers[3], e)
		default:
			tiers[4] = append(tiers[4], e)
		}
	}
	var out []Emoji
	for _, t := range tiers {
		out = append(out, t...)
	}
	return out
}
