package messages

import (
	"crypto/sha256"
	"testing"
)

func hashOf(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func TestPollVotesAreCounted(t *testing.T) {
	sm := receiptSM(t)
	text := "[POLL] Dinner?\n○ Pizza\n○ Dosa\n○ Both"
	addTestMsg(t, sm.db, Message{Id: "p1", ChatId: "g@g.us", Text: text, Timestamp: 100})
	sm.db.SetPollVote("p1", "111@s.whatsapp.net", [][]byte{hashOf("Pizza")}, 10)
	sm.db.SetPollVote("p1", "222@s.whatsapp.net", [][]byte{hashOf("Dosa")}, 10)
	sm.db.SetPollVote("p1", "", [][]byte{hashOf("Pizza")}, 10)
	// a changed vote replaces the earlier one; a stale one doesn't
	sm.db.SetPollVote("p1", "222@s.whatsapp.net", [][]byte{hashOf("Pizza")}, 20)
	sm.db.SetPollVote("p1", "222@s.whatsapp.net", [][]byte{hashOf("Both")}, 15)
	// taking a vote back
	sm.db.SetPollVote("p1", "333@s.whatsapp.net", [][]byte{hashOf("Both")}, 10)
	sm.db.SetPollVote("p1", "333@s.whatsapp.net", nil, 30)

	msgs := []Message{{Id: "p1", Text: text}}
	sm.attachPolls(msgs)
	p := msgs[0].Poll
	if p == nil || p.Question != "Dinner?" || len(p.Options) != 3 {
		t.Fatalf("poll: %+v", p)
	}
	if n := len(p.Options[0].Voters); n != 3 || !p.Options[0].Mine || p.Options[0].Voters[0].Name != "You" {
		t.Fatalf("pizza: %+v", p.Options[0])
	}
	if len(p.Options[1].Voters) != 0 || len(p.Options[2].Voters) != 0 {
		t.Fatalf("dosa/both: %+v", p.Options)
	}
	if p.Voters != 3 || p.Max != 1 {
		t.Fatalf("voters %d max %d", p.Voters, p.Max)
	}
}

func TestPollTextLimit(t *testing.T) {
	_, opts, max, ok := pollText("[POLL] When?\n○ Mon\n○ Tue\n(pick up to 2)")
	if !ok || len(opts) != 2 || max != 2 {
		t.Fatalf("%v %d %v", opts, max, ok)
	}
	if _, _, _, ok := pollText("hello"); ok {
		t.Fatal("not a poll")
	}
}
