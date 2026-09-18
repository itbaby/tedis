package encode

import "strings"

// JSON token class for syntax coloring.
type JSONClass int

const (
	JSONPunct  JSONClass = iota // { } [ ] : ,
	JSONKey                     // object member name
	JSONString                  // string values
	JSONNumber                  // numbers
	JSONBool                    // true / false
	JSONNull                    // null
)

// JSONSeg is one colored run of a JSON document.
type JSONSeg struct {
	Text  string
	Class JSONClass
}

// HighlightJSON splits JSON text into token segments for coloring.
// Malformed input degrades gracefully: the remainder is returned as one
// punct segment. Key detection: a string immediately followed (ignoring
// whitespace) by ':' is a key.
func HighlightJSON(text string) []JSONSeg {
	var out []JSONSeg
	i, n := 0, len(text)
	flush := func(s string, c JSONClass) {
		if s != "" {
			out = append(out, JSONSeg{Text: s, Class: c})
		}
	}
	for i < n {
		c := text[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			j := i
			for j < n && strings.IndexByte(" \t\n\r", text[j]) >= 0 {
				j++
			}
			flush(text[i:j], JSONPunct) // whitespace rides along, uncolored
			i = j
		case c == '"':
			j := i + 1
			for j < n && text[j] != '"' {
				if text[j] == '\\' && j+1 < n {
					j++
				}
				j++
			}
			if j < n {
				j++ // closing quote
			}
			flush(text[i:j], JSONString)
			i = j
			// key? skip whitespace, expect ':'
			k := j
			for k < n && text[k] == ' ' {
				k++
			}
			if k < n && text[k] == ':' {
				out[len(out)-1].Class = JSONKey
			}
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ':' || c == ',':
			flush(string(c), JSONPunct)
			i++
		case c == '-' || (c >= '0' && c <= '9'):
			j := i
			for j < n && strings.IndexByte("-+.eE0123456789", text[j]) >= 0 {
				j++
			}
			flush(text[i:j], JSONNumber)
			i = j
		case strings.HasPrefix(text[i:], "true") || strings.HasPrefix(text[i:], "false"):
			flush(word(text, &i, n), JSONBool)
		case strings.HasPrefix(text[i:], "null"):
			flush(word(text, &i, n), JSONNull)
		default:
			// unknown char: bail out with the rest uncolored
			flush(text[i:], JSONPunct)
			i = n
		}
	}
	return out
}

func word(text string, i *int, n int) string {
	j := *i
	for j < n && text[j] >= 'a' && text[j] <= 'z' {
		j++
	}
	s := text[*i:j]
	*i = j
	return s
}
