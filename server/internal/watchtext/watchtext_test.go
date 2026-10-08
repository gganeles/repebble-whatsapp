package watchtext

import (
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello…"},
		{"ääääää", 7, "ää…"},
		{"abc", 2, "ab"},
	}
	for _, c := range cases {
		got := Truncate(c.in, c.max)
		if got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
		if len(got) > c.max || !utf8.ValidString(got) {
			t.Errorf("Truncate(%q, %d) = %q: too long or invalid", c.in, c.max, got)
		}
	}
}

func TestClean(t *testing.T) {
	cases := map[string]string{
		"see you\n\n at 8 👍": "see you at 8 (y)",
		"love ❤️ it":         "love <3 it",
		"thumbs 👍🏽":          "thumbs (y)",
		"party 🦄 time":       "party time",
		"family 👨‍👩‍👧 photo": "family photo",
		"  trim  me ":        "trim me",
	}
	for in, want := range cases {
		if got := Clean(in, Options{}); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Clean("hi 🦄", Options{KeepEmoji: true}); got != "hi 🦄" {
		t.Errorf("KeepEmoji: got %q", got)
	}
}
