// Package app is the tview application shell: layout, focus management,
// overlays (connection manager, help), global key routing and status bar.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
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
	// modalPrev is the focused primitive when the first modal opened;
	// closeModal returns focus there.
	modalPrev tview.Primitive
	tapp      *tview.Application
	cfg       *config.Config
	cfgPath   string
	th        theme.Theme
	log       *slog.Logger

	pages *tview.Pages
	panes *tview.Flex

	ns    *tview.Table // namespace tree
	keys  *tview.Table // key list
	value *tview.Table // value view: table mode
	cmd   *tview.InputField

	statusL *tview.TextView
	statusR *tview.TextView

	nsW, keysW int // pane widths (adjustable with < and >)

	graph      *graphView
	scan       *scanState
	treeRows   []scanner.Row
	listOffset int
	inFill     bool
	valPage    *valuePage
	query      *queryPage
	splash     *tview.TextView
	focusOrder []tview.Primitive

	// metaSeq/flashSeq are read from fetch goroutines/timers, hence atomic;
	// both implement "only the latest request may update state".
	metaSeq  atomic.Uint64
	flashSeq atomic.Uint64

	openModals []string  // stack of visible modal/overlay pages, bottom → top
	lastQ      time.Time // last bare-UI q press (double-q quits)
	typingIn   bool      // a tracked text field currently has focus

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
		SetPlaceholder(cmdPlaceholder).
		SetPlaceholderStyle(tcell.StyleDefault.Foreground(a.th.Dim))
	a.cmd.SetFocusFunc(func() { a.typingIn = true })
	a.cmd.SetBlurFunc(func() { a.typingIn = false })

	a.keys.SetSelectionChangedFunc(a.keysSelectionChanged)
	a.ns.SetSelectedFunc(func(int, int) { a.treeEnter() })
	a.keys.SetInputCapture(a.keysLocalKeys)
	a.value.SetInputCapture(a.valueLocalKeys)

	a.statusL = tview.NewTextView().SetDynamicColors(true)
	a.statusR = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight)
	a.setHints()

	a.nsW, a.keysW = 26, 40
	a.panes = tview.NewFlex().
		AddItem(a.ns, a.nsW, 1, false).
		AddItem(a.keys, a.keysW, 1, true).
		AddItem(a.value, 0, 3, false)
	panes := a.panes
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
func (a *App) Run() error {
	a.showSplash()
	return a.tapp.Run()
}

// ---- global keys -------------------------------------------------------

func (a *App) globalKeys(ev *tcell.EventKey) *tcell.EventKey {
	if ev.Key() == tcell.KeyCtrlC {
		a.tapp.Stop()
		return nil
	}
	// q is the universal exit: one press closes the top window; on the bare
	// UI a second press within quitWindow quits the app. While a text field
	// has focus, q belongs to the text (the filter/query bars implement
	// their own empty-field q shortcut).
	if ev.Key() == tcell.KeyRune && ev.Rune() == 'q' && a.splash == nil && !a.typingIn && !a.fieldFocused() {
		if top := a.topModal(); top != "" {
			a.closeModal(top)
			return nil
		}
		if time.Since(a.lastQ) <= quitWindow {
			a.tapp.Stop()
			return nil
		}
		a.lastQ = time.Now()
		a.flash("press q again to quit", a.th.Warn)
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
	// ⌘/⌥ + ←→ jump between panes (Meta arrives via the kitty keyboard
	// protocol; Alt works in every terminal), but never while typing —
	// mod-arrows there belong to the text field (word movement).
	case tcell.KeyLeft:
		if ev.Modifiers()&(tcell.ModMeta|tcell.ModAlt) != 0 {
			a.cycleFocus(-1)
			return nil
		}
	case tcell.KeyRight:
		if ev.Modifiers()&(tcell.ModMeta|tcell.ModAlt) != 0 {
			a.cycleFocus(+1)
			return nil
		}
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
		case '<':
			a.adjustPane(-2)
			return nil
		case '>':
			a.adjustPane(+2)
			return nil
		case 's':
			a.openSettings()
			return nil
		case 'r':
			if a.scan != nil {
				if a.scan.fuzzy != "" {
					a.startFuzzy(a.scan.fuzzy)
				} else {
					a.startScan(a.scan.pattern)
				}
			}
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

// quitWindow is how quickly the second q must land to quit the app.
const quitWindow = 1500 * time.Millisecond

// typingFocus reports whether the focused primitive is a text-entry widget,
// where letters belong to the field rather than to keybindings.
func (a *App) typingFocus() bool {
	switch a.tapp.GetFocus().(type) {
	case *tview.InputField, *tview.TextArea, *tview.Form, *tview.Button,
		*tview.Checkbox, *tview.DropDown:
		return true
	}
	return false
}

// fieldFocused is the narrower q-gate: only widgets that actually consume
// typed characters swallow the quit/close key (buttons and checkboxes don't).
func (a *App) fieldFocused() bool {
	switch a.tapp.GetFocus().(type) {
	case *tview.InputField, *tview.TextArea, *tview.Form, *tview.DropDown:
		return true
	}
	return false
}

// inTextInput reports whether the focused primitive (or an open modal) is in
// text-entry mode, where letter keys must pass through untouched.
func (a *App) inTextInput() bool {
	return a.typingFocus() || len(a.openModals) > 0
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

// cmdPlaceholder is the command bar's idle hint.
const cmdPlaceholder = "query  ·  : opens the query view"

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
	f := a.tapp.GetFocus()
	for _, p := range a.focusOrder {
		c := a.th.Border
		if p == f {
			c = a.th.BorderFocus
		}
		if b, ok := p.(interface{ SetBorderColor(tcell.Color) *tview.Box }); ok {
			b.SetBorderColor(c)
		}
	}
}

func (a *App) showModal(name string, p tview.Primitive, w, h int) {
	if len(a.openModals) == 0 { // remember which pane opened the modal
		a.modalPrev = a.tapp.GetFocus()
	}
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
	a.pushModal(name)
	a.pages.AddPage(name, centered, true, true)
}

func (a *App) pushModal(name string) {
	if !slices.Contains(a.openModals, name) {
		a.openModals = append(a.openModals, name)
	}
}

func (a *App) popModal(name string) {
	a.openModals = slices.DeleteFunc(a.openModals, func(m string) bool { return m == name })
}

// topModal is the most recently opened visible modal ("" when none).
func (a *App) topModal() string {
	if len(a.openModals) == 0 {
		return ""
	}
	return a.openModals[len(a.openModals)-1]
}

func (a *App) closeModal(name string) {
	a.popModal(name)
	a.pages.RemovePage(name)
	if a.modalPrev != nil {
		a.tapp.SetFocus(a.modalPrev) // back to the pane we launched from
	} else {
		a.tapp.SetFocus(a.keys)
	}
}

func (a *App) openHelp() {
	tv := tview.NewTextView().SetDynamicColors(true).SetTextColor(a.th.Text)
	tv.SetBorder(true).SetTitle(" help · esc/q close ").SetTitleColor(a.th.Title)
	tv.SetText(helpText(a.th))
	tv.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == 'q' {
			a.closeModal("help")
			return nil
		}
		return ev
	})
	a.showModal("help", tv, 56, 20)
}

// helpText lays out the keybind cheatsheet. Colors use proper [#hex]…[-]
// spans and each entry is a literal string, so column spacing stays exact —
// dynamic-color tags are invisible to the terminal's cell layout.
func helpText(th theme.Theme) string {
	hi, dim, sec := hex(th.Title), hex(th.Dim), hex(th.TypeHash)
	key := func(s string) string { return "[" + hi + "]" + s + "[-]" }
	desc := func(s string) string { return "[" + dim + "]" + s + "[-]" }
	hdr := func(s string) string { return "[" + sec + "]▸ " + s + "[-]" }

	return strings.Join([]string{
		hdr("navigation"),
		"  " + key("Tab") + desc(" cycle panes     ") + key("/") + desc(" filter keys"),
		"  " + key("arrows") + desc(" move selection  ") + key(":") + desc(" query console"),
		"  " + key("⌘/⌥←→") + desc(" jump panes       ") + key("r") + desc(" rescan"),
		"",
		hdr("key list"),
		"  " + key("d") + desc(" delete   ") + key("t") + desc(" ttl   ") + key("m") + desc(" rename   ") + key("y") + desc(" copy"),
		"",
		hdr("value pane"),
		"  " + key("⏎") + desc(" edit/fold    ") + key("e") + desc(" edit row   ") + key("v") + desc(" codec"),
		"  " + key("d") + desc(" delete item  ") + key("n") + desc(" new item   ") + key("g") + desc(" graph"),
		"  " + key("l / ←→") + desc(" fold     ") + key(".") + desc(" next page  ") + key(",") + desc(" prev"),
		"  " + key("f") + desc(" find json  ") + key("] [") + desc(" next/prev match"),
		"",
		hdr("connection"),
		"  " + key("c") + desc(" connect   ") + key("a") + desc(" alert   ") + key("i") + desc(" info   ") + key("s") + desc(" settings"),
		"  " + key("?") + desc(" this help    ") + key("q") + desc(" close window · ") + key("qq") + desc(" quit"),
	}, "\n")
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
	seq := a.flashSeq.Add(1)
	go func() {
		time.Sleep(3 * time.Second)
		if a.flashSeq.Load() != seq {
			return // a newer flash owns the status bar now
		}
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

// Form items get their style rebuilt by Form.Draw every frame from bare
// colors (SetFormAttributes drops decorations), so per-field underline styles
// never survive. These add helpers wrap fields so the underline is re-applied
// from inside that hook; the "_"-run placeholder draws the bottom line for
// empty fields, where an underline style has no glyphs to mark.

func (a *App) addInput(form *tview.Form, label, value string, width int, changed func(string)) {
	form.AddFormItem(underlinedField{FormItem: a.newInput(label, value, width, changed)})
}

func (a *App) addPassword(form *tview.Form, label, value string, width int, changed func(string)) {
	in := a.newInput(label, value, width, changed)
	in.SetMaskCharacter('*')
	form.AddFormItem(underlinedField{FormItem: in})
}

// newInput builds a form input that also advertises its focus state, since
// Application.GetFocus can't identify text fields reliably (Box doesn't
// propagate the focus delegate).
func (a *App) newInput(label, value string, width int, changed func(string)) *tview.InputField {
	in := tview.NewInputField().
		SetLabel(label).SetText(value).SetFieldWidth(width).SetChangedFunc(changed)
	if width > 0 {
		in.SetPlaceholder(strings.Repeat("_", width))
	}
	in.SetFocusFunc(func() { a.typingIn = true }).
		SetBlurFunc(func() { a.typingIn = false })
	return in
}

type underlinedField struct {
	tview.FormItem
}

func (u underlinedField) SetFormAttributes(labelWidth int, labelColor, bgColor, fieldTextColor, fieldBgColor tcell.Color) tview.FormItem {
	u.FormItem.SetFormAttributes(labelWidth, labelColor, bgColor, fieldTextColor, fieldBgColor)
	u.FormItem.(*tview.InputField).SetFieldStyle(
		tcell.StyleDefault.Foreground(fieldTextColor).Background(fieldBgColor).Underline(true))
	return u
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
	case 'y':
		a.copySelected()
		return nil
	}
	return ev
}

// valueLocalKeys: item operations + pagination on the value pane.
func (a *App) valueLocalKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEnter:
		if n := a.selectedTreeNode(); n != nil && n.IsBranch() {
			a.valueToggle() // ⏎ drills into branches; e edits them whole
			return nil
		}
		a.editValueItem()
		return nil
	case tcell.KeyLeft, tcell.KeyRight:
		a.valueToggle()
		return nil
	case tcell.KeyEscape:
		if p := a.valPage; p != nil && p.findTerm != "" {
			p.findTerm, p.findPos, p.findCount = "", 0, 0
			a.renderValue()
			return nil
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'e':
			a.editValueItem()
			return nil
		case 'f':
			a.openFind()
			return nil
		case ']':
			a.findNext(1)
			return nil
		case '[':
			a.findNext(-1)
			return nil
		case 'l':
			a.valueToggle()
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
		case 'g':
			a.openGraph()
			return nil
		case 'y':
			a.copySelected()
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
