package encode

import (
	"strings"
	"testing"
)

func TestDisplayValueJSON(t *testing.T) {
	// compact json gets pretty-decoded, then collapsed to one line
	got := DisplayValue(`{"a":1,"b":[1,2]}`, 120)
	if strings.Contains(got, "\n") || !strings.Contains(got, `"a": 1`) {
		t.Fatalf("got %q", got)
	}
}

func TestDisplayValueTruncates(t *testing.T) {
	got := DisplayValue(strings.Repeat("x", 300), 120)
	if len([]rune(got)) != 121 || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %d runes", len([]rune(got)))
	}
}

func TestDisplayValueNewlinesEscaped(t *testing.T) {
	if got := DisplayValue("a\nb", 120); got != "a\\nb" {
		t.Fatalf("got %q", got)
	}
}

func TestDisplayValueBinaryHex(t *testing.T) {
	got := DisplayValue("\x00\xff\xfe", 120)
	if !strings.Contains(got, "000000") && !strings.Contains(got, "ff") {
		t.Fatalf("hex render: %q", got)
	}
}

func TestDisplayValueMsgpackField(t *testing.T) {
	raw, _ := msgpackMarshal(map[string]interface{}{"n": 5})
	got := DisplayValue(string(raw), 120)
	if !strings.Contains(got, `"n": 5`) {
		t.Fatalf("got %q", got)
	}
}
