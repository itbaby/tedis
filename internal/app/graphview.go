package app

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tedis/internal/jtree"
)

// graphView renders the value tree as a node-card diagram (jsoncrack-style,
// character edition): every node is a rounded card, parents connect to
// children with rail lines, the cursor moves node-to-node and folds
// branches. Fullscreen overlay, opened with g on the value pane.
type graphView struct {
	*tview.Box
	app        *App
	root       *jtree.Node
	cursor     *jtree.Node
	pos        map[*jtree.Node]graphPos
	showValues bool
	offX, offY int
	folded     map[*jtree.Node]bool // extra folds local to the graph
}

type graphPos struct {
	x, y, w, h int
}

const (
	cardW   = 18 // uniform card width keeps the layout arithmetic simple
	cardGap = 2  // horizontal gap between sibling cards
)

func newGraphView(a *App, root *jtree.Node, key string) *graphView {
	root = shallowCopyRoot(root, key)
	g := &graphView{
		Box:        tview.NewBox().SetBorder(true).SetTitle(" graph · ⏎ fold · ←↑↓→ navigate · v values · y copy · esc close ").SetTitleColor(a.th.Title),
		app:        a,
		root:       root,
		pos:        map[*jtree.Node]graphPos{},
		showValues: true,
		folded:     map[*jtree.Node]bool{},
	}
	g.layout()
	g.cursor = g.root
	g.SetInputCapture(g.keys)
	return g
}

func (g *graphView) keys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyEnter:
		if ev.Key() == tcell.KeyEscape {
			g.app.closeGraph()
			return nil
		}
		g.toggleFold()
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			g.app.closeGraph()
			return nil
		case 'l':
			g.moveCursor(childOf)
		case 'h':
			g.moveCursor(parentOf)
		case 'j':
			g.moveCursor(nextSiblingOf)
		case 'k':
			g.moveCursor(prevSiblingOf)
		case ' ', 10:
			g.toggleFold()
		case 'v':
			g.showValues = !g.showValues
			g.layout()
		case 'y':
			if g.cursor != nil {
				g.app.copyOSC52(g.cursor.Value)
			}
		default:
			return ev
		}
		return nil
	case tcell.KeyRight:
		g.moveCursor(childOf)
	case tcell.KeyLeft:
		g.moveCursor(parentOf)
	case tcell.KeyDown:
		g.moveCursor(nextSiblingOf)
	case tcell.KeyUp:
		g.moveCursor(prevSiblingOf)
	}
	return nil
}

func (g *graphView) toggleFold() {
	if g.cursor == nil || len(g.cursor.Children) == 0 {
		return
	}
	g.folded[g.cursor] = !g.folded[g.cursor]
	g.layout()
}

func (g *graphView) moveCursor(step func(n, from *jtree.Node, folded map[*jtree.Node]bool) *jtree.Node) {
	if g.cursor == nil {
		return
	}
	if next := step(g.root, g.cursor, g.folded); next != nil {
		g.cursor = next
		g.scrollIntoView()
	}
}

func (g *graphView) scrollIntoView() {
	p, ok := g.pos[g.cursor]
	if !ok {
		return
	}
	_, _, w, h := g.GetInnerRect()
	if p.x < g.offX {
		g.offX = p.x
	}
	if p.y < g.offY {
		g.offY = p.y
	}
	if p.x+cardW > g.offX+w {
		g.offX = p.x + cardW - w
	}
	if p.y+p.h > g.offY+h {
		g.offY = p.y + p.h - h
	}
}

// layout assigns card positions: leaves spread left-to-right, parents center
// over their children, depth grows downward.
func (g *graphView) layout() {
	g.pos = map[*jtree.Node]graphPos{}
	leaf := 0
	maxDepth := 0
	var place func(n *jtree.Node, depth int) int // returns center x
	place = func(n *jtree.Node, depth int) int {
		if depth > maxDepth {
			maxDepth = depth
		}
		h := 2 // label + bottom
		if g.showValues {
			h = 3 // label + value + bottom
		}
		y := depth * (h + 2)
		kids := visibleKids(n, g.folded)
		if len(kids) == 0 {
			x := leaf * (cardW + cardGap)
			leaf++
			g.pos[n] = graphPos{x: x, y: y, w: cardW, h: h}
			return x + cardW/2
		}
		var centers []int
		for _, k := range kids {
			centers = append(centers, place(k, depth+1))
		}
		cx := (centers[0] + centers[len(centers)-1]) / 2
		g.pos[n] = graphPos{x: cx - cardW/2, y: y, w: cardW, h: h}
		return cx
	}
	place(g.root, 0)
}

func visibleKids(n *jtree.Node, folded map[*jtree.Node]bool) []*jtree.Node {
	if n == nil || len(n.Children) == 0 || folded[n] {
		return nil
	}
	return n.Children
}

// ---- node navigation -----------------------------------------------------

func childOf(root, from *jtree.Node, folded map[*jtree.Node]bool) *jtree.Node {
	if kids := visibleKids(from, folded); len(kids) > 0 {
		return kids[0]
	}
	return nil
}

func parentOf(root, from *jtree.Node, folded map[*jtree.Node]bool) *jtree.Node {
	var find func(n *jtree.Node) *jtree.Node
	find = func(n *jtree.Node) *jtree.Node {
		for _, k := range visibleKids(n, folded) {
			if k == from {
				return n
			}
			if p := find(k); p != nil {
				return p
			}
		}
		return nil
	}
	return find(root)
}

func siblingOf(root, from *jtree.Node, folded map[*jtree.Node]bool, delta int) *jtree.Node {
	p := parentOf(root, from, folded)
	if p == nil {
		return nil
	}
	kids := visibleKids(p, folded)
	for i, k := range kids {
		if k == from {
			j := i + delta
			if j >= 0 && j < len(kids) {
				return kids[j]
			}
		}
	}
	return nil
}

func nextSiblingOf(root, from *jtree.Node, folded map[*jtree.Node]bool) *jtree.Node {
	return siblingOf(root, from, folded, +1)
}

func prevSiblingOf(root, from *jtree.Node, folded map[*jtree.Node]bool) *jtree.Node {
	return siblingOf(root, from, folded, -1)
}

// ---- drawing -------------------------------------------------------------

func (g *graphView) Draw(screen tcell.Screen) {
	g.Box.DrawForSubclass(screen, g)
	x0, y0, w, h := g.GetInnerRect()
	g.layoutIfNeeded()

	// pass 1: connectors, pass 2: cards (cards overwrite rail stubs)
	g.eachVisible(func(n *jtree.Node) {
		p := g.pos[n]
		kids := visibleKids(n, g.folded)
		if len(kids) == 0 {
			return
		}
		rail := p.y + p.h // row under the parent card
		var minC, maxC int
		for i, k := range kids {
			kc := g.pos[k].x + g.pos[k].w/2
			if i == 0 || kc < minC {
				minC = kc
			}
			if i == 0 || kc > maxC {
				maxC = kc
			}
		}
		parentC := p.x + p.w/2
		for c := minC; c <= maxC; c++ {
			g.put(screen, x0, y0, w, h, rail, c, '─', g.app.th.Dim)
		}
		g.put(screen, x0, y0, w, h, rail, parentC, '┴', g.app.th.Dim)
		for _, k := range kids {
			kc := g.pos[k].x + g.pos[k].w/2
			g.put(screen, x0, y0, w, h, rail, kc, '┬', g.app.th.Dim)
			g.put(screen, x0, y0, w, h, rail+1, kc, '│', g.app.th.Dim)
		}
	})
	g.eachVisible(func(n *jtree.Node) {
		g.drawCard(screen, n, x0, y0, w, h)
	})
}

func (g *graphView) layoutIfNeeded() {
	if len(g.pos) == 0 {
		g.layout()
	}
}

func (g *graphView) eachVisible(fn func(n *jtree.Node)) {
	var walk func(n *jtree.Node)
	walk = func(n *jtree.Node) {
		fn(n)
		for _, k := range visibleKids(n, g.folded) {
			walk(k)
		}
	}
	walk(g.root)
}

// put draws one cell with viewport clipping.
func (g *graphView) put(screen tcell.Screen, x0, y0, w, h int, row, col int, ch rune, c tcell.Color) {
	sx, sy := x0+col-g.offX, y0+row-g.offY
	if sx < x0 || sy < y0 || sx >= x0+w || sy >= y0+h {
		return
	}
	screen.SetContent(sx, sy, ch, nil, tcell.StyleDefault.Foreground(c))
}

func (g *graphView) drawCard(screen tcell.Screen, n *jtree.Node, x0, y0, w, h int) {
	p := g.pos[n]
	inner := cardW - 2
	label := fit(n.Label, inner)
	val := fit(g.cardValueText(n), inner)
	border := nodeColor(g.app.th, n)
	if n == g.cursor {
		border = g.app.th.BorderFocus
	}
	var mid rune = '─'
	// rounded top: ╭─ label ─╮
	g.put(screen, x0, y0, w, h, p.y, p.x, '╭', border)
	g.put(screen, x0, y0, w, h, p.y, p.x+p.w-1, '╮', border)
	for c := p.x + 1; c < p.x+p.w-1; c++ {
		g.put(screen, x0, y0, w, h, p.y, c, mid, border)
	}
	labelCol := p.x + 2
	for i, r := range label {
		g.put(screen, x0, y0, w, h, p.y, labelCol+i, r, g.app.th.Title)
	}
	rows := []struct {
		text  string
		color tcell.Color
	}{
		{val, g.valueColor(n)},
	}
	if !g.showValues {
		rows = nil
	}
	for ri, row := range rows {
		y := p.y + 1 + ri
		g.put(screen, x0, y0, w, h, y, p.x, '│', border)
		g.put(screen, x0, y0, w, h, y, p.x+p.w-1, '│', border)
		for i, r := range row.text {
			g.put(screen, x0, y0, w, h, y, p.x+2+i, r, row.color)
		}
		for c := p.x + 1; c < p.x+p.w-1; c++ {
			// blank fill so rails don't bleed through cards
			if c < p.x+2 || c >= p.x+2+len([]rune(row.text)) {
				g.put(screen, x0, y0, w, h, y, c, ' ', border)
			}
		}
	}
	bottom := p.y + p.h - 1
	g.put(screen, x0, y0, w, h, bottom, p.x, '╰', border)
	g.put(screen, x0, y0, w, h, bottom, p.x+p.w-1, '╯', border)
	for c := p.x + 1; c < p.x+p.w-1; c++ {
		g.put(screen, x0, y0, w, h, bottom, c, '─', border)
	}
}

func (g *graphView) cardValueText(n *jtree.Node) string {
	if n.IsBranch() {
		return jtree.Summary(n)
	}
	return nodeText(n)
}

func (g *graphView) valueColor(n *jtree.Node) tcell.Color {
	if n.IsBranch() {
		return g.app.th.Dim
	}
	return nodeColor(g.app.th, n)
}

func fit(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}

// openGraph shows the value tree as a fullscreen node-card diagram.
func (a *App) openGraph() {
	p := a.valPage
	if p == nil || p.tree == nil {
		a.flash("open a value first", a.th.Dim)
		return
	}
	a.graph = newGraphView(a, p.tree, p.key)
	a.pages.RemovePage("graph")
	a.openModals["graph"] = true
	a.pages.AddPage("graph", a.graph, true, true)
	a.tapp.SetFocus(a.graph)
}

// closeGraph dismisses the diagram.
func (a *App) closeGraph() {
	delete(a.openModals, "graph")
	a.pages.RemovePage("graph")
	a.tapp.SetFocus(a.value)
	a.applyFocusStyles()
}

// shallowCopyRoot gives the graph its own root node (labeled with the key)
// so folding in the diagram never mutates the value pane's tree.
func shallowCopyRoot(root *jtree.Node, key string) *jtree.Node {
	cp := *root
	cp.Label = key
	return &cp
}
