package emoji

import "testing"

func TestSearch(t *testing.T) {
	if len(All()) < 1500 {
		t.Fatalf("only %d emoji", len(All()))
	}
	for q, want := range map[string]string{"fire": "🔥", "red heart": "❤️", "thumbs up": "👍", "FIRE": "🔥", "🔥": "🔥"} {
		if got := Search(q); len(got) == 0 || got[0].Char != want {
			t.Errorf("%q: first %v, want %s", q, got[:min(len(got), 3)], want)
		}
	}
	// prefix matches before matches inside a name
	got := Search("cat")
	if len(got) == 0 || got[0].Name[:3] != "cat" {
		t.Errorf("cat: %v", got[:3])
	}
	if len(Search("zzzqqq")) != 0 {
		t.Error("nonsense matched")
	}
}
