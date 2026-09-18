package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"tedis/internal/encode"
	"tedis/internal/keyview"
	"tedis/internal/theme"
)

// valuePage is the current page of the selected key's content.
type valuePage struct {
	key       string
	kind      string // redis type: string/hash/list/set/zset/stream
	raw       string // string payload before decoding
	codecName string // "", "auto", or codec name (v key cycles)

	cursor  uint64 // SCAN-family cursor (hash/set/zset)
	start   int64  // list offset
	total   int64  // list length
	lastID  string // stream high-water id
	hasMore bool

	rows      [][2]string // rendered rows (col1, col2)
	usedCodec string      // codec that produced the current rendering
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
		case "string":
			v, err := keyview.LoadString(ctx, c.Client, p.key)
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
				// keep the window anchored: show "…" marker for earlier items
				rows = append([][2]string{{fmt.Sprintf("… (%d earlier)", p.start), ""}}, rows...)
			}
		case "ReJSON-RL":
			v, err := keyview.LoadJSON(ctx, c.Client, p.key)
			if err != nil {
				a.valueErr(err)
				return
			}
			p.raw = v
			rows = a.stringRowsDecoded()
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
		return [][2]string{{"error", err.Error()}}
	}
	p.usedCodec = used.Name()
	return stringRows(text)
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
	if p == nil || p.kind != "string" {
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

func stringRows(v string) [][2]string {
	if !strings.Contains(v, "\n") {
		return [][2]string{{"value", v}}
	}
	lines := strings.Split(v, "\n")
	rows := make([][2]string, len(lines))
	for i, l := range lines {
		rows[i] = [2]string{strconv.Itoa(i + 1), l}
	}
	return rows
}

func formatScore(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
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
	if p.kind == "string" && p.usedCodec != "" {
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
	h1, h2 := valueHeaders(p.kind)
	a.value.SetCell(0, 0, cell(h1, a.th.Dim).SetSelectable(false))
	a.value.SetCell(0, 1, cell(h2, a.th.Dim).SetSelectable(false).SetExpansion(1))
	for i, r := range p.rows {
		c1 := cell(r[0], valueCol1Color(a.th, p.kind)).SetMaxWidth(24)
		if p.kind == "string" {
			c1 = cell(r[0], a.th.Dim).SetMaxWidth(8)
		}
		a.value.SetCell(i+1, 0, c1)
		a.value.SetCell(i+1, 1, cell(r[1], a.th.Text).SetExpansion(1))
	}
	if len(p.rows) == 0 {
		a.value.SetCell(1, 0, cell("(empty)", a.th.Dim).SetSelectable(false))
	}
	a.value.Select(1, 0)
}

func valueHeaders(kind string) (string, string) {
	switch kind {
	case "hash":
		return "field", "value"
	case "set":
		return "member", ""
	case "zset":
		return "member", "score"
	case "list":
		return "index", "value"
	case "stream":
		return "id", "fields"
	}
	return "line", "value"
}

func valueCol1Color(th theme.Theme, kind string) tcell.Color {
	switch kind {
	case "hash":
		return th.TypeString
	case "set":
		return th.TypeSet
	case "zset":
		return th.TypeZSet
	case "stream":
		return th.TypeStream
	}
	return th.Dim
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
	case "stream":
		if p.hasMore {
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
