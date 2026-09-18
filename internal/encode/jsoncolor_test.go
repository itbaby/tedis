package encode

import "testing"

func classes(segs []JSONSeg) []JSONClass {
	out := make([]JSONClass, len(segs))
	for i, s := range segs {
		out[i] = s.Class
	}
	return out
}

func TestHighlightJSONClasses(t *testing.T) {
	text := `{"id": 1001, "active": true, "name": "ada", "note": null, "tags": ["a", 2]}`
	segs := HighlightJSON(text)
	var got []JSONClass
	var strs []string
	for _, s := range segs {
		if s.Text == " " || s.Text == "\n" {
			continue
		}
		got = append(got, s.Class)
		strs = append(strs, s.Text)
	}
	// spot-check semantics rather than exact run boundaries
	byText := map[string]JSONClass{}
	for i, s := range strs {
		byText[s] = got[i]
	}
	if byText[`"id"`] != JSONKey {
		t.Errorf("id: %v", byText[`"id"`])
	}
	if byText[`"name"`] != JSONKey {
		t.Errorf("name: %v", byText[`"name"`])
	}
	if byText[`"ada"`] != JSONString {
		t.Errorf("ada: %v", byText[`"ada"`])
	}
	if byText["1001"] != JSONNumber {
		t.Errorf("1001: %v", byText["1001"])
	}
	if byText["true"] != JSONBool || byText["null"] != JSONNull {
		t.Errorf("bool/null: %v %v", byText["true"], byText["null"])
	}
	if byText["2"] != JSONNumber {
		t.Errorf("2: %v", byText["2"])
	}
}

func TestHighlightJSONEscapedStrings(t *testing.T) {
	text := `{"s": "a\"b", "n": -1.5e3}`
	segs := HighlightJSON(text)
	var joined string
	for _, s := range segs {
		joined += s.Text
	}
	if joined != text {
		t.Fatalf("roundtrip mismatch:\n%s\n%s", joined, text)
	}
}

func TestHighlightJSONMalformedTail(t *testing.T) {
	segs := HighlightJSON(`{"a": 1 ` + "\x00weird")
	var joined string
	for _, s := range segs {
		joined += s.Text
	}
	if joined != `{"a": 1 `+"\x00weird" {
		t.Fatalf("lossy: %q", joined)
	}
}
