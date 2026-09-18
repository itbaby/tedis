// Package app is the tview application shell: layout, focus management,
// overlays (connection manager, help), global key routing and status bar.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync/atomic"
	"tedis/internal/scanner"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tedis/internal/config"
	"tedis/internal/conn"
	"tedis/internal/i18n"
	"tedis/internal/theme"
)

// App owns the whole UI.
type App struct {
	tapp    *tview.Application
	cfg     *config.Config
	cfgPath string
	th      theme.Theme
	log     *slog.Logger

	pages *tview.Pages

	ns    *tview.Table // namespace tree (filled in phase 2)
	keys  *tview.Table // key list     (filled in phase 2)
	value *tview.Table // value view   (filled in phase 2)
	cmd   *tview.InputField

	statusL *tview.TextView
	statusR *tview.TextView

	scan       *scanState
	treeRows   []scanner.Row
	listOffset int
	inFill     bool
	metaSeq    int
	valPage    *valuePage
	query      *queryPage
	focusOrder []tview.Primitive

	rc    atomic.Pointer[conn.Conn]
	alert bool
}

// New wires everything up. Run() starts the event loop.
func New(cfg *config.Config, cfgPath string, log *slog.Logger) *App {
	a := &App{
		tapp:    tview.NewApplication(),
		cfg:     cfg,
		cfgPath: cfgPath,
		th:      theme.ByName(cfg.Settings.Theme),
		log:     log,
	}
	a.build()
	return a
}

func (a *App) build() {
	theme.Apply(a.th)

	a.ns = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	a.ns.SetBorder(true).SetTitle(" connections ").SetTitleColor(a.th.Title).SetTitleAlign(tview.AlignLeft)
	a.keys = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	a.keys.SetBorder(true).SetTitle(" keys ").SetTitleColor(a.th.Title).SetTitleAlign(tview.AlignLeft)
	a.value = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	a.value.SetBorder(true).SetTitle(" value ").SetTitleColor(a.th.Title).SetTitleAlign(tview.AlignLeft)
	for _, t := range []*tview.Table{a.ns, a.keys, a.value} {
		t.SetSelectedStyle(tcell.StyleDefault.Background(a.th.SelBg).Foreground(a.th.SelFg))
	}

	a.cmd = tview.NewInputField().
		SetLabel(" ❯ ").
		SetLabelColor(a.th.Read).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetPlaceholder("query (phase 3)").
		SetPlaceholderStyle(tcell.StyleDefault.Foreground(a.th.Dim))

	a.keys.SetSelectionChangedFunc(a.keysSelectionChanged)
	a.ns.SetSelectedFunc(func(int, int) { a.treeEnter() })
	a.keys.SetInputCapture(a.keysLocalKeys)
	a.value.SetInputCapture(a.valueLocalKeys)

	a.statusL = tview.NewTextView().SetDynamicColors(true)
	a.statusR = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight)
	a.setHints()

	panes := tview.NewFlex().
		AddItem(a.ns, 26, 1, false).
		AddItem(a.keys, 40, 1, true).
		AddItem(a.value, 0, 3, false)
	status := tview.NewFlex().
		AddItem(a.statusL, 0, 3, false).
		AddItem(a.statusR, 0, 2, false)
	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(panes, 0, 1, true).
		AddItem(a.cmd, 1, 0, false).
		AddItem(status, 1, 0, false)

	a.pages = tview.NewPages().AddPage("main", root, true, true)
	a.focusOrder = []tview.Primitive{a.ns, a.keys, a.value, a.cmd}

	a.tapp.SetInputCapture(a.globalKeys).
		SetRoot(a.pages, true).
		EnableMouse(true).
		SetFocus(a.keys)

	a.setStatusDisconnected()
}

// Run enters the tview event loop.
func (a *App) Run() error { return a.tapp.Run() }

// ---- global keys -------------------------------------------------------

func (a *App) globalKeys(ev *tcell.EventKey) *tcell.EventKey {
	if ev.Key() == tcell.KeyCtrlC {
		a.tapp.Stop()
		return nil
	}
	// While typing (command bar or any modal input), only Tab/Backtab are
	// intercepted: single-letter global shortcuts must never eat keystrokes
	// meant for a text field.
	if a.inTextInput() {
		switch ev.Key() {
		case tcell.KeyTab, tcell.KeyBacktab:
			// Tab moves between panes only when a pane has focus; inside a
			// modal it must stay with the form's own field navigation.
			if a.focusInPanes() {
				if ev.Key() == tcell.KeyTab {
					a.cycleFocus(+1)
				} else {
					a.cycleFocus(-1)
				}
				return nil
			}
		}
		return ev
	}
	switch ev.Key() {
	case tcell.KeyTab:
		a.cycleFocus(+1)
		return nil
	case tcell.KeyBacktab:
		a.cycleFocus(-1)
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case '/':
			a.openFilter()
			return nil
		case ':':
			a.openQuery()
			return nil
		case 'i':
			a.openInfo()
			return nil
		case 's':
			a.openSettings()
			return nil
		case 'r':
			if a.scan != nil {
				a.startScan(a.scan.pattern)
			}
			return nil
		case 'q':
			a.tapp.Stop()
			return nil
		case 'c':
			a.openConnect()
			return nil
		case 'a':
			a.alert = !a.alert
			a.flash(fmt.Sprintf("alert mode %s", onOff(a.alert)), a.th.Warn)
			a.setHints()
			return nil
		case '?':
			a.openHelp()
			return nil
		default:
			if a.dbKey(ev.Rune()) {
				return nil
			}
		}
	}
	return ev
}

// inTextInput reports whether the focused primitive (or an open modal) is in
// text-entry mode, where letter keys must pass through untouched.
func (a *App) inTextInput() bool {
	switch a.tapp.GetFocus().(type) {
	case *tview.InputField, *tview.TextArea, *tview.Form, *tview.Button,
		*tview.Checkbox, *tview.DropDown:
		return true
	}
	for _, name := range []string{"confirm", "prompt", "editor", "connect", "help"} {
		if a.pages.HasPage(name) {
			return true
		}
	}
	return false
}

// focusInPanes reports whether focus is on one of the main panes (not a
// modal widget).
func (a *App) focusInPanes() bool {
	f := a.tapp.GetFocus()
	for _, p := range a.focusOrder {
		if p == f {
			return true
		}
	}
	return false
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (a *App) cycleFocus(d int) {
	for i, p := range a.focusOrder {
		if p == a.tapp.GetFocus() {
			next := (i + d + len(a.focusOrder)) % len(a.focusOrder)
			a.tapp.SetFocus(a.focusOrder[next])
			a.applyFocusStyles()
			return
		}
	}
	a.tapp.SetFocus(a.focusOrder[0])
	a.applyFocusStyles()
}

// applyFocusStyles colors the focused pane's border brighter than the rest.
func (a *App) applyFocusStyles() {
	for _, p := range a.focusOrder {
		c := a.th.Border
		if p == a.tapp.GetFocus() {
			c = a.th.BorderFocus
		}
		if b, ok := p.(interface{ SetBorderColor(tcell.Color) *tview.Box }); ok {
			b.SetBorderColor(c)
		}
	}
}

func (a *App) showModal(name string, p tview.Primitive, w, h int) {
	centered := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(
			tview.NewFlex().SetDirection(tview.FlexColumn).
				AddItem(nil, 0, 1, false).
				AddItem(p, w, 1, true).
				AddItem(nil, 0, 1, false),
			h, 1, true,
		).
		AddItem(nil, 0, 1, false)
	a.pages.AddPage(name, centered, true, true)
}

func (a *App) closeModal(name string) {
	a.pages.RemovePage(name)
	a.tapp.SetFocus(a.keys)
}

func (a *App) openHelp() {
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetBorder(true).SetTitle(" help ").SetTitleColor(a.th.Title)
	tv.SetText(helpText(a.th))
	tv.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == 'q' {
			a.closeModal("help")
			return nil
		}
		return ev
	})
	a.showModal("help", tv, 56, 14)
}

func helpText(th theme.Theme) string {
	dim, hi := hex(th.Dim), hex(th.Title)
	return fmt.Sprintf(`[%s]navigation[-]
  [%s]Tab[-]%s cycle panes        [%s]arrows[-]%s move selection
  [%s]/[-]%s filter keys          [%s]r[-]%s rescan      [%s]:[-]%s query

[%s]key list[-]
  [%s]d[-]%s delete               [%s]t[-]%s ttl            [%s]m[-]%s rename

[%s]value pane[-]
  [%s]e/⏎[-]%s edit item          [%s]d[-]%s delete item    [%s]n[-]%s new item
  [%s].[-]%s next page            [%s],[-]%s prev page (list)

[%s]connection[-]
  [%s]c[-]%s connect / profiles   [%s]a[-]%s alert mode     [%s]q[-]%s quit`,
		dim, hi, dim, hi, dim, hi, dim, hi, dim, hi, dim,
		dim, hi, dim, hi, dim, hi, dim,
		dim, hi, dim, hi, dim, hi, dim, hi, dim, hi, dim,
		dim, hi, dim, hi, dim, hi, dim)
}

// ---- status ------------------------------------------------------------

func (a *App) setHints() {
	h := fmt.Sprintf("[%s]c[%s] %s  [%s]:[%s] %s  [%s]/[%s] %s  [%s]r[%s] %s  [%s]i[%s] %s [%s]s[%s] %s  [%s]a[%s] %s:%s  [%s]?[%s] %s  [%s]q[%s] %s",
		hex(a.th.Title), hex(a.th.Dim), i18n.T("conn"),
		hex(a.th.Title), hex(a.th.Dim), i18n.T("query"),
		hex(a.th.Title), hex(a.th.Dim), i18n.T("filter"),
		hex(a.th.Title), hex(a.th.Dim), i18n.T("rescan"),
		hex(a.th.Title), hex(a.th.Dim), i18n.T("info"),
		hex(a.th.Title), hex(a.th.Dim), i18n.T("settings"),
		hex(a.th.Title), hex(a.th.Dim), i18n.T("alert"), onOff(a.alert),
		hex(a.th.Title), hex(a.th.Dim), i18n.T("help"),
		hex(a.th.Title), hex(a.th.Dim), i18n.T("quit"))
	a.statusR.SetText(h)
}

func (a *App) setStatusDisconnected() {
	a.statusL.SetText(fmt.Sprintf("[%s]%s", hex(a.th.Dim), i18n.T("not connected")))
}

func (a *App) setStatusConnected(p *config.Profile, c *conn.Conn, nkeys int64, lat time.Duration) {
	a.statusL.SetText(fmt.Sprintf("[%s]%s[-] · [%s]db%d[-] · Redis %s · [%s]%s keys[-] · %s",
		hex(a.th.Title), p.Name, hex(a.th.Dim), p.DB, c.Info.Version,
		hex(a.th.Text), human(nkeys), lat.Round(time.Microsecond)))
}

func (a *App) flash(msg string, c tcell.Color) {
	a.statusL.SetText(fmt.Sprintf("[%s]%s", hex(c), msg))
	go func() {
		time.Sleep(3 * time.Second)
		a.tapp.QueueUpdateDraw(func() {
			if c := a.rc.Load(); c != nil {
				if n, err := c.DBSize(context.Background()); err == nil {
					if lat, err := c.Ping(context.Background()); err == nil {
						a.setStatusConnected(c.P, c, n, lat)
						return
					}
					a.setStatusConnected(c.P, c, n, 0)
					return
				}
			}
			a.setStatusDisconnected()
		})
	}()
}

func netAddr(p *config.Profile) string {
	return net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
}

func human(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return strconv.FormatInt(n, 10)
}

func hex(c tcell.Color) string {
	r, g, b := c.RGB()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r), uint8(g), uint8(b))
}

// keysLocalKeys: per-key operations on the key list.
func (a *App) keysLocalKeys(ev *tcell.EventKey) *tcell.EventKey {
	if ev.Key() != tcell.KeyRune {
		return ev
	}
	switch ev.Rune() {
	case 'd':
		if k := a.selectedKey(); k != "" {
			a.deleteKey(k)
		}
		return nil
	case 't':
		if k := a.selectedKey(); k != "" {
			a.editKeyTTL(k)
		}
		return nil
	case 'm':
		if k := a.selectedKey(); k != "" {
			a.renameKey(k)
		}
		return nil
	}
	return ev
}

// valueLocalKeys: item operations + pagination on the value pane.
func (a *App) valueLocalKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEnter:
		a.editValueItem()
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'e':
			a.editValueItem()
			return nil
		case 'd':
			a.delValueItem()
			return nil
		case 'n':
			a.newValueItem()
			return nil
		case 'v':
			a.cycleValueCodec()
			return nil
		case '.':
			a.nextValuePage()
			return nil
		case ',':
			a.prevValuePage()
			return nil
		}
	}
	return ev
}
