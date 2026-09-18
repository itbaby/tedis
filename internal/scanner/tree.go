package scanner

import (
	"sort"
	"strings"
)

// Row is one rendered line of the namespace tree.
type Row struct {
	Prefix   string // full key prefix this row represents ("" = all keys)
	Label    string // display label (without indent)
	Count    int    // keys in this subtree
	Depth    int    // 0 = top level
	Folder   bool   // has children → rendered with ▸/▾
	Expanded bool
}

// node is one trie vertex. A node can be a folder, a leaf key, or both
// (e.g. key "users" and keys "users:*"); folder rendering wins for both.
type node struct {
	name     string
	leaf     bool
	expanded bool
	count    int
	children map[string]*node
}

// Tree incrementally indexes scanned key names by separator, capping folder
// depth at maxFold levels (Medis "fold level"): deeper parts are joined back
// into the leaf label. Tree is not safe for concurrent use; feed it from one
// goroutine (the scan session owner).
type Tree struct {
	sep     string
	maxFold int
	root    node
	index   map[string]*node // prefix → node (for Toggle)
	seen    map[string]bool  // SCAN can return duplicates; count once
	total   int
}

// NewTree creates a tree; sep defaults to ":" and maxFold to 1.
func NewTree(sep string, maxFold int) *Tree {
	if sep == "" {
		sep = ":"
	}
	if maxFold <= 0 {
		maxFold = 1
	}
	return &Tree{sep: sep, maxFold: maxFold, index: map[string]*node{}, seen: map[string]bool{}}
}

// Add indexes one key. Returns false if the key was already indexed.
func (t *Tree) Add(key string) bool {
	if t.seen[key] {
		return false
	}
	t.seen[key] = true
	t.total++

	parts := strings.Split(key, t.sep)
	cur := &t.root
	prefix := ""
	for i, part := range parts {
		if i >= t.maxFold { // fold: join the remainder into one leaf label
			part = strings.Join(parts[i:], t.sep)
		}
		if prefix == "" {
			prefix = part
		} else {
			prefix += t.sep + part
		}
		cur = cur.child(part, prefix, t.index)
		cur.count++
		if i >= t.maxFold || i == len(parts)-1 {
			break
		}
	}
	cur.leaf = true
	return true
}

func (n *node) child(name, prefix string, index map[string]*node) *node {
	if n.children == nil {
		n.children = map[string]*node{}
	}
	c, ok := n.children[name]
	if !ok {
		c = &node{name: name}
		n.children[name] = c
		index[prefix] = c
	}
	return c
}

// Total returns the number of distinct keys indexed.
func (t *Tree) Total() int { return t.total }

// Toggle expands/collapses the folder at prefix. Returns whether it changed.
func (t *Tree) Toggle(prefix string) bool {
	n, ok := t.index[prefix]
	if !ok || len(n.children) == 0 {
		return false
	}
	n.expanded = !n.expanded
	return true
}

// Rows renders the visible tree (collapsed folders show a single row).
// The first row is always the synthetic "all keys" root.
func (t *Tree) Rows() []Row {
	rows := []Row{{Prefix: "", Label: "*", Count: t.total}}
	t.walk(&t.root, "", 0, &rows)
	return rows
}

func (t *Tree) walk(n *node, prefix string, depth int, rows *[]Row) {
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := n.children[name]
		var p string
		if prefix == "" {
			p = name
		} else {
			p = prefix + t.sep + name
		}
		folder := len(c.children) > 0
		*rows = append(*rows, Row{
			Prefix:   p,
			Label:    name,
			Count:    c.count,
			Depth:    depth,
			Folder:   folder,
			Expanded: c.expanded,
		})
		if folder && c.expanded {
			t.walk(c, p, depth+1, rows)
		}
	}
}
