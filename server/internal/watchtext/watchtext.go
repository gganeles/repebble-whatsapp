// Package watchtext turns WhatsApp strings into text the Pebble can store and draw:
// byte-limited on UTF-8 boundaries, whitespace collapsed, and emoji the system
// fonts can't render replaced or dropped.
package watchtext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits in UTF-8 bytes, excluding the trailing NUL the watch adds.
// Keep in sync with docs/protocol.md §3 and watchapp/src/c/model.h.
const (
	ChatName    = 31
	Preview     = 63
	SenderName  = 23
	MessageText = 319
	ReplyText   = 39
	// FullText caps the "read more" view; the watch fetches it in chunks.
	FullText = 4000
)

const ellipsis = "…"

// emojiText maps the most common emoji to something the Pebble fonts can draw.
var emojiText = map[string]string{
	"👍": "(y)", "👎": "(n)", "❤️": "<3", "❤": "<3", "😂": ":'D", "🤣": ":'D",
	"😊": ":)", "🙂": ":)", "😀": ":D", "😃": ":D", "😄": ":D", "😁": ":D",
	"😉": ";)", "😢": ":'(", "😭": ":'(", "😮": ":O", "😱": ":O", "😡": ">:(",
	"😘": ":*", "😍": "<3", "🙏": "(pray)", "👌": "(ok)", "🎉": "(party)",
	"🔥": "(fire)", "😅": "^^'", "🤔": "(hmm)", "👋": "(wave)", "💪": "(strong)",
	"😎": "B)", "🙄": "(eyeroll)", "😴": "(zzz)", "✅": "(v)", "❌": "(x)",
}

// Options controls emoji handling.
type Options struct {
	// KeepEmoji passes emoji through untouched (for firmware with emoji fonts).
	KeepEmoji bool
}

// Clean collapses whitespace and handles emoji, without truncating.
func Clean(s string, opt Options) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for i := 0; i < len(s); {
		if !opt.KeepEmoji {
			if rep, n := matchEmoji(s[i:]); n > 0 {
				if space && b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(rep)
				i += n
				space = false
				continue
			}
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		switch {
		case r == utf8.RuneError && n <= 1:
			continue
		case unicode.IsSpace(r):
			space = true
			continue
		case !opt.KeepEmoji && isEmojiLike(r):
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// Fit cleans s and truncates it to at most max bytes, appending "…" if cut.
func Fit(s string, max int, opt Options) string {
	return Truncate(Clean(s, opt), max)
}

// Truncate cuts s to at most max bytes on a rune boundary, appending "…" if cut.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < len(ellipsis) {
		return cutRunes(s, max)
	}
	return strings.TrimRightFunc(cutRunes(s, max-len(ellipsis)), unicode.IsSpace) + ellipsis
}

func cutRunes(s string, max int) string {
	end := 0
	for i := range s {
		if i > max {
			break
		}
		end = i
	}
	if len(s) <= max {
		end = len(s)
	}
	return s[:end]
}

func matchEmoji(s string) (string, int) {
	// Longest keys are two runes ("❤️"), so try the two-rune prefix first.
	_, n1 := utf8.DecodeRuneInString(s)
	if n1 == 0 {
		return "", 0
	}
	if _, n2 := utf8.DecodeRuneInString(s[n1:]); n2 > 0 {
		if rep, ok := emojiText[s[:n1+n2]]; ok {
			return rep, n1 + n2
		}
	}
	if rep, ok := emojiText[s[:n1]]; ok {
		// Swallow a trailing variation selector or skin tone modifier.
		n := n1
		for n < len(s) {
			r, k := utf8.DecodeRuneInString(s[n:])
			if r != 0xFE0F && !(r >= 0x1F3FB && r <= 0x1F3FF) {
				break
			}
			n += k
		}
		return rep, n
	}
	return "", 0
}

func isEmojiLike(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF: // pictographs, emoticons, transport, flags, etc.
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols and dingbats
		return true
	case r == 0x200D || r == 0xFE0F || r == 0x20E3: // ZWJ, variation selector, keycap
		return true
	case r >= 0xE0020 && r <= 0xE007F: // tag characters (subdivision flags)
		return true
	}
	return false
}
