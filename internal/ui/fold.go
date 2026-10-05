package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Search matching, the same way as the full-text index: every word of the
// query, anywhere, in any order, accents ignored ("cafe" finds "Café"),
// case ignored unless the query has capitals (vim's smartcase).

// queryWords splits a query into words to find, and whether to ignore case.
func queryWords(query string) (words []string, fold bool) {
	_, fold = smartCase(query)
	for _, w := range strings.Fields(query) {
		f, _ := foldText(w, fold)
		words = append(words, f)
	}
	return words, fold
}

// foldText strips accents (and lowercases, if lower) rune by rune; off maps
// each byte of the result to the byte in s it came from (plus one past the
// end).
func foldText(s string, lower bool) (string, []int) {
	var b strings.Builder
	off := make([]int, 0, len(s)+1)
	for i, r := range s {
		base := r
		if r >= 0x80 {
			if d := norm.NFD.String(string(r)); d != "" {
				if f, _ := utf8.DecodeRuneInString(d); !unicode.Is(unicode.Mn, f) {
					base = f
				}
			}
		}
		if lower {
			base = unicode.ToLower(base)
		}
		n := b.Len()
		b.WriteRune(base)
		for j := n; j < b.Len(); j++ {
			off = append(off, i)
		}
	}
	return b.String(), append(off, len(s))
}

// matchesQuery reports whether text has every word of query.
func matchesQuery(text, query string) bool {
	words, fold := queryWords(query)
	if len(words) == 0 {
		return false
	}
	hay, _ := foldText(text, fold)
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// matchRanges are the byte ranges of text where query's words occur, in
// order and not overlapping.
func matchRanges(text, query string) [][2]int {
	words, fold := queryWords(query)
	if len(words) == 0 {
		return nil
	}
	hay, off := foldText(text, fold)
	var out [][2]int
	for pos := 0; pos < len(hay); {
		best, bestLen := -1, 0
		for _, w := range words {
			if w == "" {
				continue
			}
			if i := strings.Index(hay[pos:], w); i >= 0 && (best < 0 || i < best || i == best && len(w) > bestLen) {
				best, bestLen = i, len(w)
			}
		}
		if best < 0 {
			break
		}
		a, b := pos+best, pos+best+bestLen
		out = append(out, [2]int{off[a], off[b]})
		pos = b
	}
	return out
}
