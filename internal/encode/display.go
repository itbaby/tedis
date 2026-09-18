package encode

import (
	"strings"
	"unicode/utf8"
)

// DisplayValue renders a container field value for a single-line table
// cell: auto-detected decode (json/msgpack/gzip/php), newlines escaped,
// result truncated to max runes. Decode failures fall back to the raw text.
// Editing paths must keep using the raw bytes — this is view-only.
func DisplayValue(raw string, max int) string {
	if max <= 0 {
		max = 120
	}
	text := raw
	if t, _, err := Format([]byte(raw), nil); err == nil {
		text = t
	}
	text = strings.ReplaceAll(text, "\n", "\\n")
	text = strings.ReplaceAll(text, "\r", "")
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	cut := []rune(text)[:max]
	return string(cut) + "…"
}
