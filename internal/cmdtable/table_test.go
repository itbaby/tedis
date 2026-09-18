package cmdtable

import "testing"

func reply() []interface{} {
	// get: readonly; set: write; CONFIG: write + readonly flag mix (write wins)
	return []interface{}{
		[]interface{}{"get", int64(2), []interface{}{"readonly", "fast"}, int64(1), int64(1), int64(1)},
		[]interface{}{"set", int64(-3), []interface{}{"write", "denyoom"}, int64(1), int64(1), int64(1)},
		[]interface{}{"CONFIG", int64(-2), []interface{}{"admin", "noscript"}, int64(0), int64(0), int64(0)},
		[]interface{}{[]byte("del"), int64(-2), []interface{}{"write"}, int64(1), int64(1), int64(1)},
	}
}

func TestParseReplyClassification(t *testing.T) {
	tbl := &Table{classes: map[string]Class{}}
	ParseReply(reply(), tbl)
	if tbl.ClassOf("get") != Readonly {
		t.Errorf("get=%v", tbl.ClassOf("get"))
	}
	if tbl.ClassOf("SET") != Write { // case-insensitive
		t.Errorf("SET=%v", tbl.ClassOf("SET"))
	}
	if tbl.ClassOf("del") != Write { // []byte name
		t.Errorf("del=%v", tbl.ClassOf("del"))
	}
	if tbl.ClassOf("config") != Unknown { // neither flag
		t.Errorf("config=%v", tbl.ClassOf("config"))
	}
	if tbl.ClassOf("nope") != Unknown || tbl.ClassOf("") != Unknown {
		t.Error("unknown names must classify as Unknown")
	}
}

func TestParseReplySortedNames(t *testing.T) {
	tbl := &Table{classes: map[string]Class{}}
	ParseReply(reply(), tbl)
	if len(tbl.Names()) != 4 {
		t.Fatalf("names=%v", tbl.Names())
	}
	for i := 1; i < len(tbl.names); i++ {
		if tbl.names[i-1] > tbl.names[i] {
			t.Fatalf("names not sorted: %v", tbl.names)
		}
	}
}

func TestNilTableSafe(t *testing.T) {
	var tbl *Table
	if tbl.ClassOf("get") != Unknown || tbl.Names() != nil {
		t.Error("nil table must be safe")
	}
}
