package ui

import "unicode"

// Fuzzy matching for the palette, the way VS Code's Quick Open and fzf
// do it: the query's letters must appear in order ("hrsh" finds "Hari
// Shankar"), and a match scores higher when its letters start words, run
// together or come early in the name.

const (
	scoreMatch       = 16
	scoreWordStart   = 10 // after a space, punctuation, or a lower→upper change
	scoreFirstChar   = 12 // the name's first letter
	scoreConsecutive = 8  // right after the previous matched letter
	penaltyGapStart  = 3
	penaltyGapExtend = 1
)

// fuzzyMatch reports whether q (lowercase) matches name (lowered is its
// lowercase runes, same length as name's runes), how well, and which
// runes matched.
func fuzzyMatch(q, lowered []rune, name []rune) (score int, pos []int, ok bool) {
	return fuzzy(q, lowered, name, true)
}

// fuzzyScore is fuzzyMatch without the positions (no allocation): for
// ranking thousands of names, then fuzzyMatch for the few on screen.
func fuzzyScore(q, lowered []rune, name []rune) (int, bool) {
	s, _, ok := fuzzy(q, lowered, name, false)
	return s, ok
}

func fuzzy(q, lowered []rune, name []rune, wantPos bool) (score int, pos []int, ok bool) {
	if len(q) == 0 {
		return 0, nil, true
	}
	if len(q) > len(lowered) {
		return 0, nil, false
	}
	// forward: the earliest end of a match
	qi, end := 0, -1
	for i, r := range lowered {
		if r == q[qi] {
			qi++
			if qi == len(q) {
				end = i
				break
			}
		}
	}
	if end < 0 {
		return 0, nil, false
	}
	// backward from there: the latest start, which gives the tightest
	// window (fzf's v1 algorithm)
	qi = len(q) - 1
	start := end
	for i := end; i >= 0; i-- {
		if lowered[i] == q[qi] {
			qi--
			if qi < 0 {
				start = i
				break
			}
		}
	}
	best, bestPos := scoreWindow(q, lowered, name, start, end, wantPos)

	// a word that starts with the query beats letters spread through a
	// tighter window ("sam" should find "Sam" in "Usha Sam", not "usha sAM")
	for s := 0; s+len(q) <= len(lowered); s++ {
		if s > 0 && !wordStart(name, s) {
			continue
		}
		if !runesHavePrefix(lowered[s:], q) {
			continue
		}
		if sc, p := scoreWindow(q, lowered, name, s, s+len(q)-1, wantPos); sc > best {
			best, bestPos = sc, p
		}
		break
	}
	return best, bestPos, true
}

// scoreWindow matches q greedily inside lowered[start..end] and scores it.
func scoreWindow(q, lowered, name []rune, start, end int, wantPos bool) (int, []int) {
	var pos []int
	if wantPos {
		pos = make([]int, 0, len(q))
	}
	score, qi, prev, inGap := 0, 0, -2, false
	for i := start; i <= end && qi < len(q); i++ {
		if lowered[i] != q[qi] {
			if prev >= 0 {
				if inGap {
					score -= penaltyGapExtend
				} else {
					score -= penaltyGapStart
				}
				inGap = true
			}
			continue
		}
		score += scoreMatch
		switch {
		case i == 0:
			score += scoreFirstChar
		case wordStart(name, i):
			score += scoreWordStart
		}
		if prev == i-1 {
			score += scoreConsecutive
		}
		if wantPos {
			pos = append(pos, i)
		}
		prev, inGap = i, false
		qi++
	}
	// earlier matches rank a little higher; shorter names too
	score -= min(start, 10)
	score -= min(len(lowered)/8, 4)
	return score, pos
}

func wordStart(name []rune, i int) bool {
	if i == 0 {
		return true
	}
	p, c := name[i-1], name[i]
	if !unicode.IsLetter(p) && !unicode.IsDigit(p) {
		return unicode.IsLetter(c) || unicode.IsDigit(c)
	}
	return unicode.IsLower(p) && unicode.IsUpper(c)
}

func runesHavePrefix(s, prefix []rune) bool {
	if len(prefix) > len(s) {
		return false
	}
	for i, r := range prefix {
		if s[i] != r {
			return false
		}
	}
	return true
}

// lowerRunes lowercases s rune by rune, keeping positions aligned with
// []rune(s) (strings.ToLower can change the length).
func lowerRunes(s string) (orig, lowered []rune) {
	orig = []rune(s)
	lowered = make([]rune, len(orig))
	for i, r := range orig {
		lowered[i] = unicode.ToLower(r)
	}
	return orig, lowered
}
