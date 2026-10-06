package messages

import (
	"database/sql"
	"fmt"
	"strings"
)

// Full-text search: an FTS5 index of message text, kept current by
// triggers, finds every word of a query in any order, word beginnings
// included ("flat addr" finds "address of the flat"). It needs SQLite built
// with FTS5 (go build -tags sqlite_fts5, as the Makefile does); without it
// search falls back to plain substring matching.
//
// Safety: if a build without FTS5 opens a database whose triggers feed the
// index, every insert would fail. So without FTS5 the triggers are
// dropped (and the index marked stale), and with it they're restored and a
// stale index rebuilt.

// Chats you turned indexing off for (noindex) stay out of it: their
// messages are never added, so never deleted from it either.
var ftsTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages
		WHEN new.chat_id NOT IN (SELECT jid FROM noindex) BEGIN
		INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text); END`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages
		WHEN old.chat_id NOT IN (SELECT jid FROM noindex) BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text); END`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE OF text ON messages
		WHEN old.chat_id NOT IN (SELECT jid FROM noindex) BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
		INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text); END`,
}

// ftsTriggerVersion changes when the triggers do, so old ones are replaced.
const ftsTriggerVersion = "2"

// initFTS sets up (or safely disables) the index; md.fts says which.
func (md *MessageDatabase) initFTS() {
	md.db.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`)
	md.db.Exec(`CREATE TABLE IF NOT EXISTS noindex (jid TEXT PRIMARY KEY)`)
	if _, err := md.db.Exec(`CREATE VIRTUAL TABLE temp.fts_probe USING fts5(x)`); err != nil {
		// no FTS5 in this build: never let the triggers break inserts
		for _, t := range []string{"messages_fts_ai", "messages_fts_ad", "messages_fts_au"} {
			md.db.Exec(`DROP TRIGGER IF EXISTS ` + t)
		}
		md.db.Exec(`INSERT INTO meta (key, value) VALUES ('fts_built', '0')
			ON CONFLICT(key) DO UPDATE SET value = '0'`)
		md.fts = false
		return
	}
	md.db.Exec(`DROP TABLE temp.fts_probe`)
	if _, err := md.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(text,
		content='messages', content_rowid='rowid', tokenize='unicode61 remove_diacritics 2')`); err != nil {
		md.fts = false
		return
	}
	var tv string
	md.db.QueryRow(`SELECT value FROM meta WHERE key = 'fts_triggers'`).Scan(&tv)
	if tv != ftsTriggerVersion {
		for _, t := range []string{"messages_fts_ai", "messages_fts_ad", "messages_fts_au"} {
			md.db.Exec(`DROP TRIGGER IF EXISTS ` + t)
		}
		md.db.Exec(`INSERT INTO meta (key, value) VALUES ('fts_triggers', ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, ftsTriggerVersion)
	}
	for _, t := range ftsTriggers {
		if _, err := md.db.Exec(t); err != nil {
			md.fts = false
			return
		}
	}
	var built string
	md.db.QueryRow(`SELECT value FROM meta WHERE key = 'fts_built'`).Scan(&built)
	if built != "1" {
		// new, or stale from a build without FTS5: index everything
		if _, err := md.db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES ('rebuild')`); err != nil {
			md.fts = false
			return
		}
		md.db.Exec(`INSERT INTO meta (key, value) VALUES ('fts_built', '1')
			ON CONFLICT(key) DO UPDATE SET value = '1'`)
	}
	md.fts = true
}

// ftsQuery turns what was typed into an FTS5 query: every word, as a word
// beginning ("flat addr" -> "flat"* "addr"*).
func ftsQuery(s string) string {
	var parts []string
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, `"*^:()`)
		if w == "" {
			continue
		}
		parts = append(parts, `"`+strings.ReplaceAll(w, `"`, `""`)+`"*`)
	}
	return strings.Join(parts, " ")
}

// searchFTS finds messages with every word of keyword (newest first).
func (md *MessageDatabase) searchFTS(chatID, keyword string, limit int) ([]Message, error) {
	q := ftsQuery(keyword)
	if q == "" {
		return nil, nil
	}
	var rows *sql.Rows
	var err error
	match := `rowid IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)`
	if chatID != "" {
		rows, err = md.db.Query(`SELECT `+msgColumns+` FROM messages WHERE chat_id = ? AND `+match+`
			ORDER BY timestamp DESC LIMIT ?`, chatID, q, limit)
	} else {
		rows, err = md.db.Query(`SELECT `+msgColumns+` FROM messages WHERE `+match+`
			ORDER BY timestamp DESC LIMIT ?`, q, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()
	return collectMessages(rows)
}

// Indexed reports whether a chat's messages are indexed for search (they
// are unless you turned it off).
func (md *MessageDatabase) Indexed(jid string) bool {
	var n int
	md.db.QueryRow(`SELECT COUNT(*) FROM noindex WHERE jid = ?`, jid).Scan(&n)
	return n == 0
}

// NotIndexed lists the chats indexing is off for.
func (md *MessageDatabase) NotIndexed() ([]string, error) {
	rows, err := md.db.Query(`SELECT jid FROM noindex ORDER BY jid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// SetIndexed turns search indexing on or off for a chat. Off takes its
// messages out of the word index and drops their meaning vectors; on puts
// them back in the word index (meaning vectors come back as the
// background indexer gets to them).
func (md *MessageDatabase) SetIndexed(jid string, on bool) error {
	if md.Indexed(jid) == on {
		return nil
	}
	tx, err := md.db.Begin()
	if err != nil {
		return err
	}
	fail := func(err error) error { tx.Rollback(); return err }
	if on {
		if _, err := tx.Exec(`DELETE FROM noindex WHERE jid = ?`, jid); err != nil {
			return fail(err)
		}
		if md.fts {
			if _, err := tx.Exec(`INSERT INTO messages_fts(rowid, text) SELECT rowid, text FROM messages WHERE chat_id = ?`, jid); err != nil {
				return fail(err)
			}
		}
	} else {
		if md.fts {
			if _, err := tx.Exec(`INSERT INTO messages_fts(messages_fts, rowid, text)
				SELECT 'delete', rowid, text FROM messages WHERE chat_id = ?`, jid); err != nil {
				return fail(err)
			}
		}
		if _, err := tx.Exec(`DELETE FROM embeddings WHERE msg_id IN (SELECT id FROM messages WHERE chat_id = ?)`, jid); err != nil {
			return fail(err)
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO noindex (jid) VALUES (?)`, jid); err != nil {
			return fail(err)
		}
	}
	return tx.Commit()
}

// SetChatIndexed turns search indexing on or off for a chat.
func (sm *SessionManager) SetChatIndexed(jid string, on bool) error {
	if err := sm.db.SetIndexed(jid, on); err != nil {
		return err
	}
	if on {
		sm.indexMeaningSoon()
	}
	return nil
}

// ChatIndexed reports whether a chat is indexed for search.
func (sm *SessionManager) ChatIndexed(jid string) bool { return sm.db.Indexed(jid) }

// NotIndexedChats lists the chats indexing is off for.
func (sm *SessionManager) NotIndexedChats() []string {
	list, _ := sm.db.NotIndexed()
	return list
}
