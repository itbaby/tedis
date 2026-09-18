package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tedis/internal/cmdquery"
	"tedis/internal/cmdtable"
	"tedis/internal/conn"
	"tedis/internal/i18n"
)

// queryPage is the fullscreen command query view (Medis' query window).
// Layout (compact): editor TextArea on top, live colored preview line under
// it, results below. Ctrl+Enter executes the caret line (or the selected
// lines); alert mode routes write commands through the confirm dialog.
type queryPage struct {
	app     *App
	editor  *tview.TextArea
	preview *tview.TextView
	results *tview.TextView
	root    *tview.Flex
	history []string
	histIdx int
	cmds    *cmdtable.Table
	mirror  string // editor text mirror; GetText can transiently lag (tview quirk)
}

func newQueryPage(a *App) *queryPage {
	q := &queryPage{app: a}
	q.editor = tview.NewTextArea().
		SetPlaceholder("type commands…  ^R run  ^A alert  ^P/^N history  esc close")
	q.editor.SetBorder(true).
		SetTitle(" query ").
		SetTitleColor(a.th.Title).SetTitleAlign(tview.AlignLeft)
	q.editor.SetTextStyle(tcell.StyleDefault.Foreground(a.th.Text))
	q.editor.SetPlaceholderStyle(tcell.StyleDefault.Foreground(a.th.Dim))

	q.preview = tview.NewTextView().SetDynamicColors(true)

	q.results = tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	q.results.SetBorder(true).
		SetTitle(" results ").
		SetTitleColor(a.th.Title).SetTitleAlign(tview.AlignLeft)

	left := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(q.editor, 0, 2, true).
		AddItem(q.preview, 1, 0, false)
	q.root = tview.NewFlex().
		AddItem(left, 0, 3, true).
		AddItem(q.results, 0, 2, false)

	q.editor.SetChangedFunc(func() {
		if t := q.editor.GetText(); t != "" {
			q.mirror = t
		}
		q.updatePreview()
	})
	q.editor.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		return q.keys(ev)
	})
	q.loadHistory()
	return q
}

// open shows the query page, loading the server command table on first use.
func (a *App) openQuery() {
	c := a.rc.Load()
	if c == nil {
		a.flash("not connected", a.th.Error)
		return
	}
	if a.query == nil {
		a.query = newQueryPage(a)
		a.pages.AddPage("query", a.query.root, true, false)
	}
	if a.query.cmds == nil || a.query.cmds.ClassOf("get") == cmdtable.Unknown {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			tbl := c.LoadCmdTable(ctx)
			a.tapp.QueueUpdateDraw(func() {
				a.query.cmds = tbl
				a.query.updatePreview()
			})
		}()
	}
	a.query.setTitle()
	a.pages.ShowPage("query")
	a.tapp.SetFocus(a.query.editor)
}

func (a *App) closeQuery() {
	a.pages.HidePage("query")
	a.tapp.SetFocus(a.keys)
	a.applyFocusStyles()
}

func (q *queryPage) setTitle() {
	status := "off"
	if q.app.alert {
		status = "on"
	}
	q.editor.SetTitle(fmt.Sprintf(" query · alert:%s ", status))
}

func (q *queryPage) keys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEscape:
		q.app.closeQuery()
		return nil
	case tcell.KeyCtrlP:
		q.recallHistory(-1)
		return nil
	case tcell.KeyCtrlN:
		q.recallHistory(+1)
		return nil
	case tcell.KeyCtrlR:
		q.runSelection()
		return nil
	case tcell.KeyCtrlA:
		q.app.alert = !q.app.alert
		q.setTitle()
		q.app.setHints()
		return nil
	}
	return ev
}

// updatePreview renders the caret line's command with read/write coloring.
func (q *queryPage) updatePreview() {
	line := q.currentLine()
	cmds, err := cmdquery.Parse(line)
	th := q.app.th
	if err != nil {
		q.preview.SetText(fmt.Sprintf("[%s]%s", hex(th.Error), err.Error()))
		return
	}
	if len(cmds) == 0 || cmds[0].Name == "" {
		cls := ""
		if q.cmds != nil {
			cls = " · " + fmt.Sprint(len(q.cmds.Names())) + " cmds"
		}
		q.preview.SetText(fmt.Sprintf("[%s]%s", hex(th.Dim), cls))
		return
	}
	var b strings.Builder
	for _, cmd := range cmds {
		cls := q.cmds.ClassOf(cmd.Name)
		color := hex(th.Text)
		label := cls.String()
		switch cls {
		case cmdtable.Readonly:
			color = hex(th.Read)
		case cmdtable.Write:
			color = hex(th.Write)
		}
		b.WriteString(fmt.Sprintf("[%s]%s[%s] %s[%s]",
			color, strings.ToUpper(cmd.Name), hex(th.Text), renderArgs(cmd), hex(th.Dim)))
		b.WriteString(fmt.Sprintf("  ·%s·", label))
		b.WriteString("[:-:] ")
	}
	q.preview.SetText(b.String())
}

func renderArgs(cmd cmdquery.Command) string {
	parts := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		if a.Quoted {
			parts[i] = fmt.Sprintf("%q", a.Text)
		} else {
			parts[i] = a.Text
		}
	}
	return strings.Join(parts, " ")
}

// currentLine returns the editor line holding the caret.
func (q *queryPage) currentLine() string {
	_, _, row, _ := q.editor.GetCursor()
	return q.lineAt(row)
}

func (q *queryPage) text() string {
	if t := q.editor.GetText(); t != "" {
		return t
	}
	return q.mirror
}

func (q *queryPage) lineAt(row int) string {
	text := q.text()
	lines := strings.Split(text, "\n")
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

// selectedLines returns the line range of the selection, if any.
func (q *queryPage) selectedLines() (int, int, bool) {
	fromRow, fromCol, toRow, toCol := q.editor.GetCursor()
	if fromRow == toRow && fromCol == toCol {
		return 0, 0, false
	}
	if fromRow > toRow {
		fromRow, toRow = toRow, fromRow
	}
	return fromRow, toRow, true
}

// runSelection executes the caret line, or every line in the selection.
func (q *queryPage) runSelection() {
	a := q.app
	c := a.rc.Load()
	if c == nil {
		return
	}
	from, to, hasSel := q.selectedLines()
	var lines []int
	if hasSel {
		for i := from; i <= to; i++ {
			lines = append(lines, i)
		}
	} else {
		_, _, row, _ := q.editor.GetCursor()
		lines = []int{row}
	}
	var toRun []cmdquery.Command
	for _, ln := range lines {
		cmds, err := cmdquery.Parse(q.lineAt(ln))
		if err != nil {
			a.flash(fmt.Sprintf("line %d: %v", ln+1, err), a.th.Error)
			return
		}
		toRun = append(toRun, cmds...)
	}
	if len(toRun) == 0 {
		return
	}
	// alert mode: any write command needs explicit approval
	if a.alert {
		for _, cmd := range toRun {
			if q.cmds.ClassOf(cmd.Name) == cmdtable.Write {
				a.confirmModal(i18n.T("alert·write"),
					[]string{strings.ToUpper(cmd.Name) + " " + renderArgs(cmd)},
					"run", func() { q.exec(c, toRun) })
				return
			}
		}
	}
	q.exec(c, toRun)
}

func (q *queryPage) exec(c *conn.Conn, cmds []cmdquery.Command) {
	a := q.app
	q.appendHistory(cmds)
	go func() {
		var out strings.Builder
		for _, cmd := range cmds {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			r := c.Exec(ctx, cmd)
			cancel()
			out.WriteString(q.renderResult(r))
			out.WriteByte('\n')
		}
		a.tapp.QueueUpdateDraw(func() {
			fmt.Fprint(q.results, out.String())
			q.results.ScrollToEnd()
		})
	}()
}

func (q *queryPage) renderResult(r conn.ExecResult) string {
	th := q.app.th
	var b strings.Builder
	color := hex(th.Text)
	switch q.cmds.ClassOf(r.Cmd.Name) {
	case cmdtable.Readonly:
		color = hex(th.Read)
	case cmdtable.Write:
		color = hex(th.Write)
	}
	fmt.Fprintf(&b, "[%s]❯ %s %s[%s]\n", color, strings.ToUpper(r.Cmd.Name), renderArgs(r.Cmd), hex(th.Dim))
	if r.Err != nil {
		fmt.Fprintf(&b, "[%s](error) %s", hex(th.Error), tview.Escape(r.Text))
	} else {
		fmt.Fprintf(&b, "%s", tview.Escape(r.Text))
	}
	return b.String()
}

// ---- history -------------------------------------------------------------

// recallHistory steps through history (-1 = older, +1 = newer) and replaces
// the editor content.
func (q *queryPage) recallHistory(d int) {
	if len(q.history) == 0 {
		return
	}
	q.histIdx += d
	if q.histIdx < 0 {
		q.histIdx = 0
	}
	if q.histIdx >= len(q.history) {
		q.histIdx = len(q.history)
		q.editor.SetText("", true)
		return
	}
	q.editor.SetText(q.history[q.histIdx], false)
}

func (q *queryPage) appendHistory(cmds []cmdquery.Command) {
	for _, cmd := range cmds {
		line := strings.ToUpper(cmd.Name) + " " + renderArgs(cmd)
		q.history = append(q.history, strings.TrimSpace(line))
	}
	q.histIdx = len(q.history)
	q.saveHistory()
}

func (q *queryPage) historyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "tedis", "history")
}

func (q *queryPage) loadHistory() {
	p := q.historyPath()
	if p == "" {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			q.history = append(q.history, l)
		}
	}
	q.histIdx = len(q.history)
}

func (q *queryPage) saveHistory() {
	p := q.historyPath()
	if p == "" || len(q.history) == 0 {
		return
	}
	// keep the tail bounded
	const maxLines = 500
	if len(q.history) > maxLines {
		q.history = q.history[len(q.history)-maxLines:]
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(strings.Join(q.history, "\n")+"\n"), 0o644)
}
