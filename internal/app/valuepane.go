package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"tedis/internal/encode"
	"tedis/internal/jtree"
	"tedis/internal/keyview"
)

// valuePage is the current page of the selected key's content.
type valuePage struct {
	key       string
	kind      string // redis type: string/hash/list/set/zset/stream/ReJSON-RL
	raw       string // string payload before decoding
	codecName string // "", "auto", or codec name (v key cycles)

	cursor  uint64 // SCAN-family cursor (hash/set/zset)
	start   int64  // list offset
	total   int64  // list length
	lastID  string // stream high-water id
	hasMore bool

	rows      [][2]string // raw page items (col1, col2) — edits act on these
	usedCodec string      // codec that produced the current rendering
	decoded   string      // decoded text (string/ReJSON payloads)
	tree      *jtree.Node // universal value tree over the page
	treeRows  []jtree.Row
}

// loadValue opens the typed view for a key (called when selection settles).
func (a *App) loadValue(key, kind string) {
	a.valPage = &valuePage{key: key, kind: kind}
	a.fetchValuePage()
}

func (a *App) fetchValuePage() {
	p := a.valPage
	if p == nil {
		return
	}
	c := a.rc.Load()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var rows [][2]string
		var more bool
		switch p.kind {
		case "string", "ReJSON-RL":
			var v string
			var err error
			if p.kind == "ReJSON-RL" {
				v, err = keyview.LoadJSON(ctx, c.Client, p.key)
			} else {
				v, err = keyview.LoadString(ctx, c.Client, p.key)
			}
			if err != nil {
				a.valueErr(err)
				return
			}
			p.raw = v
			rows = a.stringRowsDecoded()
		case "hash":
			next, items, err := keyview.LoadHash(ctx, c.Client, p.key, p.cursor, 0)
			if err != nil {
				a.valueErr(err)
				return
			}
			for _, it := range items {
				rows = append(rows, [2]string{it.Field, it.Value})
			}
			p.cursor, more = next, next != 0
		case "set":
			next, members, err := keyview.LoadSet(ctx, c.Client, p.key, p.cursor, 0)
			if err != nil {
				a.valueErr(err)
				return
			}
			for _, m := range members {
				rows = append(rows, [2]string{m, ""})
			}
			p.cursor, more = next, next != 0
		case "zset":
			next, items, err := keyview.LoadZSet(ctx, c.Client, p.key, p.cursor, 0)
			if err != nil {
				a.valueErr(err)
				return
			}
			for _, it := range items {
				rows = append(rows, [2]string{it.Member, formatScore(it.Score)})
			}
			p.cursor, more = next, next != 0
		case "list":
			items, total, err := keyview.LoadList(ctx, c.Client, p.key, p.start, 0)
			if err != nil {
				a.valueErr(err)
				return
			}
			for _, it := range items {
				rows = append(rows, [2]string{strconv.FormatInt(it.Index, 10), it.Value})
			}
			p.total = total
			more = p.start+int64(len(items)) < total
			if p.start > 0 {
				rows = append([][2]string{{fmt.Sprintf("… (%d earlier)", p.start), ""}}, rows...)
			}
		case "stream":
			items, err := keyview.LoadStream(ctx, c.Client, p.key, p.lastID, 0)
			if err != nil {
				a.valueErr(err)
				return
			}
			for _, it := range items {
				var b strings.Builder
				for i, f := range it.Fields {
					if i > 0 {
						b.WriteByte(' ')
					}
					b.WriteString(f[0] + "=" + f[1])
				}
				rows = append(rows, [2]string{it.ID, b.String()})
				p.lastID = it.ID
			}
			more = len(items) > 0
		default:
			rows = [][2]string{{"(unsupported type: " + p.kind + ")", ""}}
		}
		p.rows = rows
		p.hasMore = more
		a.tapp.QueueUpdateDraw(a.renderValue)
	}()
}

func (a *App) valueErr(err error) {
	a.tapp.QueueUpdateDraw(func() {
		p := a.valPage
		if p == nil {
			return
		}
		a.renderValueTitle(0)
		a.value.Clear()
		a.value.SetCell(1, 0, cell(err.Error(), a.th.Error).SetSelectable(false))
	})
}

// stringRowsDecoded renders the string payload through the active codec
// (content rule first, else the manually chosen one, else auto-detect).
func (a *App) stringRowsDecoded() [][2]string {
	p := a.valPage
	codec := a.valueCodec()
	text, used, err := encode.Format([]byte(p.raw), codec)
	if err != nil {
		p.usedCodec = ""
		p.decoded = ""
		return [][2]string{{"error", err.Error()}}
	}
	p.usedCodec = used.Name()
	p.decoded = text
	return [][2]string{{"value", text}}
}

// valueCodec resolves the codec for the current string view.
func (a *App) valueCodec() encode.Codec {
	p := a.valPage
	if p == nil {
		return nil
	}
	if p.codecName == "" || p.codecName == "auto" {
		if c := a.rc.Load(); c != nil {
			var rules []encode.Rule
			for _, r := range c.P.Rules {
				rules = append(rules, encode.Rule{Pattern: r.Pattern, Type: r.Type, Encoder: r.Encoder})
			}
			if name := encode.Resolve(rules, p.key, p.kind); name != "" {
				return encode.ByName(name)
			}
		}
		return nil // auto
	}
	return encode.ByName(p.codecName)
}

// cycleValueCodec steps through auto → builtins → externals.
func (a *App) cycleValueCodec() {
	p := a.valPage
	if p == nil || (p.kind != "string" && p.kind != "ReJSON-RL") {
		a.flash("codec applies to string values", a.th.Dim)
		return
	}
	names := encode.CycleNames()
	cur := p.codecName
	if cur == "" {
		cur = "auto"
	}
	idx := 0
	for i, n := range names {
		if n == cur {
			idx = i
			break
		}
	}
	next := names[(idx+1)%len(names)]
	p.codecName = next
	a.renderValue()
}

func formatScore(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// ---- universal tree view --------------------------------------------------

// buildValueTree converts the current page into one value tree: containers
// become objects/arrays, scalars classify, and any field that decodes to
// JSON nests further (msgpack/gzip/php included).
func (a *App) buildValueTree() *jtree.Node {
	p := a.valPage
	if p == nil {
		return nil
	}
	switch p.kind {
	case "string", "ReJSON-RL":
		if t := jtree.FromJSON(p.decoded); t != nil {
			return t.WithItem(0)
		}
		if n := jtree.ScalarLeaf("", p.decoded); n != nil {
			return n.WithItem(0)
		}
	case "set": // array of members
		b := &jtree.Node{Kind: jtree.KindArray, Expanded: true}
		for i, r := range p.rows {
			if strings.HasPrefix(r[0], "… (") {
				continue
			}
			b.Children = append(b.Children, nodeForRaw("", r[0]).WithItem(i))
		}
		return b
	case "zset": // object member → score
		b := &jtree.Node{Kind: jtree.KindObject, Expanded: true}
		for i, r := range p.rows {
			score, err := strconv.ParseFloat(r[1], 64)
			if err != nil {
				score = 0
			}
			b.Children = append(b.Children, jtree.Leaf(r[0], formatScore(score), jtree.KindNumber).WithItem(i))
		}
		return b
	default: // hash: object; list/stream: array
		array := p.kind == "list" || p.kind == "stream"
		b := &jtree.Node{Kind: jtree.KindObject, Expanded: true}
		if array {
			b.Kind = jtree.KindArray
		}
		for i, r := range p.rows {
			if strings.HasPrefix(r[0], "… (") { // pagination anchor row
				b.Children = append(b.Children, jtree.Leaf(r[0], "", jtree.KindText).WithItem(i))
				continue
			}
			if p.kind == "stream" {
				kids := fieldsFromJoined(r[1])
				n := &jtree.Node{Label: r[0], Kind: jtree.KindObject, Expanded: false, Children: kids}
				b.Children = append(b.Children, n.WithItem(i))
				continue
			}
			b.Children = append(b.Children, nodeForRaw(r[0], r[1]).WithItem(i))
		}
		return b
	}
	return nil
}

// nodeForRaw classifies one field/item payload: JSON-ish (incl. decoded
// msgpack/gzip/php) nests as a branch, otherwise a classified scalar.
func nodeForRaw(label, raw string) *jtree.Node {
	if text, used, err := encode.Format([]byte(raw), nil); err == nil {
		switch used.Name() {
		case "json", "msgpack", "gzip", "php":
			if t := jtree.FromJSON(text); t != nil {
				t.Label = label
				return t
			}
		}
		return jtree.ScalarLeaf(label, text)
	}
	return jtree.ScalarLeaf(label, raw)
}

// fieldsFromJoined splits the stream "k=v k2=v2" joined form back to leaves.
func fieldsFromJoined(s string) []*jtree.Node {
	if s == "" {
		return nil
	}
	var kids []*jtree.Node
	for _, part := range strings.Fields(s) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			kids = append(kids, jtree.Leaf(part, "", jtree.KindText))
			continue
		}
		kids = append(kids, jtree.ScalarLeaf(k, v))
	}
	return kids
}

func kindLabel(kind string) string {
	if kind == "ReJSON-RL" {
		return "json"
	}
	return kind
}

func (a *App) renderValueTitle(n int) {
	p := a.valPage
	if p == nil {
		a.value.SetTitle(" value ")
		return
	}
	t := fmt.Sprintf(" [%s]%s[%s] · %s", hex(a.th.Title), p.key, hex(a.th.Dim), kindLabel(p.kind))
	if p.kind == "string" || p.kind == "ReJSON-RL" {
		if p.usedCodec != "" {
			t += fmt.Sprintf(" · %s", p.usedCodec)
		}
	}
	t += fmt.Sprintf(" · %d", n)
	if p.hasMore {
		t += "+"
	}
	a.value.SetTitle(t + " ")
}

func (a *App) renderValue() {
	p := a.valPage
	if p == nil {
		return
	}
	a.value.Clear()
	if p.tree == nil {
		p.tree = a.buildValueTree()
	}
	if p.tree == nil {
		a.value.SetCell(1, 0, cell("(empty)", a.th.Dim).SetSelectable(false))
		a.renderValueTitle(0)
		return
	}
	p.treeRows = jtree.Rows(p.tree)
	for i, r := range p.treeRows {
		label := r.Rails + r.Marker + " " + r.Node.Label + " " + r.Open
		c1 := cell(label, a.labelColor(r)).SetExpansion(1)
		a.value.SetCell(i+1, 0, c1)
		if r.Branch {
			v := r.Summary
			a.value.SetCell(i+1, 1, cell(v, a.th.Dim))
			continue
		}
		a.value.SetCell(i+1, 1, cell(a.scalarText(r.Node), a.scalarColor(r.Node)))
	}
	a.renderValueTitle(len(p.treeRows))
	if len(p.treeRows) == 0 {
		a.value.SetCell(1, 0, cell("(empty)", a.th.Dim).SetSelectable(false))
	}
	a.value.Select(1, 0)
}

// scalarText renders a leaf value: strings quoted, numbers/bools bare.
func (a *App) scalarText(n *jtree.Node) string {
	switch n.Kind {
	case jtree.KindString:
		return `"` + n.Value + `"`
	case jtree.KindNull:
		return "null"
	case jtree.KindText:
		v := strings.ReplaceAll(n.Value, "\n", "\\n")
		if r := []rune(v); len(r) > 200 {
			return string(r[:200]) + "…"
		}
		return v
	}
	return n.Value
}

func (a *App) scalarColor(n *jtree.Node) tcell.Color {
	switch n.Kind {
	case jtree.KindString:
		return (a.th.JSONString)
	case jtree.KindNumber:
		return (a.th.JSONNumber)
	case jtree.KindBool:
		return (a.th.JSONBool)
	case jtree.KindNull:
		return (a.th.JSONNull)
	case jtree.KindObject, jtree.KindArray:
		return (a.th.Title)
	}
	return (a.th.Text)
}

func (a *App) labelColor(r jtree.Row) tcell.Color {
	if r.Depth == 1 {
		return a.th.Title
	}
	return a.th.Text
}

// valueToggle flips the branch under the cursor (Enter/l on the value pane).
// valueSelectedRow maps the tree cursor back to its raw page item.
func (a *App) valueSelectedRow() ([2]string, int, bool) {
	p := a.valPage
	if p == nil || p.treeRows == nil {
		return [2]string{}, 0, false
	}
	row, _ := a.value.GetSelection()
	if row <= 0 || row > len(p.treeRows) {
		return [2]string{}, 0, false
	}
	idx := p.treeRows[row-1].Node.ItemIdx
	if idx < 0 || idx >= len(p.rows) {
		return [2]string{}, 0, false
	}
	return p.rows[idx], idx, true
}

func (a *App) valueToggle() {
	p := a.valPage
	if p == nil || p.tree == nil {
		return
	}
	row, _ := a.value.GetSelection()
	if jtree.ToggleVisible(p.tree, row-1) {
		a.renderValue()
	}
}

// prefetchAround bulk-loads type+ttl for rows near idx (visible window).
func (a *App) prefetchAround(idx int) {
	s := a.scan
	c := a.rc.Load()
	if s == nil || c == nil {
		return
	}
	lo := idx - 10
	if lo < 0 {
		lo = 0
	}
	hi := idx + 10
	if hi > len(s.keys)-1 {
		hi = len(s.keys) - 1
	}
	var want []string
	for i := lo; i <= hi; i++ {
		if _, ok := s.meta[s.keys[i]]; !ok {
			want = append(want, s.keys[i])
		}
	}
	if len(want) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		metas, err := c.KeyMetaBatch(ctx, want)
		if err != nil {
			return
		}
		a.tapp.QueueUpdateDraw(func() {
			for i, k := range want {
				if i >= len(metas) {
					break
				}
				if _, exists := a.scan.meta[k]; !exists {
					a.scan.meta[k] = metas[i]
				}
			}
			a.renderKeyList()
			a.keys.Select(idx-a.listOffset+1, 0)
		})
	}()
}

// nextValuePage advances pagination (forward-only for cursor types).
func (a *App) nextValuePage() {
	p := a.valPage
	if p == nil {
		return
	}
	switch p.kind {
	case "list":
		if p.start+keyview.PageLen < p.total {
			p.start += keyview.PageLen
			a.fetchValuePage()
		}
	default:
		if p.hasMore {
			a.fetchValuePage()
		}
	}
}

// prevValuePage rewinds where the type allows it.
func (a *App) prevValuePage() {
	p := a.valPage
	if p == nil {
		return
	}
	if p.kind == "list" && p.start > 0 {
		p.start -= keyview.PageLen
		if p.start < 0 {
			p.start = 0
		}
		p.lastID = ""
		a.fetchValuePage()
		return
	}
	a.flash("cursor pages are forward-only (r to restart)", a.th.Dim)
}
