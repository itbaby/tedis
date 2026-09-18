package app

import (
	"encoding/base64"
	"fmt"
	"os"
)

// copyOSC52 sends text to the terminal clipboard via the OSC 52 escape
// sequence — the only mechanism that also works through SSH sessions
// (terminals and tmux ≥3.3 with set-clipboard on honor it).
func (a *App) copyOSC52(text string) {
	if text == "" {
		return
	}
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		a.flash("clipboard: "+err.Error(), a.th.Error)
		return
	}
	defer f.Close()
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
	if _, err := f.WriteString(seq); err != nil {
		a.flash("clipboard: "+err.Error(), a.th.Error)
		return
	}
	a.flash(fmt.Sprintf("copied %d bytes", len(text)), a.th.OK)
}

// copySelected dispatches y by focus: value pane copies the row's second
// column, the tree copies the selected prefix, the key list copies the key.
func (a *App) copySelected() {
	switch a.tapp.GetFocus() {
	case a.value, a.valueText:
		if a.valPage != nil {
			if a.valPage.decoded != "" {
				a.copyOSC52(a.valPage.decoded)
				return
			}
			if item, _, ok := a.valueSelectedRow(); ok {
				a.copyOSC52(item[1])
				return
			}
		}
	case a.ns:
		if row, _ := a.ns.GetSelection(); row > 0 && row < len(a.treeRows) {
			a.copyOSC52(a.treeRows[row].Prefix)
			return
		}
	}
	if k := a.selectedKey(); k != "" {
		a.copyOSC52(k)
	}
}
