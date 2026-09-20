package encode

import (
	"strings"
)

// Rule mirrors config.Rule for matching (kept local so encode does not
// depend on the config package).
type Rule struct {
	Pattern string // key glob (* and ?), "" = all
	Type    string // redis type, "" = all
	Encoder string // codec name or "" = auto
}

// Match reports whether the rule applies to a key/type pair.
func (r Rule) Match(key, typ string) bool {
	if r.Type != "" && r.Type != typ {
		return false
	}
	return r.Pattern == "" || GlobMatch(r.Pattern, key)
}

// Resolve returns the configured encoder name for the first matching rule
// ("" when none matches → auto detection).
func Resolve(rules []Rule, key, typ string) string {
	for _, r := range rules {
		if r.Match(key, typ) {
			return r.Encoder
		}
	}
	return ""
}

// GlobMatch implements the * / ? glob used by content rules (same semantics
// as Medis key patterns).
func GlobMatch(pattern, s string) bool {
	// iterative two-way matcher
	pi, si := 0, 0
	star, mark := -1, -1
	for si < len(s) {
		if pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]) {
			pi++
			si++
		} else if pi < len(pattern) && pattern[pi] == '*' {
			star = pi
			mark = si
			pi++
		} else if star >= 0 {
			pi = star + 1
			mark++
			si = mark
		} else {
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

// ByName resolves a codec: builtins first, then external scripts. "auto"
// and unknown names return nil (caller falls back to detection).
func ByName(name string) Codec {
	switch strings.ToLower(name) {
	case "", "auto":
		return nil
	case "json":
		return JSON
	case "msgpack":
		return Msgpack
	case "gzip":
		return Gzip
	case "php":
		return PHP
	case "text":
		return Text
	case "hex":
		return Hex
	}
	for _, e := range ScanExternal() {
		if strings.EqualFold(e.Name(), name) {
			return e
		}
	}
	return nil
}

// CycleNames lists codec names for the value-pane switcher: auto first,
// then builtins, then externals.
func CycleNames() []string {
	out := []string{"auto", "text", "json", "msgpack", "gzip", "php", "hex"}
	for _, e := range ScanExternal() {
		out = append(out, e.Name())
	}
	return out
}
