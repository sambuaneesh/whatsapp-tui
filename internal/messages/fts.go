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

var ftsTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
		INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text); END`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text); END`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE OF text ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
		INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text); END`,
}

// initFTS sets up (or safely disables) the index; md.fts says which.
func (md *MessageDatabase) initFTS() {
	md.db.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`)
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
