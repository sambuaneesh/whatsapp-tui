package messages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Poll votes. A vote names the options it picks by their SHA-256, and comes
// encrypted with the poll's secret; each voter's latest vote is kept (an
// empty one takes their vote back). The options come from the poll's own
// text ("○ option" lines), so a vote that arrives before its poll still
// counts once the poll is there.

func (md *MessageDatabase) initPolls() error {
	_, err := md.db.Exec(`CREATE TABLE IF NOT EXISTS poll_votes (
		poll_id TEXT,
		voter TEXT,           -- "" for you
		options TEXT NOT NULL, -- hex SHA-256 of each picked option, comma-separated
		timestamp INTEGER NOT NULL DEFAULT 0, -- unix ms
		PRIMARY KEY (poll_id, voter)
	)`)
	return err
}

// SetPollVote records voter's vote (option hashes) on a poll; an older vote
// than the one kept is ignored.
func (md *MessageDatabase) SetPollVote(pollID, voter string, hashes [][]byte, ms int64) error {
	hs := make([]string, len(hashes))
	for i, h := range hashes {
		hs[i] = hex.EncodeToString(h)
	}
	_, err := md.db.Exec(`INSERT INTO poll_votes (poll_id, voter, options, timestamp) VALUES (?, ?, ?, ?)
		ON CONFLICT(poll_id, voter) DO UPDATE SET options = excluded.options, timestamp = excluded.timestamp
		WHERE excluded.timestamp >= poll_votes.timestamp`, pollID, voter, strings.Join(hs, ","), ms)
	return err
}

type pollVote struct {
	voter   string
	options []string // hex hashes
}

// pollVotes returns the votes on the given polls.
func (md *MessageDatabase) pollVotes(ids []string) (map[string][]pollVote, error) {
	out := map[string][]pollVote{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := md.db.Query(`SELECT poll_id, voter, options FROM poll_votes WHERE poll_id IN (?`+
		strings.Repeat(",?", len(ids)-1)+`) ORDER BY timestamp ASC`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, opts string
		var v pollVote
		if err := rows.Scan(&id, &v.voter, &opts); err != nil {
			return out, err
		}
		if opts != "" {
			v.options = strings.Split(opts, ",")
		}
		out[id] = append(out[id], v)
	}
	return out, rows.Err()
}

// Poll is a poll's options and who picked each.
type Poll struct {
	Question string
	Options  []PollOption
	Voters   int // people who voted for anything
	Max      int // how many options one person may pick (1 = single choice)
}

// PollOption is one answer and its voters.
type PollOption struct {
	Name   string
	Voters []PollVoter
	Mine   bool // you picked it
}

// PollVoter is someone who picked an option.
type PollVoter struct {
	User string // "" for you
	Name string
}

// pollText reads a poll's question, options and choice limit back from the
// text it is stored as.
func pollText(text string) (question string, options []string, max int, ok bool) {
	if !strings.HasPrefix(text, "[POLL] ") {
		return "", nil, 0, false
	}
	lines := strings.Split(text, "\n")
	question = strings.TrimPrefix(lines[0], "[POLL] ")
	max = 1
	for _, l := range lines[1:] {
		switch {
		case strings.HasPrefix(l, "○ "):
			options = append(options, strings.TrimPrefix(l, "○ "))
		case strings.HasPrefix(l, "(pick up to "):
			var n int
			if _, err := fmt.Sscanf(l, "(pick up to %d)", &n); err == nil && n > 1 {
				max = n
			}
		}
	}
	return question, options, max, len(options) > 0
}

func optionHash(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:])
}

// tallyPoll counts votes against a poll's options. names resolves a voter
// to a name ("" voter is you).
func tallyPoll(text string, votes []pollVote, names func(string) string) *Poll {
	q, opts, max, ok := pollText(text)
	if !ok {
		return nil
	}
	p := &Poll{Question: q, Max: max, Options: make([]PollOption, len(opts))}
	idx := map[string]int{}
	for i, o := range opts {
		p.Options[i].Name = o
		idx[optionHash(o)] = i
	}
	for _, v := range votes {
		counted := false
		for _, h := range v.options {
			i, ok := idx[h]
			if !ok {
				continue
			}
			counted = true
			p.Options[i].Voters = append(p.Options[i].Voters, PollVoter{User: v.voter, Name: names(v.voter)})
			if v.voter == "" {
				p.Options[i].Mine = true
			}
		}
		if counted {
			p.Voters++
		}
	}
	for i := range p.Options {
		p.Options[i].Voters = sortedVoters(p.Options[i].Voters)
	}
	return p
}

// attachPolls fills in Message.Poll for the polls among msgs.
func (sm *SessionManager) attachPolls(msgs []Message) {
	var ids []string
	for _, m := range msgs {
		if strings.HasPrefix(m.Text, "[POLL] ") {
			ids = append(ids, m.Id)
		}
	}
	if len(ids) == 0 {
		return
	}
	votes, err := sm.db.pollVotes(ids)
	if err != nil {
		sm.debugf("load poll votes: %v", err)
	}
	ctx := context.Background()
	names := map[string]string{}
	name := func(u string) string {
		if u == "" {
			return "You"
		}
		if n, ok := names[u]; ok {
			return n
		}
		n := u
		if jid, err := types.ParseJID(u); err == nil {
			if cn := sm.contactName(ctx, jid); cn != "" {
				n = cn
			}
		}
		names[u] = n
		return n
	}
	for i := range msgs {
		if strings.HasPrefix(msgs[i].Text, "[POLL] ") {
			msgs[i].Poll = tallyPoll(msgs[i].Text, votes[msgs[i].Id], name)
		}
	}
}

// voterOf is who cast a vote, as stored: "" for you.
func (sm *SessionManager) voterOf(ctx context.Context, sender types.JID, fromMe bool) string {
	if fromMe {
		return ""
	}
	return sm.pnForLID(ctx, sender.ToNonAD()).String()
}

// handlePollVote decrypts a vote that just came in and stores it. It must
// see the event as received (before canonicalSource): the vote's key is
// derived from those addresses.
func (sm *SessionManager) handlePollVote(evt *events.Message, quiet bool) bool {
	pu := evt.Message.GetPollUpdateMessage()
	if pu == nil {
		return false
	}
	client := sm.getClient()
	if client == nil {
		return true
	}
	ctx := context.Background()
	vote, err := client.DecryptPollVote(ctx, evt)
	if err != nil && !evt.Info.SenderAlt.IsEmpty() {
		// the poll's secret may be filed under the voter's other address
		alt := *evt
		alt.Info.Sender, alt.Info.SenderAlt = evt.Info.SenderAlt, evt.Info.Sender
		vote, err = client.DecryptPollVote(ctx, &alt)
	}
	if err != nil {
		sm.debugf("poll vote %s: %v", evt.Info.ID, err)
		return true
	}
	ms := pu.GetSenderTimestampMS()
	if ms == 0 {
		ms = evt.Info.Timestamp.UnixMilli()
	}
	src := evt.Info.MessageSource
	sm.canonicalSource(ctx, &src)
	voter := sm.voterOf(ctx, src.Sender, src.IsFromMe)
	if err := sm.db.SetPollVote(pu.GetPollCreationMessageKey().GetID(), voter, vote.GetSelectedOptions(), ms); err != nil {
		sm.debugf("store poll vote: %v", err)
		return true
	}
	if !quiet {
		sm.refreshIfOpen(src.Chat.String())
	}
	return true
}

// storeHistoryPollVotes keeps the (already decrypted) votes history sync
// carries on a poll.
func (sm *SessionManager) storeHistoryPollVotes(chat types.JID, pollID string, ups []*waWeb.PollUpdate) {
	ctx := context.Background()
	for _, u := range ups {
		k := u.GetPollUpdateMessageKey()
		voter := ""
		if !k.GetFromMe() {
			who := k.GetParticipant()
			if who == "" {
				who = k.GetRemoteJID()
			}
			if who == "" {
				who = chat.String()
			}
			jid, err := types.ParseJID(who)
			if err != nil {
				continue
			}
			voter = sm.voterOf(ctx, jid, false)
		}
		if err := sm.db.SetPollVote(pollID, voter, u.GetVote().GetSelectedOptions(), u.GetSenderTimestampMS()); err != nil {
			sm.debugf("store poll vote: %v", err)
		}
	}
}

// sortedVoters lists a poll's voters by name, you first.
func sortedVoters(vs []PollVoter) []PollVoter {
	out := append([]PollVoter(nil), vs...)
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].User == "") != (out[j].User == "") {
			return out[i].User == ""
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}
