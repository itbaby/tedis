package app

// adjustPane widens/narrows the focused pane by d columns: the tree and key
// list have their own width; the value pane widens by shrinking the key
// list (it holds the remaining space). Bounds keep the compact layout sane.
func (a *App) adjustPane(d int) {
	switch a.tapp.GetFocus() {
	case a.ns:
		a.nsW = clamp(a.nsW+d, 12, 60)
	case a.keys:
		a.keysW = clamp(a.keysW+d, 20, 110)
	case a.value:
		a.keysW = clamp(a.keysW-d, 20, 110)
	default:
		return
	}
	a.panes.ResizeItem(a.ns, a.nsW, 1)
	a.panes.ResizeItem(a.keys, a.keysW, 1)
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
