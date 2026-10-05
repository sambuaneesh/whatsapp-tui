package ui

import (
	"sort"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// bubbleCache keeps rendered message bubbles between redraws, so a redraw
// only renders the bubbles whose look changed (the selection moving, an
// image arriving, a new reaction) and reuses the rest. Entries a redraw
// doesn't use are dropped, so it holds at most the open chat.
type bubbleCache struct {
	cur, next map[uint64]cachedBubble
}

type cachedBubble struct {
	lines []string
	parts bubbleParts
}

func newBubbleCache() *bubbleCache {
	return &bubbleCache{cur: map[uint64]cachedBubble{}, next: map[uint64]cachedBubble{}}
}

// get returns the cached entry for key, keeping it for the next redraw.
func (c *bubbleCache) get(key uint64) (cachedBubble, bool) {
	b, ok := c.cur[key]
	if ok {
		c.next[key] = b
	}
	return b, ok
}

func (c *bubbleCache) put(key uint64, b cachedBubble) { c.next[key] = b }

// done ends a redraw: what it didn't use is forgotten.
func (c *bubbleCache) done() {
	c.cur, c.next = c.next, c.cur
	clear(c.next)
}

// keyer hashes the inputs of a rendered piece (FNV-1a, without
// allocating).
type keyer uint64

const (
	fnvOffset keyer = 14695981039346656037
	fnvPrime  keyer = 1099511628211
)

func (c *bubbleCache) keyer() keyer { return fnvOffset }

func (k keyer) str(s string) keyer {
	k = k.int(len(s))
	for i := 0; i < len(s); i++ {
		k = (k ^ keyer(s[i])) * fnvPrime
	}
	return k
}

func (k keyer) int(n int) keyer {
	for i := 0; i < 8; i++ {
		k = (k ^ keyer(byte(n>>(8*i)))) * fnvPrime
	}
	return k
}

func (k keyer) bool(v bool) keyer {
	if v {
		return k.int(1)
	}
	return k.int(0)
}

func (k keyer) sum() uint64 { return uint64(k) }

// bubbleKey hashes everything that changes how msg's bubble looks.
func (m Model) bubbleKey(msg messages.Message, showSender, selected bool, maxInner, width int) uint64 {
	k := m.bubbles.keyer().str("bubble").str(msg.Id).str(msg.Text).int(msg.Status).bool(msg.Edited).
		bool(msg.FromMe).bool(msg.Forwarded).str(msg.ContactId).str(msg.ContactShort).str(msg.ContactName).
		int(int(msg.Timestamp)).str(msg.MediaType).str(string(msg.Media)).int(msg.Deleted).
		bool(showSender).bool(selected).int(maxInner).int(width)
	if msg.QuotedID != "" {
		k = k.str(msg.QuotedID).str(msg.QuotedText).str(msg.QuotedSender).str(m.quotedSenderName(msg))
	}
	for _, r := range msg.Reactions {
		k = k.str(r.Sender).str(r.Emoji)
	}
	if len(msg.Mentions) > 0 {
		users := make([]string, 0, len(msg.Mentions))
		for u := range msg.Mentions {
			users = append(users, u)
		}
		sort.Strings(users)
		for _, u := range users {
			k = k.str(u).str(msg.Mentions[u])
		}
	}
	if m.search != nil {
		k = k.str(m.search.query)
	}
	// the picture: loading, failed or drawn (and which drawing)
	if meta, ok := msg.MediaMeta(); ok && m.img.enabled() {
		cols, rows := m.mediaCells(meta, maxInner)
		if e := m.img.get(imgKey{imgMessage, msg.Id, cols, rows}); e != nil {
			k = k.int(int(e.state)).str(e.text)
		}
	}
	return k.sum()
}
