// Package jtree is the universal value-tree model: every redis value —
// scalar, container, or nested JSON inside either — becomes one tree of
// nodes that the value pane renders with expand/collapse.
package jtree

import (
	"encoding/json"
	"fmt"
	"slices"
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
	Inline   bool // flat container rendered as a one-line JSON preview
	ItemIdx  int
}

// Leaf builds a scalar node.
func Leaf(label, value string, k Kind) *Node {
	return &Node{Label: label, Value: value, Kind: k, ItemIdx: -1}
}

// IsBranch reports whether the node is a container (object or array).
func (n *Node) IsBranch() bool { return n.Kind == KindObject || n.Kind == KindArray }

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
	markInline(n)
	return n
}

// markInline flags every flat container (all children scalar, preview fits
// the width cap) so Rows renders it on a single line by default.
func markInline(n *Node) {
	if _, ok := inlinePreview(n); ok {
		n.Inline = true
	}
	for _, c := range n.Children {
		markInline(c)
	}
}

// maxInline caps the one-line preview of a flat container.
const maxInline = 120

// inlinePreview renders a flat container as ["a", "b"] / {"k": 1}.
func inlinePreview(n *Node) (string, bool) {
	if !n.IsBranch() || len(n.Children) == 0 {
		return "", false
	}
	open, shut := "{", "}"
	if n.Kind == KindArray {
		open, shut = "[", "]"
	}
	var b strings.Builder
	b.WriteString(open)
	for i, c := range n.Children {
		if c.IsBranch() {
			return "", false
		}
		if i > 0 {
			b.WriteString(", ")
		}
		if n.Kind == KindObject {
			b.WriteString(`"` + c.Label + `": `)
		}
		b.WriteString(ScalarText(c))
		if b.Len() > maxInline {
			return "", false
		}
	}
	b.WriteString(shut)
	return b.String(), true
}

// Toggle cycles a branch's display. Flat containers go
// inline → expanded tree → collapsed → inline; others flip expanded/collapsed.
func (n *Node) Toggle() bool {
	if !n.IsBranch() {
		return false
	}
	if _, ok := inlinePreview(n); ok {
		switch {
		case n.Inline:
			n.Inline = false
			n.Expanded = true
		case n.Expanded:
			n.Expanded = false
		default:
			n.Inline = true
		}
		return true
	}
	n.Expanded = !n.Expanded
	return true
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

// ScalarText renders one leaf's display text: strings quoted, numbers/bools
// bare, null and newlines in plain text escaped.
func ScalarText(n *Node) string {
	switch n.Kind {
	case KindString:
		return `"` + n.Value + `"`
	case KindNull:
		return "null"
	case KindText:
		return strings.ReplaceAll(n.Value, "\n", "\\n")
	}
	return n.Value
}

// Row is one visible line of the rendered tree.
type Row struct {
	Node    *Node
	Depth   int
	Branch  bool
	Marker  string // ▸ / ▾ / " "
	Rails   string // indent guides ("│ " per ancestor with later siblings)
	Summary string // "{3}" / "[2]" for collapsed branches; JSON preview for inline ones
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
			if prev, ok := inlinePreview(c); ok && c.Inline {
				*out = append(*out, Row{Node: c, Depth: depth, Branch: true,
					Marker: "▸", Rails: rails, Summary: prev})
				continue
			}
			marker, summary := "▸", ""
			open := openBrace(c)
			if len(c.Children) == 0 || c.Expanded {
				marker = "▾"
			} else {
				summary = Summary(c)
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

// Summary renders the collapsed-branch hint, e.g. "{3}" or "[2]".
func Summary(n *Node) string {
	if n.Kind == KindObject {
		return fmt.Sprintf("{%d}", len(n.Children))
	}
	return fmt.Sprintf("[%d]", len(n.Children))
}

// Marshal renders the subtree as compact JSON. Object key order and raw
// number text are preserved; strings are re-escaped.
func Marshal(n *Node) string {
	switch n.Kind {
	case KindObject:
		var b strings.Builder
		b.WriteByte('{')
		for i, c := range n.Children {
			if i > 0 {
				b.WriteByte(',')
			}
			k, _ := json.Marshal(c.Label)
			b.Write(k)
			b.WriteByte(':')
			b.WriteString(Marshal(c))
		}
		b.WriteByte('}')
		return b.String()
	case KindArray:
		var b strings.Builder
		b.WriteByte('[')
		for i, c := range n.Children {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(Marshal(c))
		}
		b.WriteByte(']')
		return b.String()
	case KindString, KindText:
		k, _ := json.Marshal(n.Value)
		return string(k)
	case KindNull:
		return "null"
	}
	if n.Value == "" {
		return "null"
	}
	return n.Value
}

// Replace swaps the descendant pointer old with repl, keeping the slot's
// label. Reports whether old was found.
func (n *Node) Replace(old, repl *Node) bool {
	for i, c := range n.Children {
		if c == old {
			repl.Label = c.Label
			n.Children[i] = repl
			return true
		}
		if c.IsBranch() && c.Replace(old, repl) {
			return true
		}
	}
	return false
}

// FindParent returns the branch that holds target as a child (nil for root
// or when target is not in the tree).
func FindParent(root, target *Node) *Node {
	for _, c := range root.Children {
		if c == target {
			return root
		}
		if c.IsBranch() {
			if p := FindParent(c, target); p != nil {
				return p
			}
		}
	}
	return nil
}

// Path returns the labels from root down to target (nil when not found).
// Array elements carry their index string.
func Path(root, target *Node) []string {
	var cur, found []string
	var walk func(n *Node) bool
	walk = func(n *Node) bool {
		for _, c := range n.Children {
			cur = append(cur, c.Label)
			if c == target {
				found = slices.Clone(cur)
				return true
			}
			if c.IsBranch() && walk(c) {
				return true
			}
			cur = cur[:len(cur)-1]
		}
		return false
	}
	walk(root)
	return found
}
