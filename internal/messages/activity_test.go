package messages

import "testing"

func TestActivity(t *testing.T) {
	md := newTestDB(t)
	const base = 1_700_000_000 // seconds; reactions are stored in milliseconds
	add := func(m Message) {
		t.Helper()
		if err := md.AddMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	add(Message{Id: "mine", ChatId: "g@g.us", FromMe: true, Text: "dinner at 8?", Timestamp: base + 100})
	add(Message{Id: "reply", ChatId: "g@g.us", ContactShort: "Priya", Text: "yes!", QuotedID: "mine", Timestamp: base + 200})
	add(Message{Id: "ment", ChatId: "h@g.us", ContactShort: "Ravi", Text: "@919876 call me", Timestamp: base + 300})
	add(Message{Id: "not", ChatId: "h@g.us", ContactShort: "Ravi", Text: "@9198765 is someone else", Timestamp: base + 350})
	add(Message{Id: "all", ChatId: "h@g.us", ContactShort: "Sita", Text: "@all meeting now", Timestamp: base + 400})
	add(Message{Id: "other", ChatId: "h@g.us", ContactShort: "Sita", Text: "unrelated", Timestamp: base + 500})
	_ = md.SetReaction("mine", "91222@s.whatsapp.net", "❤️", (base+600)*1000) // ms
	_ = md.SetReaction("mine", "", "👍", (base+700)*1000)                      // your own: not activity
	_ = md.SetReaction("reply", "91333@s.whatsapp.net", "😂", (base+800)*1000) // not your message

	items, err := md.Activity([]string{"919876"}, 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Kind+":"+it.Msg.Id)
	}
	want := "reaction:mine,mention:all,mention:ment,reply:reply"
	if s := join(got); s != want {
		t.Fatalf("got %s\nwant %s", s, want)
	}
	if items[0].Emoji != "❤️" || items[0].Who != "91222@s.whatsapp.net" || items[0].Time != base+600 {
		t.Fatalf("reaction %+v", items[0])
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}
