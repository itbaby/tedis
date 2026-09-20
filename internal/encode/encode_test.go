package encode

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectJSON(t *testing.T) {
	raw := []byte(`{"b":1,"a":[1,2]}`)
	text, used, err := Format(raw, nil)
	if err != nil || used.Name() != "json" {
		t.Fatalf("used=%v err=%v", used, err)
	}
	if text != "{\n  \"b\": 1,\n  \"a\": [\n    1,\n    2\n  ]\n}" {
		t.Fatalf("text=%q", text)
	}
	back, err := used.Encode(text)
	if err != nil || !bytes.Equal(back, raw) {
		t.Fatalf("roundtrip %q err=%v", back, err)
	}
}

func TestDetectGzipJSON(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(`{"x":1}`))
	zw.Close()
	text, used, err := Format(buf.Bytes(), nil)
	if err != nil || used.Name() != "gzip" {
		t.Fatalf("used=%v err=%v", used, err)
	}
	if text != "{\n  \"x\": 1\n}" {
		t.Fatalf("text=%q", text)
	}
}

func TestDetectMsgpack(t *testing.T) {
	raw, _ := msgpackMarshal(map[string]interface{}{"name": "ada", "n": 42})
	if !isMsgpack(raw) {
		t.Fatal("msgpack not detected")
	}
	// msgpack map bytes are also valid… ensure detect picks msgpack not text
	text, used, err := Format(raw, nil)
	if err != nil || used.Name() != "msgpack" {
		t.Fatalf("used=%v err=%v", used, err)
	}
	var v map[string]interface{}
	if json.Unmarshal([]byte(text), &v) != nil || v["name"] != "ada" || v["n"] != float64(42) {
		t.Fatalf("decoded=%s", text)
	}
}

func TestDetectPHP(t *testing.T) {
	raw := []byte(`a:2:{s:1:"a";i:1;s:1:"b";s:3:"two";}`)
	if !isPHP(raw) {
		t.Fatal("php not detected")
	}
	text, used, err := Format(raw, nil)
	if err != nil || used.Name() != "php" {
		t.Fatalf("used=%v err=%v text=%q", used, err, text)
	}
}

func TestDetectTextAndHex(t *testing.T) {
	_, used, _ := Format([]byte("plain utf8 ✓"), nil)
	if used.Name() != "text" {
		t.Fatalf("text detect=%v", used)
	}
	_, used, _ = Format([]byte{0x00, 0xff, 0xfe, 0x01}, nil)
	if used.Name() != "hex" {
		t.Fatalf("hex detect=%v", used)
	}
}

func TestExternalReverseEncoder(t *testing.T) {
	// exact script from the Medis docs (decode only)
	dir := t.TempDir()
	script := filepath.Join(dir, "encoder_Reverse.sh")
	os.WriteFile(script, []byte(`#!/bin/bash
set -e
set -o pipefail
if [ $1 = "decode" ]; then
  input=$(cat)
  decoded=$( base64 -d <<< $input )
  reversed=$( echo -n $decoded | rev)
  echo -n $( echo -n $reversed | base64 )
elif [ $1 = "encode" ]; then
  echo "Encode not implemented!" 1>&2
  exit 1
fi
`), 0o755)

	// point UserConfigDir at the temp dir (macOS: ~/Library/Application Support)
	oldHome := os.Getenv("HOME")
	home := filepath.Join(dir, "cfg")
	xdir := filepath.Join(home, "Library", "Application Support", "tedis", "encoders")
	os.MkdirAll(xdir, 0o755)
	os.Rename(script, filepath.Join(xdir, "encoder_Reverse.sh"))
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", home)
	ClearExternalCache() // scan against the temp HOME, not the cached listing

	list := ScanExternal()
	if len(list) != 1 || list[0].Name() != "Reverse" {
		t.Fatalf("scan=%v", list)
	}
	text, err := list[0].Decode([]byte("-17"))
	if err != nil || text != "71-" {
		t.Fatalf("decode=%q err=%v", text, err) // doc example: -17 reverses to 71-
	}
	if _, err := list[0].Encode("x"); err == nil {
		t.Fatal("encode must surface the script error")
	}
}
