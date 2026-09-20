// Package jtree is the universal value-tree model: every redis value —
// scalar, container, or nested JSON inside either — becomes one tree of
// nodes that the value pane renders with expand/collapse.
package jtree

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Kind classifies a node for coloring and quoting.
type Kind int

const (
	KindObject Kind = iota
	KindArray
	KindString
	KindNumber
	KindBool
	KindNull
	KindText // non-JSON plain text / binary fallback
)

// Node is one tree vertex. Branches carry Children; leaves carry Value.
// ItemIdx ties a node back to the top-level page item it came from.
type Node struct {
	Label    string
	Value    string
	Kind     Kind
	Children []*Node
	Expanded bool
	ItemIdx  int
}

// Leaf builds a scalar node.
func Leaf(label, value string, k Kind) *Node {
	return &Node{Label: label, Value: value, Kind: k, ItemIdx: -1}
}

// IsBranch reports whether the node is a container (object or array).
func (n *Node) IsBranch() bool { return n.Kind == KindObject || n.Kind == KindArray }

// Branch builds an object or array node.
func Branch(label string, array bool, expanded bool, children ...*Node) *Node {
	k := KindObject
	if array {
		k = KindArray
	}
	return &Node{Label: label, Kind: k, Expanded: expanded, Children: children, ItemIdx: -1}
}

// FromJSON parses text into a tree, preserving object key order. Returns
// nil when the text is not a single valid JSON value.
func FromJSON(text string) *Node {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	n, err := parseValue(dec)
	if err != nil || n == nil {
		return nil
	}
	if dec.More() { // trailing garbage
		return nil
	}
	return n
}

func parseValue(dec *json.Decoder) (*Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return tokenNode(dec, tok)
}

func tokenNode(dec *json.Decoder, tok json.Token) (*Node, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := &Node{Kind: KindObject, Expanded: true}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := keyTok.(string)
				child, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				child.Label = key
				n.Children = append(n.Children, child)
			}
			if _, err := dec.Token(); err != nil { // closing }
				return nil, err
			}
			return n, nil
		case '[':
			n := &Node{Kind: KindArray, Expanded: true}
			i := 0
			for dec.More() {
				child, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				child.Label = strconv.Itoa(i)
				i++
				n.Children = append(n.Children, child)
			}
			if _, err := dec.Token(); err != nil { // closing ]
				return nil, err
			}
			return n, nil
		}
	case string:
		return &Node{Value: t, Kind: KindString}, nil
	case json.Number:
		return &Node{Value: t.String(), Kind: KindNumber}, nil
	case bool:
		return &Node{Value: strconv.FormatBool(t), Kind: KindBool}, nil
	case nil:
		return &Node{Kind: KindNull}, nil
	}
	return nil, fmt.Errorf("unsupported token %T", tok)
}

// ScalarLeaf classifies raw scalar text (a redis value that is not JSON):
// numbers, booleans and null keep their kind for coloring; anything else is
// plain text.
func ScalarLeaf(label, raw string) *Node {
	switch raw {
	case "true", "false":
		return Leaf(label, raw, KindBool)
	case "null", "":
		if raw == "" {
			return Leaf(label, raw, KindText)
		}
		return Leaf(label, raw, KindNull)
	}
	var num json.Number
	if err := json.Unmarshal([]byte(raw), &num); err == nil {
		return Leaf(label, raw, KindNumber)
	}
	return Leaf(label, raw, KindText)
}

// Item wraps a node with the page-item index it belongs to.
func (n *Node) WithItem(idx int) *Node {
	stamp(n, idx)
	return n
}

func stamp(n *Node, idx int) {
	n.ItemIdx = idx
	for _, c := range n.Children {
		stamp(c, idx)
	}
}

// Row is one visible line of the rendered tree.
type Row struct {
	Node    *Node
	Depth   int
	Branch  bool
	Marker  string // ▸ / ▾ / " "
	Rails   string // indent guides ("│ " per ancestor with later siblings)
	Summary string // "{3}" / "[2]" for collapsed branches
	Open    string // "{" / "[" shown on expanded branches
}

// Rows renders visible rows depth-first, honoring collapse state. Children
// carry indent rails: "│ " under each ancestor that still has siblings
// below, plain spaces under a last child.
func Rows(root *Node) []Row {
	var out []Row
	walk(root, -1, "", &out)
	return out
}

func walk(n *Node, depth int, rails string, out *[]Row) {
	for i, c := range n.Children {
		depth := depth + 1
		isLast := i == len(n.Children)-1
		if c.IsBranch() {
			marker, summary := "▸", ""
			open := openBrace(c)
			if len(c.Children) == 0 || c.Expanded {
				marker = "▾"
			} else {
				summary = summaryFor(c)
				open = ""
			}
			*out = append(*out, Row{Node: c, Depth: depth, Branch: true, Marker: marker,
				Rails: rails, Summary: summary, Open: open})
			if c.Expanded {
				childRails := rails + railFor(isLast)
				walk(c, depth, childRails, out)
			}
			continue
		}
		*out = append(*out, Row{Node: c, Depth: depth, Marker: " ", Rails: rails})
	}
}

func railFor(isLast bool) string {
	if isLast {
		return "  "
	}
	return "│ "
}

func openBrace(n *Node) string {
	if n.Kind == KindObject {
		return "{"
	}
	return "["
}

func summaryFor(n *Node) string {
	var b bytes.Buffer
	if n.Kind == KindObject {
		b.WriteByte('{')
	} else {
		b.WriteByte('[')
	}
	b.WriteString(strconv.Itoa(len(n.Children)))
	if n.Kind == KindObject {
		b.WriteByte('}')
	} else {
		b.WriteByte(']')
	}
	return b.String()
}

// ToggleVisible flips the branch at visible row index i. Returns true if a
// branch was toggled.
func ToggleVisible(root *Node, i int) bool {
	rows := Rows(root)
	if i < 0 || i >= len(rows) || !rows[i].Branch {
		return false
	}
	rows[i].Node.Expanded = !rows[i].Node.Expanded
	return true
}
