package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tedis/internal/config"
	"tedis/internal/conn"
	fz "tedis/internal/fuzzy"
	"tedis/internal/scanner"
	"tedis/internal/theme"
)

// listWindow is how many key rows the table keeps rendered at once. The full
// key set lives in scan.keys; the table shows a movable window over it.
const listWindow = 60

// scanState is the current discovery session. All fields are owned by the UI
// goroutine; the scan goroutine only touches them via QueueUpdateDraw.
type scanState struct {
	cancel  context.CancelFunc
	pattern string
	fuzzy   string // non-empty: client-side fuzzy filter over a full scan
	tree    *scanner.Tree
	keys    []string
	seen    map[string]bool
	meta    map[string]conn.KeyMeta
	scanned int
	done    bool
}

func (a *App) resetScan(pattern string) {
	if a.scan != nil && a.scan.cancel != nil {
		a.scan.cancel()
	}
	a.scan = &scanState{
		pattern: pattern,
		tree:    scanner.NewTree(a.connProfile().Separator, a.connProfile().MaxFoldLevel),
		seen:    map[string]bool{},
		meta:    map[string]conn.KeyMeta{},
	}
	a.listOffset = 0
	a.valPage = nil
	a.value.Clear()
	a.value.SetTitle(" value ")
}

// connProfile returns the active connection's profile (sane defaults when
// disconnected).
func (a *App) connProfile() *config.Profile {
	if c := a.rc.Load(); c != nil {
		p := c.P
		if p.Separator == "" {
			p.Separator = ":"
		}
		if p.MaxFoldLevel <= 0 {
			p.MaxFoldLevel = 1
		}
		return p
	}
	return &config.Profile{Separator: ":", MaxFoldLevel: 1}
}

// startScan kicks off a cancellable SCAN for pattern and streams results
// into the tree + key list.
func (a *App) startScan(pattern string) {
	a.startScanF(pattern, "")
}

// startFuzzy scans everything and filters client-side with fzf-style
// ranking; the pattern bar keeps the ~query as the display form.
func (a *App) startFuzzy(terms string) {
	a.startScanF("~"+terms, terms)
}

func (a *App) startScanF(pattern, terms string) {
	c := a.rc.Load()
	if c == nil {
		a.flash("not connected", a.th.Error)
		return
	}
	a.resetScan(pattern)
	a.renderTree()
	a.renderKeyList()
	a.keys.SetTitle(fmt.Sprintf(" keys · %s · scanning… ", pattern))
	s := a.scan
	s.fuzzy = terms
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	profile := c.P
	count := profile.ScanCount
	scanMatch := pattern
	if terms != "" {
		scanMatch = "*"
	}

	go func() {
		err := scanner.Scan(ctx, c.Client, scanMatch, count, func(batch []string) bool {
			a.tapp.QueueUpdateDraw(func() {
				if terms != "" {
					batch = fz.Filter(terms, batch)
				}
				for _, k := range batch {
					if s.seen[k] {
						continue
					}
					s.seen[k] = true
					if terms == "" {
						s.tree.Add(k)
					}
					s.keys = append(s.keys, k)
					s.scanned++
				}
				if terms != "" {
					s.keys = fz.RankTop(terms, s.keys, 500)
				}
				a.renderTree()
				a.renderKeyList()
				a.keys.SetTitle(fmt.Sprintf(" keys · %s · %d… ", pattern, s.scanned))
			})
			return ctx.Err() == nil
		})
		a.tapp.QueueUpdateDraw(func() {
			s.done = true
			if err != nil && ctx.Err() == nil {
				a.flash("scan: "+err.Error(), a.th.Error)
			}
			a.keys.SetTitle(fmt.Sprintf(" keys · %s · %d ", pattern, s.scanned))
			a.renderTree()
			a.renderKeyList()
		})
	}()
}

// ---- namespace tree pane -------------------------------------------------

func (a *App) renderTree() {
	if a.scan == nil {
		return
	}
	if a.scan.fuzzy != "" {
		a.ns.Clear()
		a.ns.SetCell(1, 0, cell("~"+a.scan.fuzzy, a.th.Title))
		a.ns.SetCell(2, 0, cell(fmt.Sprintf("%d matched", len(a.scan.keys)), a.th.Dim))
		a.ns.Select(1, 0)
		a.treeRows = nil
		return
	}
	t := a.scan.tree
	rows := t.Rows()
	a.ns.Clear()
	a.ns.SetCell(0, 0, cell("*", a.th.Dim).SetSelectable(false))
	a.ns.SetCell(0, 1, cell(human(int64(t.Total())), a.th.Dim).SetSelectable(false).SetAlign(tview.AlignRight))
	a.treeRows = rows
	for i, r := range rows[1:] { // rows[0] is the synthetic root
		mark := "  "
		if r.Folder {
			mark = "▸ "
			if r.Expanded {
				mark = "▾ "
			}
		}
		label := strings.Repeat("  ", r.Depth) + mark + r.Label
		a.ns.SetCell(i+1, 0, cell(label, a.th.Text))
		a.ns.SetCell(i+1, 1, cell(human(int64(r.Count)), a.th.Dim).SetAlign(tview.AlignRight))
	}
	a.ns.Select(1, 0)
}

// treeEnter handles Enter on the namespace pane: folders toggle and refilter.
func (a *App) treeEnter() {
	if a.scan == nil {
		return
	}
	row, _ := a.ns.GetSelection()
	if row <= 0 || row >= len(a.treeRows) {
		return
	}
	r := a.treeRows[row]
	if r.Prefix == "" { // "*" root
		a.startScan("*")
		return
	}
	if a.scan.tree.Toggle(r.Prefix) {
		a.renderTree()
	}
	a.startScan(r.Prefix + a.connProfile().Separator + "*")
}

// ---- key list pane -------------------------------------------------------

func cell(text string, c tcell.Color) *tview.TableCell {
	return tview.NewTableCell(text).SetTextColor(c).SetExpansion(1)
}

func (a *App) renderKeyList() {
	if a.scan == nil {
		return
	}
	s := a.scan
	a.keys.Clear()
	for i, h := range []string{"key", "type", "ttl", "size"} {
		a.keys.SetCell(0, i, cell(h, a.th.Dim).SetSelectable(false).SetAlign(tview.AlignRight))
	}
	a.keys.GetCell(0, 0).SetAlign(tview.AlignLeft)

	end := a.listOffset + listWindow
	if end > len(s.keys) {
		end = len(s.keys)
	}
	for i := a.listOffset; i < end; i++ {
		a.fillKeyRow(i-a.listOffset+1, i)
	}
	if len(s.keys) == 0 {
		a.keys.SetCell(1, 0, cell("no keys", a.th.Dim).SetSelectable(false))
		return
	}
	a.keys.Select(1, 0)
}

func (a *App) fillKeyRow(displayRow, idx int) {
	s := a.scan
	k := s.keys[idx]
	a.keys.SetCell(displayRow, 0, cell(k, a.th.Text))
	m, ok := s.meta[k]
	if !ok {
		a.keys.SetCell(displayRow, 1, cell("…", a.th.Dim))
		a.keys.SetCell(displayRow, 2, cell("", a.th.Dim))
		a.keys.SetCell(displayRow, 3, cell("", a.th.Dim))
		return
	}
	a.keys.SetCell(displayRow, 1, cell(m.Type, typeColor(a.th, m.Type)).SetMaxWidth(7))
	a.keys.SetCell(displayRow, 2, cell(ttlText(m.TTL), ttlColor(a.th, m.TTL)).SetAlign(tview.AlignRight))
	a.keys.SetCell(displayRow, 3, cell(sizeText(m.Size), a.th.Dim).SetAlign(tview.AlignRight))
}

// keysSelectionChanged implements windowed scrolling + lazy metadata.
func (a *App) keysSelectionChanged(row, _ int) {
	if a.scan == nil || a.inFill {
		return
	}
	if row <= 0 {
		return
	}
	s := a.scan

	// advance/retreat the window when moving past its edges
	if row >= listWindow-2 && a.listOffset+listWindow < len(s.keys) {
		a.inFill = true
		a.listOffset += listWindow / 2
		a.renderKeyList()
		a.keys.Select(listWindow/2, 0)
		a.inFill = false
		return
	}
	if row <= 1 && a.listOffset > 0 {
		a.inFill = true
		a.listOffset -= listWindow / 2
		if a.listOffset < 0 {
			a.listOffset = 0
		}
		a.renderKeyList()
		a.keys.Select(listWindow/2, 0)
		a.inFill = false
		return
	}

	idx := a.listOffset + row - 1
	if idx < 0 || idx >= len(s.keys) {
		return
	}
	a.loadMeta(s.keys[idx], idx)
}

// loadMeta shows the selected key's value; on a metadata cache miss it
// debounces the fetch (time.AfterFunc) so fast scrolling never round-trips.
// Only the newest selection (metaSeq) may update the UI.
func (a *App) loadMeta(key string, idx int) {
	c := a.rc.Load()
	if c == nil {
		return
	}
	seq := a.metaSeq.Add(1)
	if m, ok := a.scan.meta[key]; ok {
		a.loadValue(key, m.Type)
		a.prefetchAround(idx)
		return
	}
	a.prefetchAround(idx)
	time.AfterFunc(80*time.Millisecond, func() {
		if a.metaSeq.Load() != seq { // selection moved on: skip the round trip
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			m, err := c.KeyMeta(ctx, key)
			a.tapp.QueueUpdateDraw(func() {
				if a.metaSeq.Load() != seq || a.scan == nil {
					return
				}
				if err != nil {
					m = conn.KeyMeta{Type: "?"}
				}
				a.scan.meta[key] = m
				row, _ := a.keys.GetSelection()
				if sel := a.listOffset + row - 1; row > 0 && sel >= 0 && sel < len(a.scan.keys) && a.scan.keys[sel] == key {
					a.fillKeyRow(row, sel)
				}
				a.loadValue(key, m.Type)
			})
		}()
	})
}

// ---- formatting helpers --------------------------------------------------

func ttlText(d time.Duration) string {
	switch {
	case d < 0:
		return "—"
	case d < time.Minute:
		return d.Truncate(time.Second).String()
	case d < time.Hour:
		return d.Truncate(time.Minute).String()
	}
	return (d / time.Hour).String() + "h"
}

func ttlColor(th theme.Theme, d time.Duration) tcell.Color {
	if d < 0 {
		return th.Dim
	}
	if d < time.Minute {
		return th.Error
	}
	return th.Dim
}

func sizeText(n int64) string {
	switch {
	case n < 0:
		return "—"
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
}

func typeColor(th theme.Theme, t string) tcell.Color {
	switch t {
	case kindString:
		return th.TypeString
	case kindHash:
		return th.TypeHash
	case kindList:
		return th.TypeList
	case kindSet:
		return th.TypeSet
	case kindZSet:
		return th.TypeZSet
	case kindStream:
		return th.TypeStream
	}
	return th.Dim
}

// openFilter switches the command bar into match-pattern mode: Enter
// restarts the scan with the typed glob, Esc restores the query bar.
func (a *App) openFilter() {
	if a.rc.Load() == nil {
		a.flash("not connected", a.th.Error)
		return
	}
	cur := "*"
	if a.scan != nil {
		cur = a.scan.pattern
	}
	a.cmd.SetLabel(" / ").SetLabelColor(a.th.Dim).SetText("").
		SetPlaceholder("substring · ~fuzzy · * ? glob · current: " + cur).
		SetPlaceholderStyle(tcell.StyleDefault.Foreground(a.th.Dim))
	a.tapp.SetFocus(a.cmd)
	a.cmd.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			p := strings.TrimSpace(a.cmd.GetText())
			a.restoreCmdBar()
			if strings.HasPrefix(p, "~") {
				if q := strings.TrimSpace(p[1:]); q != "" {
					a.startFuzzy(q)
				}
				return
			}
			switch {
			case p == "":
				p = "*"
			case strings.ContainsAny(p, "*?["):
				// expert glob, passed through to MATCH verbatim
			default:
				p = "*" + p + "*" // substring search, the intuitive default
			}
			a.startScan(p)
			return
		}
		if key == tcell.KeyEsc {
			a.restoreCmdBar()
		}
	})
}

func (a *App) restoreCmdBar() {
	a.cmd.SetLabel(" ❯ ").SetLabelColor(a.th.Read).SetText("").SetDoneFunc(nil).
		SetPlaceholder(cmdPlaceholder)
	a.tapp.SetFocus(a.keys)
	a.applyFocusStyles()
}
