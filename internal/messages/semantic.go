package messages

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Srindot/whatsapp-tui/internal/semantic"
)

// Search by meaning: messages are embedded in the background by a local
// model (see internal/semantic); a search across all chats adds messages
// close in meaning to the words that matched. Off when no model is set up.

const (
	embedBatch       = 64               // messages per request to the model
	embedMinText     = 12               // shorter texts say too little to embed
	similarMax       = 20               // messages added by meaning per search
	similarThreshold = 0.45             // how close in meaning they must be
	embedSoon        = 15 * time.Minute // new messages are indexed at most this often
	busyEvery        = 30               // during a long index, check the GPU every this many batches
	reprobeEvery     = 5 * time.Minute  // a search retries a model that wasn't running
)

type semanticState struct {
	mu      sync.Mutex
	gpuBusy func() bool       // set: indexing waits while it says so (gaming)
	cfg     semantic.Embedder // the model set up (maybe not running yet)
	probed  time.Time         // when cfg was last tried
	e       semantic.Embedder
	ready   bool // the model answered
	running bool // a pass is indexing
	timer   *time.Timer
}

// StartSemantic turns on search by meaning with this model: if it answers,
// messages get indexed in the background (newest first).
func (sm *SessionManager) StartSemantic(e semantic.Embedder) {
	sm.startSemantic(e, nil)
}

// StartSemanticGentle is StartSemantic, but background indexing waits while
// busy() says the GPU is in use by something else.
func (sm *SessionManager) StartSemanticGentle(e semantic.Embedder, busy func() bool) {
	sm.startSemantic(e, busy)
}

func (sm *SessionManager) startSemantic(e semantic.Embedder, busy func() bool) {
	s := &sm.semantic
	s.mu.Lock()
	s.gpuBusy = busy
	s.cfg, s.probed = e, time.Now()
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := e.Embed(ctx, []string{"ping"}, true); err != nil {
			sm.debugf("search by meaning off: %v", err)
			return
		}
		s := &sm.semantic
		s.mu.Lock()
		s.e, s.ready = e, true
		s.mu.Unlock()
		sm.indexMeaning()
	}()
}

// indexMeaningSoon indexes new messages a little later (batched).
func (sm *SessionManager) indexMeaningSoon() {
	s := &sm.semantic
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready || s.timer != nil {
		return
	}
	s.timer = time.AfterFunc(embedSoon, func() {
		s.mu.Lock()
		s.timer = nil
		s.mu.Unlock()
		sm.indexMeaning()
	})
}

// indexMeaning embeds every message that isn't yet, in batches.
func (sm *SessionManager) indexMeaning() {
	s := &sm.semantic
	s.mu.Lock()
	if !s.ready || s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	e, busy := s.e, s.gpuBusy
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	for batch := 0; ; batch++ {
		// the GPU is someone else's (a game): try again later
		if busy != nil && batch%busyEvery == 0 {
			if batch > 0 {
				time.Sleep(time.Second) // let our own work drop out of the reading
			}
			if busy() {
				sm.debugf("index by meaning: GPU busy, later")
				sm.indexMeaningSoon()
				return
			}
		}
		msgs, err := sm.db.UnembeddedMessages(embedBatch)
		if err != nil || len(msgs) == 0 {
			return
		}
		texts := make([]string, len(msgs))
		for i, m := range msgs {
			texts[i] = embedText(m)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		vecs, err := e.Embed(ctx, texts, false)
		cancel()
		if err != nil {
			sm.debugf("index by meaning: %v", err)
			return // try again with the next new messages
		}
		vs := make(map[string][]byte, len(msgs))
		for i, m := range msgs {
			vs[m.Id] = semantic.Quantize(vecs[i])
		}
		if err := sm.db.SaveEmbeddings(vs); err != nil {
			sm.debugf("index by meaning: %v", err)
			return
		}
		time.Sleep(20 * time.Millisecond) // stay in the background
	}
}

// embedText is what's embedded for a message: its text without media tags.
func embedText(m Message) string {
	t := m.Text
	if strings.HasPrefix(t, "[") {
		if i := strings.Index(t, "]"); i > 0 {
			t = strings.TrimSpace(t[i+1:])
		}
	}
	return t
}

// similar finds messages close in meaning to query (best first).
func (sm *SessionManager) similar(ctx context.Context, query string) []semantic.Scored {
	s := &sm.semantic
	s.mu.Lock()
	e, ready := s.e, s.ready
	retry := !ready && s.cfg != nil && time.Since(s.probed) > reprobeEvery
	cfg := s.cfg
	s.mu.Unlock()
	if retry {
		sm.startSemantic(cfg, s.gpuBusy) // the model may have been started since
	}
	if !ready || len(strings.TrimSpace(query)) < 3 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	vecs, err := e.Embed(ctx, []string{query}, true)
	if err != nil {
		return nil
	}
	found, err := sm.db.SearchEmbeddings(semantic.Quantize(vecs[0]), similarMax*2)
	if err != nil {
		return nil
	}
	var out []semantic.Scored
	for _, f := range found {
		if f.Score >= similarThreshold {
			out = append(out, f)
		}
	}
	return out
}

// ---------- storage ----------

func (md *MessageDatabase) initEmbeddings() {
	md.db.Exec(`CREATE TABLE IF NOT EXISTS embeddings (msg_id TEXT PRIMARY KEY, vec BLOB)`)
}

// UnembeddedMessages returns up to n messages with text worth embedding
// that have no vector yet, newest first.
func (md *MessageDatabase) UnembeddedMessages(n int) ([]Message, error) {
	rows, err := md.db.Query(`SELECT `+msgColumns+` FROM messages m
		WHERE length(text) >= ? AND text NOT LIKE '🚫%' AND text NOT LIKE '[REACTION]%'
		AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.msg_id = m.id)
		AND m.chat_id NOT IN (SELECT jid FROM noindex)
		ORDER BY timestamp DESC LIMIT ?`, embedMinText, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectMessages(rows)
}

// SaveEmbeddings stores vectors (one transaction).
func (md *MessageDatabase) SaveEmbeddings(vs map[string][]byte) error {
	tx, err := md.db.Begin()
	if err != nil {
		return err
	}
	for id, v := range vs {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO embeddings (msg_id, vec) VALUES (?, ?)`, id, v); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SearchEmbeddings returns the n messages whose vectors are closest to q.
func (md *MessageDatabase) SearchEmbeddings(q []byte, n int) ([]semantic.Scored, error) {
	rows, err := md.db.Query(`SELECT msg_id, vec FROM embeddings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	top := semantic.NewTop(n)
	for rows.Next() {
		var id string
		var v []byte
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		top.Add(id, semantic.Similarity(q, v))
	}
	best := top.Best()
	sort.SliceStable(best, func(i, j int) bool { return best[i].Score > best[j].Score })
	return best, rows.Err()
}
