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
	"tedis/internal/theme"
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

// Redis key kinds used throughout the value layer (from TYPE).
const (
	kindString = "string"
	kindHash   = "hash"
	kindList   = "list"
	kindSet    = "set"
	kindZSet   = "zset"
	kindStream = "stream"
	kindJSON   = "ReJSON-RL" // RedisJSON module type
)

// isTextKind reports the whole-payload kinds (one string body) as opposed to
// the container kinds (many items).
func isTextKind(kind string) bool { return kind == kindString || kind == kindJSON }

// loadValue opens the typed view for a key (called when selection settles).
func (a *App) loadValue(key, kind string) {
	a.valPage = &valuePage{key: key, kind: kind}
	a.fetchValuePage()
}

// fetchValuePage loads one page in the background. The goroutine must not
// touch p or app state: inputs are snapshotted up front and results are only
// applied inside QueueUpdateDraw (which runs on the UI goroutine).
func (a *App) fetchValuePage() {
	p := a.valPage
	if p == nil {
		return
	}
	c := a.rc.Load()
	if c == nil {
		return
	}
	key, kind := p.key, p.kind
	cursor, start, lastID := p.cursor, p.start, p.lastID
	var codec encode.Codec
	if isTextKind(kind) {
		codec = a.valueCodec()
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var rows [][2]string
		var more bool
		var raw, decoded, usedName string
		var nextCur uint64
		var total int64
		lastIDOut := lastID
		switch kind {
		case kindString, kindJSON:
			var v string
			var err error
			if kind == kindJSON {
				v, err = keyview.LoadJSON(ctx, c.Client, key)
			} else {
				v, err = keyview.LoadString(ctx, c.Client, key)
			}
			if err != nil {
				a.valueErr(err)
				return
			}
			raw = v
			text, used, ferr := encode.Format([]byte(v), codec)
			if ferr != nil {
				rows = [][2]string{{"error", ferr.Error()}}
			} else {
				decoded, usedName = text, used.Name()
				rows = [][2]string{{"value", text}}
			}
		case kindHash:
			next, items, err := keyview.LoadHash(ctx, c.Client, key, cursor, 0)
			if err != nil {
				a.valueErr(err)
				return
			}
			for _, it := range items {
				rows = append(rows, [2]string{it.Field, it.Value})
			}
			nextCur, more = next, next != 0
		case kindSet:
			next, members, err := keyview.LoadSet(ctx, c.Client, key, cursor, 0)
			if err != nil {
				a.valueErr(err)
				return
			}
			for _, m := range members {
				rows = append(rows, [2]string{m, ""})
			}
			nextCur, more = next, next != 0
		case kindZSet:
			next, items, err := keyview.LoadZSet(ctx, c.Client, key, cursor, 0)
			if err != nil {
				a.valueErr(err)
				return
			}
			for _, it := range items {
				rows = append(rows, [2]string{it.Member, formatScore(it.Score)})
			}
			nextCur, more = next, next != 0
		case kindList:
			items, tot, err := keyview.LoadList(ctx, c.Client, key, start, 0)
			if err != nil {
				a.valueErr(err)
				return
			}
			for _, it := range items {
				rows = append(rows, [2]string{strconv.FormatInt(it.Index, 10), it.Value})
			}
			total = tot
			more = start+int64(len(items)) < tot
			if start > 0 {
				rows = append([][2]string{{fmt.Sprintf("… (%d earlier)", start), ""}}, rows...)
			}
		case kindStream:
			items, err := keyview.LoadStream(ctx, c.Client, key, lastID, 0)
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
				lastIDOut = it.ID
			}
			more = len(items) > 0
		default:
			rows = [][2]string{{"(unsupported type: " + kind + ")", ""}}
		}
		a.tapp.QueueUpdateDraw(func() {
			if a.valPage != p { // selection moved on while this fetch was in flight
				return
			}
			p.raw, p.rows, p.hasMore = raw, rows, more
			p.usedCodec, p.decoded = usedName, decoded
			switch kind {
			case kindHash, kindSet, kindZSet:
				p.cursor = nextCur
			case kindList:
				p.total = total
			case kindStream:
				p.lastID = lastIDOut
			}
			p.tree = nil // page changed: rebuild the tree on the next render
			a.renderValue()
		})
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
	if p == nil || !isTextKind(p.kind) {
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
	a.fetchValuePage() // re-decode + rebuild the tree with the new codec
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
	case kindString, kindJSON:
		if t := jtree.FromJSON(p.decoded); t != nil {
			return t.WithItem(0)
		}
		if n := jtree.ScalarLeaf("", p.decoded); n != nil {
			return n.WithItem(0)
		}
	case kindSet: // array of members
		b := &jtree.Node{Kind: jtree.KindArray, Expanded: true}
		for i, r := range p.rows {
			if strings.HasPrefix(r[0], "… (") {
				continue
			}
			b.Children = append(b.Children, nodeForRaw("", r[0]).WithItem(i))
		}
		return b
	case kindZSet: // object member → score
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
		array := p.kind == kindList || p.kind == kindStream
		b := &jtree.Node{Kind: jtree.KindObject, Expanded: true}
		if array {
			b.Kind = jtree.KindArray
		}
		for i, r := range p.rows {
			if strings.HasPrefix(r[0], "… (") { // pagination anchor row
				b.Children = append(b.Children, jtree.Leaf(r[0], "", jtree.KindText).WithItem(i))
				continue
			}
			if p.kind == kindStream {
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
	if kind == kindJSON {
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
	if isTextKind(p.kind) && p.usedCodec != "" {
		t += fmt.Sprintf(" · %s", p.usedCodec)
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
		a.value.SetCell(i+1, 1, cell(valueText(r.Node), nodeColor(a.th, r.Node)))
	}
	a.renderValueTitle(len(p.treeRows))
	if len(p.treeRows) == 0 {
		a.value.SetCell(1, 0, cell("(empty)", a.th.Dim).SetSelectable(false))
	}
	a.value.Select(1, 0)
}

// nodeText renders one leaf's display text (value pane + graph cards).
func nodeText(n *jtree.Node) string {
	return jtree.ScalarText(n)
}

// valueText is the value-pane form of nodeText: long text capped so wide
// payloads don't thrash the table layout.
func valueText(n *jtree.Node) string {
	t := nodeText(n)
	if n.Kind == jtree.KindText {
		t = fit(t, 200)
	}
	return t
}

// nodeColor maps a tree node's kind to its theme color.
func nodeColor(th theme.Theme, n *jtree.Node) tcell.Color {
	switch n.Kind {
	case jtree.KindString:
		return th.JSONString
	case jtree.KindNumber:
		return th.JSONNumber
	case jtree.KindBool:
		return th.JSONBool
	case jtree.KindNull:
		return th.JSONNull
	case jtree.KindObject:
		return th.Title
	case jtree.KindArray:
		return th.TypeZSet
	}
	return th.Text
}

func (a *App) labelColor(r jtree.Row) tcell.Color {
	if r.Depth == 1 {
		return a.th.Title
	}
	return a.th.Text
}

// selectedTreeNode returns the tree node under the value-pane cursor
// (nil when there is none).
func (a *App) selectedTreeNode() *jtree.Node {
	p := a.valPage
	if p == nil || len(p.treeRows) == 0 {
		return nil
	}
	row, _ := a.value.GetSelection()
	if row <= 0 || row > len(p.treeRows) {
		return nil
	}
	return p.treeRows[row-1].Node
}

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

// valueToggle flips the branch under the cursor (Enter/l on the value pane).
func (a *App) valueToggle() {
	p := a.valPage
	if p == nil || p.tree == nil {
		return
	}
	row, _ := a.value.GetSelection()
	if row <= 0 || row > len(p.treeRows) {
		return
	}
	r := p.treeRows[row-1]
	if !r.Branch {
		return
	}
	r.Node.Toggle()
	a.renderValue()
	if row > len(p.treeRows) { // branch folded away: keep cursor in range
		row = len(p.treeRows)
	}
	a.value.Select(row, 0)
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
		if i == idx { // the caller fetches the selected key itself (incl. size)
			continue
		}
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
	case kindList:
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
	if p.kind == kindList && p.start > 0 {
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
