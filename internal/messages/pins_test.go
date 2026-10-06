package messages

import (
	"testing"
	"time"
)

func TestPins(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	ms := now.UnixMilli()
	week := now.Add(7 * 24 * time.Hour).UnixMilli()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.SetPin("c@g.us", "m1", "", true, ms-2000, week))
	must(db.SetPin("c@g.us", "m2", "p@s.whatsapp.net", true, ms-1000, week))
	must(db.SetPin("c@g.us", "old", "", true, ms-9000, ms-1)) // expired
	must(db.SetPin("other@g.us", "x", "", true, ms, week))
	ids, err := db.PinnedIDs("c@g.us", now)
	must(err)
	if len(ids) != 2 || ids[0] != "m2" || ids[1] != "m1" {
		t.Fatalf("pins = %v, want newest first m2 m1", ids)
	}
	// an older unpin arriving late doesn't undo a newer pin
	must(db.SetPin("c@g.us", "m2", "", false, ms-5000, 0))
	// a newer unpin does
	must(db.SetPin("c@g.us", "m1", "", false, ms, 0))
	ids, _ = db.PinnedIDs("c@g.us", now)
	if len(ids) != 1 || ids[0] != "m2" {
		t.Fatalf("after unpins = %v, want m2", ids)
	}
}

func TestPinDuration(t *testing.T) {
	if pinDuration(0) != DefaultPinDuration || pinDuration(86400) != 24*time.Hour {
		t.Fatal("pin durations")
	}
}
