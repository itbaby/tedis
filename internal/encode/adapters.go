package encode

import (
	"fmt"
	"strconv"

	"github.com/elliotchance/phpserialize"
	"github.com/vmihailenco/msgpack/v5"
)

func msgpackUnmarshal(raw []byte, v *interface{}) error {
	return msgpack.Unmarshal(raw, v)
}

func msgpackMarshal(v interface{}) ([]byte, error) {
	return msgpack.Marshal(v)
}

// phpUnserialize decodes by top-level type: phpserialize needs a concrete
// out pointer, so try map → slice → scalar.
func phpUnserialize(raw []byte) (interface{}, error) {
	var m map[interface{}]interface{}
	if err := phpserialize.Unmarshal(raw, &m); err == nil {
		return phpKeyed(m), nil
	}
	var s []interface{}
	if err := phpserialize.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var str string
	if err := phpserialize.Unmarshal(raw, &str); err == nil {
		return str, nil
	}
	var i int64
	if err := phpserialize.Unmarshal(raw, &i); err == nil {
		return i, nil
	}
	var f float64
	if err := phpserialize.Unmarshal(raw, &f); err == nil {
		return f, nil
	}
	return nil, errPHPUnsupported
}

// phpKeyed normalizes map[interface{}]interface{} into map[string]interface{}
// so it JSON-renders cleanly.
func phpKeyed(m map[interface{}]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		key := fmtKey(k)
		if child, ok := v.(map[interface{}]interface{}); ok {
			out[key] = phpKeyed(child)
			continue
		}
		out[key] = v
	}
	return out
}

func fmtKey(k interface{}) string {
	switch x := k.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return fmt.Sprint(k)
}

type phpErr string

func (e phpErr) Error() string { return string(e) }

const errPHPUnsupported = phpErr("unsupported php serialize payload")
