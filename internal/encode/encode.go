// Package encode detects and converts stored value formats: JSON, gzip,
// MessagePack, PHP serialize, hex and plain text — plus user-supplied
// external encoders using Medis' encoder_* protocol (base64 over stdio).
package encode

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// Codec converts between raw stored bytes and human-readable text.
type Codec interface {
	Name() string
	Decode(raw []byte) (text string, err error)
	Encode(text string) (raw []byte, err error)
}

// Format renders raw bytes with a codec (or auto-detection when codec==nil).
func Format(raw []byte, codec Codec) (text string, used Codec, err error) {
	if codec == nil {
		codec = Detect(raw)
	}
	t, err := codec.Decode(raw)
	return t, codec, err
}

// ---- auto detection -------------------------------------------------------

// Detect guesses the codec for raw bytes. Order: JSON, gzip (recursive),
// MessagePack, PHP serialize, UTF-8 text, hex fallback.
func Detect(raw []byte) Codec {
	switch {
	case isJSON(raw):
		return JSON
	case isGzip(raw):
		return Gzip
	case isMsgpack(raw):
		return Msgpack
	case isPHP(raw):
		return PHP
	case utf8.Valid(raw):
		return Text
	}
	return Hex
}

func isJSON(raw []byte) bool {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return false
	}
	if t[0] != '{' && t[0] != '[' {
		return false
	}
	return json.Valid(t)
}

func isGzip(raw []byte) bool {
	return len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b
}

func isMsgpack(raw []byte) bool {
	if len(raw) < 2 {
		return false
	}
	// only accept container-shaped payloads; msgpack otherwise accepts
	// nearly any byte sequence (fixints, fixstrs) and would shadow text/php
	switch b := raw[0]; {
	case b >= 0x80 && b <= 0x9f: // fixmap / fixarray
	case b == 0xdc || b == 0xdd || b == 0xde || b == 0xdf: // array16/32, map16/32
	default:
		return false
	}
	var v interface{}
	if msgpackUnmarshal(raw, &v) != nil {
		return false
	}
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		return true
	}
	return false
}

func isPHP(raw []byte) bool {
	t := bytes.TrimSpace(raw)
	if len(t) < 3 {
		return false
	}
	switch t[0] {
	case 'a', 's', 'i', 'd', 'b', 'O', 'N', 'R', 'r':
		return t[1] == ':'
	}
	return false
}

// ---- JSON -----------------------------------------------------------------

type jsonCodec struct{}

// JSON pretty-prints compact or already-valid JSON.
var JSON Codec = jsonCodec{}

func (jsonCodec) Name() string { return "json" }

func (jsonCodec) Decode(raw []byte) (string, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, bytes.TrimSpace(raw), "", "  "); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (jsonCodec) Encode(text string) ([]byte, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(text)); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

// ---- gzip ------------------------------------------------------------------

type gzipCodec struct{}

// Gzip transparently gunzips (detection recurses into the payload).
var Gzip Codec = gzipCodec{}

func (gzipCodec) Name() string { return "gzip" }

func (gzipCodec) Decode(raw []byte) (string, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	defer zr.Close()
	inner, err := io.ReadAll(zr)
	if err != nil {
		return "", err
	}
	t, _, err := Format(inner, nil) // recursive detect
	return t, err
}

func (gzipCodec) Encode(text string) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(text)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- msgpack ---------------------------------------------------------------

type msgpackCodec struct{}

// Msgpack renders msgpack payloads as indented JSON.
var Msgpack Codec = msgpackCodec{}

func (msgpackCodec) Name() string { return "msgpack" }

func (msgpackCodec) Decode(raw []byte) (string, error) {
	var v interface{}
	if err := msgpackUnmarshal(raw, &v); err != nil {
		return "", err
	}
	j, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(j), nil
}

func (msgpackCodec) Encode(text string) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return nil, err
	}
	return msgpackMarshal(v)
}

// ---- php -------------------------------------------------------------------

type phpCodec struct{}

// PHP decodes PHP serialize() payloads into readable form.
var PHP Codec = phpCodec{}

func (phpCodec) Name() string { return "php" }

func (phpCodec) Decode(raw []byte) (string, error) {
	v, err := phpUnserialize(raw)
	if err != nil {
		return "", err
	}
	j, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(j), nil
}

func (phpCodec) Encode(text string) ([]byte, error) {
	return nil, fmt.Errorf("php encode not supported")
}

// ---- text / hex ------------------------------------------------------------

type textCodec struct{}

// Text passes valid UTF-8 through unchanged.
var Text Codec = textCodec{}

func (textCodec) Name() string                      { return "text" }
func (textCodec) Decode(raw []byte) (string, error) { return string(raw), nil }
func (textCodec) Encode(t string) ([]byte, error)   { return []byte(t), nil }

type hexCodec struct{}

// Hex renders arbitrary bytes (binary fallback).
var Hex Codec = hexCodec{}

func (hexCodec) Name() string { return "hex" }

func (hexCodec) Decode(raw []byte) (string, error) {
	return hex.Dump(raw), nil
}

func (hexCodec) Encode(t string) ([]byte, error) {
	return nil, fmt.Errorf("hex view is read-only")
}
