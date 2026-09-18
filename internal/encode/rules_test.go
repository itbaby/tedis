package encode

import "testing"

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"*", "anything", true},
		{"session:*", "session:9f2", true},
		{"session:*", "session", false},
		{"cache:*:v2", "cache:home:v2", true},
		{"cache:*:v2", "cache:home:v3", false},
		{"user?", "user1", true},
		{"user?", "user12", false},
		{"a*b*c", "aXbYc", true},
		{"a*b*c", "aXc", false},
		{"", "x", false},
	}
	for _, c := range cases {
		if got := GlobMatch(c.pat, c.s); got != c.want {
			t.Errorf("%q vs %q = %v", c.pat, c.s, got)
		}
	}
}

func TestResolveRules(t *testing.T) {
	rules := []Rule{
		{Pattern: "session:*", Encoder: "msgpack"},
		{Pattern: "cache:*", Type: "string", Encoder: "gzip"},
		{Pattern: "", Type: "hash", Encoder: "php"}, // all hashes
	}
	if got := Resolve(rules, "session:a", "string"); got != "msgpack" {
		t.Errorf("1: %q", got)
	}
	if got := Resolve(rules, "cache:x", "string"); got != "gzip" {
		t.Errorf("2: %q", got)
	}
	if got := Resolve(rules, "cache:x", "hash"); got != "php" { // type mismatch → rule 3
		t.Errorf("3: %q", got)
	}
	if got := Resolve(rules, "other", "string"); got != "" {
		t.Errorf("4: %q", got)
	}
}

func TestByName(t *testing.T) {
	if ByName("auto") != nil || ByName("") != nil || ByName("nope") != nil {
		t.Fatal("auto/empty/unknown must be nil")
	}
	if ByName("JSON").Name() != "json" { // case-insensitive
		t.Fatal("json lookup failed")
	}
}
