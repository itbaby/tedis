package jtree

import "testing"

// Branch builds an object or array node (test fixture helper).
func Branch(label string, array bool, expanded bool, children ...*Node) *Node {
	k := KindObject
	if array {
		k = KindArray
	}
	return &Node{Label: label, Kind: k, Expanded: expanded, Children: children, ItemIdx: -1}
}

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

// toggleAt flips the branch at visible row index i (how the app's value
// pane drives collapsing).
func toggleAt(root *Node, i int) bool {
	rows := Rows(root)
	if i < 0 || i >= len(rows) || !rows[i].Branch {
		return false
	}
	return rows[i].Node.Toggle()
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
	// flat branches cycle collapsed → inline → tree → collapsed
	if !toggleAt(root, 1) {
		t.Fatal("toggle failed")
	}
	rows = Rows(root)
	if len(rows) != 2 || rows[1].Summary != `{"a": 1, "b": "x"}` {
		t.Fatalf("inline rows: %+v", rows)
	}
	if !toggleAt(root, 1) {
		t.Fatal("toggle failed")
	}
	rows = Rows(root)
	if len(rows) != 4 || rows[1].Marker != "▾" || rows[2].Node.Label != "a" {
		t.Fatalf("expanded rows: %+v", rows)
	}
	if toggleAt(root, 0) { // leaf: no-op
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

func TestInlineFlatContainers(t *testing.T) {
	root := FromJSON(`{"roles":["admin","dev"],"nested":[[1,2]],"big":["x","aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}`)
	if root == nil {
		t.Fatal("parse failed")
	}
	roles, nested, big := root.Children[0], root.Children[1], root.Children[2]
	if !roles.Inline || nested.Inline || big.Inline {
		t.Fatalf("inline flags: roles=%v nested=%v big=%v", roles.Inline, nested.Inline, big.Inline)
	}
	rows := Rows(root)
	// roles inline on one row; nested expands to one inline child row; big as tree
	if len(rows) != 6 {
		t.Fatalf("rows: %+v", rows)
	}
	if rows[0].Summary != `["admin", "dev"]` || rows[0].Marker != "▸" {
		t.Fatalf("roles row: %+v", rows[0])
	}
	// toggle cycle: inline → expanded tree → collapsed → inline
	roles.Toggle()
	rows = Rows(root)
	if len(rows) != 8 || rows[1].Node.Label != "0" || rows[1].Node.Value != "admin" {
		t.Fatalf("tree rows: %+v", rows)
	}
	roles.Toggle()
	rows = Rows(root)
	if rows[0].Summary != "[2]" || roles.Expanded || roles.Inline {
		t.Fatalf("collapsed: %+v", rows[0])
	}
	roles.Toggle()
	if !roles.Inline {
		t.Fatal("must cycle back to inline")
	}
}

func TestInlineObjectPreview(t *testing.T) {
	root := FromJSON(`{"m":{"x":true,"b":null}}`)
	if root == nil || !root.Children[0].Inline {
		t.Fatalf("flat object should inline: %+v", root)
	}
	rows := Rows(root)
	if len(rows) != 1 || rows[0].Summary != `{"x": true, "b": null}` {
		t.Fatalf("rows: %+v", rows)
	}
}

func TestMarshalReplacePath(t *testing.T) {
	doc := `{"z":1,"a":{"n":[true,null],"s":"x\"y"},"m":{"b":2}}`
	root := FromJSON(doc)
	if root == nil {
		t.Fatal("parse failed")
	}
	if got := Marshal(root); got != doc {
		t.Fatalf("marshal roundtrip:\n got %s\nwant %s", got, doc)
	}
	target := root.Children[1].Children[0].Children[0] // the true in [true,null]
	p := Path(root, target)
	if len(p) != 3 || p[0] != "a" || p[1] != "n" || p[2] != "0" {
		t.Fatalf("path: %v", p)
	}
	if !root.Replace(target, Leaf("", "false", KindBool)) {
		t.Fatal("replace failed")
	}
	want := `{"z":1,"a":{"n":[false,null],"s":"x\"y"},"m":{"b":2}}`
	if got := Marshal(root); got != want {
		t.Fatalf("after replace:\n got %s\nwant %s", got, want)
	}
	if Path(root, Leaf("x", "", KindNull)) != nil {
		t.Fatal("unknown node must yield nil path")
	}
}
