package keymap

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Convert rewrites text as if every character had been typed with the same
// physical keys while the "to" layout was active instead of "from".
//
// Characters the source layout cannot type with at most Shift, and characters
// the target layout has no key for, are copied unchanged. Nothing is dropped
// or reordered.
func Convert(text string, from, to *Layout) string {
	if from == nil || to == nil || from == to || text == "" {
		return text
	}

	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		press, ok := from.Press(r)
		if !ok {
			b.WriteRune(r)
			continue
		}
		if converted := to.Rune(press.Key, press.Level); converted != 0 {
			r = converted
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Detect guesses which of the layouts the text was typed in.
//
// Every character votes for the layouts able to type it; characters that every
// layout can type carry no information and are ignored. The preferred index,
// normally the active layout, wins ties and is returned when nothing voted or
// layouts is empty.
func Detect(text string, layouts []*Layout, preferred int) int {
	if len(layouts) == 0 {
		return preferred
	}
	if preferred < 0 || preferred >= len(layouts) {
		preferred = 0
	}

	votes := make([]int, len(layouts))
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		typedBy := 0
		for i, l := range layouts {
			if l.CanType(r) {
				typedBy++
				votes[i]++
			}
		}
		if typedBy == len(layouts) {
			for i := range votes {
				votes[i]--
			}
		}
	}

	best := preferred
	for i, v := range votes {
		if v > votes[best] {
			best = i
		}
	}
	if votes[best] <= 0 {
		return preferred
	}
	return best
}

// SplitLastWord splits text into the part that stays as typed, the last word,
// and the whitespace after it. Only whitespace separates words: punctuation
// may be a letter in another layout (";bpym" is "жизнь"). word is empty when
// text holds no non-space character.
func SplitLastWord(text string) (head, word, tail string) {
	end := len(text)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:end])
		if !unicode.IsSpace(r) {
			break
		}
		end -= size
	}
	start := end
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if unicode.IsSpace(r) {
			break
		}
		start -= size
	}
	return text[:start], text[start:end], text[end:]
}
