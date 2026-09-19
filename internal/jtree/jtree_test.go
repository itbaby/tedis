package jtree

import "testing"

func TestFromJSONPreservesKeyOrder(t *testing.T) {
	n := FromJSON(`{"z":1,"a":2,"m":{"x":true,"b":null}}`)
	if n == nil || n.Kind != KindObject || len(n.Children) != 3 {
		t.Fatalf("root: %+v", n)
	}
	if n.Children[0].Label != "z" || n.Children[1].Label != "a" || n.Children[2].Label != "m" {
		t.Fatalf("order: %v %v %v", n.Children[0].Label, n.Children[1].Label, n.Children[2].Label)
	}
	m := n.Children[2]
	if m.Kind != KindObject || m.Children[0].Label != "x" || m.Children[0].Kind != KindBool {
		t.Fatalf("nested: %+v", m)
	}
	if m.Children[1].Kind != KindNull {
		t.Fatalf("null: %+v", m.Children[1])
	}
}

func TestFromJSONArrayAndScalars(t *testing.T) {
	n := FromJSON(`[1, 2.5, "s", [10]]`)
	if n == nil || n.Kind != KindArray || len(n.Children) != 4 {
		t.Fatalf("root: %+v", n)
	}
	if n.Children[0].Kind != KindNumber || n.Children[2].Kind != KindString {
		t.Fatalf("kinds: %+v", n.Children)
	}
	if n.Children[3].Kind != KindArray || n.Children[3].Children[0].Value != "10" {
		t.Fatalf("nested array: %+v", n.Children[3])
	}
	// labels are indexes
	if n.Children[2].Label != "2" {
		t.Fatalf("label: %q", n.Children[2].Label)
	}
}

func TestFromJSONRejectsNonJSON(t *testing.T) {
	for _, s := range []string{"", "plain text", `{"a":1} trailing`, `{"a":`} {
		if FromJSON(s) != nil {
			t.Errorf("%q should be nil", s)
		}
	}
}

func TestRowsAndToggle(t *testing.T) {
	root := Branch("", false, true,
		Leaf("plain", "hello", KindText),
		Branch("obj", false, false,
			Leaf("a", "1", KindNumber),
			Leaf("b", "x", KindString)))
	rows := Rows(root)
	// collapsed: root children only, obj shows summary {2}
	if len(rows) != 2 || rows[1].Marker != "▸" || rows[1].Summary != "{2}" {
		t.Fatalf("rows: %+v", rows)
	}
	if !ToggleVisible(root, 1) {
		t.Fatal("toggle failed")
	}
	rows = Rows(root)
	if len(rows) != 4 || rows[1].Marker != "▾" || rows[2].Node.Label != "a" {
		t.Fatalf("expanded rows: %+v", rows)
	}
	if ToggleVisible(root, 0) { // leaf: no-op
		t.Fatal("leaf toggle must fail")
	}
}

func TestRailsAndBraces(t *testing.T) {
	// object with two branches: children of the first get "│ " rails,
	// children of the last get "  "
	root := Branch("", false, true,
		Branch("a", false, true, Leaf("x", "1", KindNumber)),
		Branch("b", false, true, Leaf("y", "2", KindNumber)),
	)
	rows := Rows(root)
	// rows: [0]=▸? a expanded → "▾ a {", [1]=x rails "│ ", [2]="▾ b {", [3]=y rails "  "
	if rows[0].Open != "{" || rows[2].Open != "{" {
		t.Fatalf("open braces: %+v %+v", rows[0], rows[2])
	}
	if rows[1].Rails != "│ " || rows[3].Rails != "  " {
		t.Fatalf("rails: %q %q", rows[1].Rails, rows[3].Rails)
	}
	// collapsed branch keeps summary, no open brace
	root.Children[0].Expanded = false
	rows = Rows(root)
	if rows[0].Summary != "{1}" || rows[0].Open != "" {
		t.Fatalf("collapsed: %+v", rows[0])
	}
}

func TestWithItemStamps(t *testing.T) {
	root := Branch("", false, true,
		Branch("obj", false, true, Leaf("a", "1", KindNumber))).WithItem(7)
	if root.Children[0].ItemIdx != 7 || root.Children[0].Children[0].ItemIdx != 7 {
		t.Fatal("item idx not stamped")
	}
}

func TestScalarLeafClassification(t *testing.T) {
	if ScalarLeaf("x", "3.14").Kind != KindNumber {
		t.Fatal("number")
	}
	if ScalarLeaf("x", "true").Kind != KindBool {
		t.Fatal("bool")
	}
	if ScalarLeaf("x", "null").Kind != KindNull {
		t.Fatal("null")
	}
	if ScalarLeaf("x", "hello world").Kind != KindText {
		t.Fatal("text")
	}
}
