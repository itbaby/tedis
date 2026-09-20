package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tedis/internal/conn"
	"tedis/internal/encode"
	"tedis/internal/i18n"
	"tedis/internal/jtree"
	"tedis/internal/keyview"
)

// ---- shared modals -------------------------------------------------------

// confirmModal shows a compact yes/no dialog. Focus starts on cancel
func (a *App) confirmModal(title string, body []string, confirmLabel string, fn func()) {
	var b strings.Builder
	for _, l := range body {
		b.WriteString("[" + hex(a.th.Text) + "]" + l + "\n")
	}
	tv := tview.NewTextView().SetDynamicColors(true).SetText(b.String())

	onOK := func() { a.closeModal("confirm"); fn() }
	onCancel := func() { a.closeModal("confirm") }
	cancel := tview.NewButton(" cancel ").SetSelectedFunc(onCancel)
	ok := tview.NewButton(" " + confirmLabel + " ").SetSelectedFunc(onOK)
	cancel.SetBackgroundColorActivated(a.th.SelBg)
	ok.SetBackgroundColorActivated(a.th.Error)
	buttons := tview.NewFlex().
		AddItem(cancel, 0, 1, true).
		AddItem(nil, 2, 0, false).
		AddItem(ok, 0, 1, false)

	f := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tv, 0, 1, false).
		AddItem(nil, 1, 0, false).
		AddItem(buttons, 1, 0, true)
	f.SetBorder(true).SetTitle(" " + title + " ").SetTitleColor(a.th.Warn)
	f.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			onCancel()
			return nil
		case tcell.KeyTab, tcell.KeyLeft, tcell.KeyRight, tcell.KeyBacktab:
			if a.tapp.GetFocus() == ok {
				a.tapp.SetFocus(cancel)
			} else {
				a.tapp.SetFocus(ok)
			}
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'y', 'Y':
				onOK()
				return nil
			case 'n', 'N':
				onCancel()
				return nil
			}
		}
		return ev
	})
	a.showModal("confirm", f, 56, 8+len(body))
}

// promptModal asks for one line of input.
func (a *App) promptModal(title, label, initial string, fn func(string)) {
	form := tview.NewForm().SetButtonsAlign(tview.AlignCenter).SetFieldBackgroundColor(tcell.ColorDefault)
	form.SetLabelColor(a.th.Dim)
	input := initial
	form.AddInputField(label, initial, 40, nil, func(s string) { input = s })
	ok := func() {
		a.closeModal("prompt")
		fn(input)
	}
	if item := form.GetFormItem(0); item != nil {
		if in, isField := item.(*tview.InputField); isField {
			in.SetDoneFunc(func(key tcell.Key) {
				if key == tcell.KeyEnter {
					ok()
				}
			})
		}
	}
	form.AddButton("ok", ok)
	form.AddButton("cancel", func() { a.closeModal("prompt") })
	form.SetCancelFunc(func() { a.closeModal("prompt") })
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEnter:
			ok()
			return nil
		case tcell.KeyEsc:
			a.closeModal("prompt")
			return nil
		}
		return ev
	})
	form.SetBorder(true).SetTitle(" " + title + " ").SetTitleColor(a.th.Title)
	a.showModal("prompt", form, 56, 9)
}

// editorModal edits one or more labeled fields (key/value, score, …) in a
// standard form layout: aligned labels, tab/⏎ walks fields and buttons,
// ⌃s saves from anywhere. Multi-line fields get a text area sized to their
// content. onSave receives the field values in order.
func (a *App) editorModal(title string, fields []editField, onSave func(vals []string)) {
	_, _, tw, th := a.pages.GetRect() // root page spans the screen and is laid out
	// Width follows the data: label column + longest content line, never
	// narrower than the breadcrumb title, capped by the screen.
	labelW, maxLine := 0, 0
	for _, f := range fields {
		if w := tview.TaggedStringWidth(f.label); w > labelW {
			labelW = w
		}
		for _, ln := range strings.Split(f.initial, "\n") {
			if w := tview.TaggedStringWidth(ln); w > maxLine {
				maxLine = w
			}
		}
	}
	mw := labelW + maxLine + 8 // label, spacing, content, padding + border
	if t := tview.TaggedStringWidth(title) + 6; t > mw {
		mw = t
	}
	if mw > tw-10 {
		mw = tw - 10
	}
	if mw < 34 {
		mw = 34
	}
	maxTA := th - 16
	if maxTA > 20 {
		maxTA = 20
	}
	if maxTA < 4 {
		maxTA = 4
	}

	form := tview.NewForm().
		SetItemPadding(1).
		SetLabelColor(a.th.Dim).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(a.th.Text).
		SetButtonsAlign(tview.AlignCenter)
	form.SetBackgroundColor(tcell.ColorDefault)
	form.SetBorder(true).SetTitle(" " + title + " ").SetTitleColor(a.th.Title)
	height := 4 // border + inner padding
	content := 0
	for i, f := range fields {
		if f.multiline {
			lines := strings.Count(f.initial, "\n") + 1
			if lines < 3 {
				lines = 3
			}
			if lines > maxTA {
				lines = maxTA
			}
			form.AddTextArea(f.label, "", 0, lines, 0, nil)
			ta := form.GetFormItem(i).(*tview.TextArea)
			ta.SetText(f.initial, false) // cursor at head, like a JSON viewer
			ta.SetWrap(true)
			ta.SetPlaceholder("(empty)")
			content += lines
			continue
		}
		form.AddInputField(f.label, f.initial, 0, nil, nil)
		content++
	}
	height += content + len(fields) // items + padding between them
	height += 2                     // blank row + button row
	if len(fields) == 1 {
		height-- // no padding between a single item
	}

	save := func() {
		vals := make([]string, len(fields))
		for i := range fields {
			vals[i] = formText(form.GetFormItem(i))
		}
		a.closeModal("editor")
		onSave(vals)
	}
	form.AddButton("save", save)
	form.AddButton("cancel", func() { a.closeModal("editor") })
	form.SetCancelFunc(func() { a.closeModal("editor") })
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlS {
			save()
			return nil
		}
		return ev
	})

	hint := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	hint.SetBackgroundColor(tcell.ColorDefault)
	hint.SetText(fmt.Sprintf("[%s]⌃s[-] save  ·  [%s]tab[-] next  ·  [%s]esc[-] cancel",
		hex(a.th.Dim), hex(a.th.Dim), hex(a.th.Dim)))
	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(hint, 1, 0, false)
	if height > th-4 {
		height = th - 4
	}
	a.showModal("editor", root, mw, height)
}

// formText reads the current text of a form item (input or text area).
func formText(item tview.FormItem) string {
	switch f := item.(type) {
	case *tview.InputField:
		return f.GetText()
	case *tview.TextArea:
		return f.GetText()
	}
	return ""
}

type editField struct {
	label, initial string
	multiline      bool
}

// ---- key operations (keys pane) ------------------------------------------

// deleteKey removes a key after confirmation (per-profile toggle, Medis
// "Delete Confirmation Dialog").
func (a *App) deleteKey(key string) {
	c := a.rc.Load()
	if c == nil {
		return
	}
	skip := c.P.SkipDeleteConfirm
	do := func() {
		a.runAsync("del: ", func(ctx context.Context) error {
			return c.Client.Del(ctx, key).Err()
		}, func() {
			a.dropKeyLocal(key)
			a.flash("deleted "+key, a.th.OK)
		})
	}
	if skip {
		do()
		return
	}
	a.confirmModal(i18n.T("delete key"), []string{key}, "delete", do)
}

// dropKeyLocal updates scan state after the key is gone on the server.
func (a *App) dropKeyLocal(key string) {
	s := a.scan
	if s == nil {
		return
	}
	s.tree.Remove(key)
	delete(s.seen, key)
	delete(s.meta, key)
	if i := slices.Index(s.keys, key); i >= 0 {
		s.keys = slices.Delete(s.keys, i, i+1)
	}
	a.keys.SetTitle(fmt.Sprintf(" keys · %s · %d ", s.pattern, len(s.keys)))
	a.renderTree()
	a.renderKeyList()
	if a.valPage != nil && a.valPage.key == key {
		a.valPage = nil
		a.value.Clear()
		a.value.SetTitle(" value ")
	}
}

// editKeyTTL opens the TTL prompt (seconds; empty = persist).
func (a *App) editKeyTTL(key string) {
	a.promptModal("ttl · "+key, "seconds (empty=persist)", "", func(v string) {
		c := a.rc.Load()
		if c == nil {
			return
		}
		if v == "" {
			a.runAsync("ttl: ", func(ctx context.Context) error {
				return c.Client.Persist(ctx, key).Err()
			}, func() {
				a.flash("ttl removed "+key, a.th.OK)
				a.reloadMeta(key)
			})
			return
		}
		secs, e := strconv.Atoi(v)
		if e != nil || secs <= 0 {
			a.flash("ttl: want positive seconds", a.th.Error)
			return
		}
		a.runAsync("ttl: ", func(ctx context.Context) error {
			return c.Client.Expire(ctx, key, time.Duration(secs)*time.Second).Err()
		}, func() {
			a.flash("ttl set "+key+" → "+v+"s", a.th.OK)
			a.reloadMeta(key)
		})
	})
}

// renameKey opens the rename prompt and updates local state on success.
func (a *App) renameKey(key string) {
	a.promptModal("rename · "+key, "new name (was "+key+")", "", func(name string) {
		c := a.rc.Load()
		if c == nil || name == "" || name == key {
			return
		}
		a.runAsync("rename: ", func(ctx context.Context) error {
			return c.Client.Rename(ctx, key, name).Err()
		}, func() {
			s := a.scan
			if s != nil {
				s.tree.Remove(key)
				s.tree.Add(name)
				delete(s.seen, key)
				delete(s.meta, key)
				if i := slices.Index(s.keys, key); i >= 0 {
					s.keys[i] = name
				}
				a.renderTree()
				a.renderKeyList()
			}
			a.loadMeta(name, -1)
			a.flash("renamed → "+name, a.th.OK)
		})
	})
}

// selectedKey returns the key under the key-list cursor ("" if none).
func (a *App) selectedKey() string {
	s := a.scan
	if s == nil {
		return ""
	}
	row, _ := a.keys.GetSelection()
	idx := a.listOffset + row - 1
	if row <= 0 || idx < 0 || idx >= len(s.keys) {
		return ""
	}
	return s.keys[idx]
}

// ---- value item operations (value pane) ----------------------------------

func (a *App) refreshValueAndMeta() {
	if a.valPage == nil {
		return
	}
	a.fetchValuePage()
	a.reloadMeta(a.valPage.key)
}

// editValueItem edits the selected row according to the key type.
func (a *App) editValueItem() {
	p := a.valPage
	if p == nil {
		return
	}
	item, _, ok := a.valueSelectedRow()
	if !ok {
		return
	}
	c := a.rc.Load()
	if c == nil {
		return
	}
	switch p.kind {
	case kindString, kindJSON:
		if p.tree != nil {
			if n := a.selectedTreeNode(); n != nil && n != p.tree {
				a.editJSONNode(n)
				return
			}
		}
		codec := a.valueCodec()
		text, used, err := encode.Format([]byte(p.raw), codec)
		if err != nil {
			a.flash("decode: "+err.Error(), a.th.Error)
			return
		}
		a.editorModal("edit string · "+p.key+" · "+used.Name(), []editField{{"value", text, true}}, func(vals []string) {
			raw := []byte(vals[0])
			if enc := used; enc != nil {
				if b, err := enc.Encode(vals[0]); err == nil {
					raw = b
				} else {
					a.flash("encode: "+err.Error()+" (saving as text)", a.th.Warn)
				}
			}
			if p.kind == kindJSON {
				a.runMutation(func(ctx context.Context) error {
					return keyview.SaveJSON(ctx, c.Client, p.key, string(raw))
				})
				return
			}
			a.saveStringKeepTTL(p.key, string(raw))
		})
	case kindHash:
		if a.editNestedNode() {
			return
		}
		text, enc := decodeField(item[1])
		a.editorModal("edit field · "+item[0], []editField{{"field", item[0], false}, {"value", text, true}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				if vals[0] != item[0] {
					if err := keyview.DelHashField(ctx, c.Client, p.key, item[0]); err != nil {
						return err
					}
				}
				return keyview.SaveHashField(ctx, c.Client, p.key, vals[0], enc(vals[1]))
			})
		})
	case kindList:
		if a.editNestedNode() {
			return
		}
		index, _ := strconv.ParseInt(item[0], 10, 64)
		text, enc := decodeField(item[1])
		a.editorModal("edit item", []editField{{"value", text, true}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				return keyview.SaveListItem(ctx, c.Client, p.key, index, enc(vals[0]))
			})
		})
	case kindSet:
		if a.editNestedNode() {
			return
		}
		text, enc := decodeField(item[0])
		a.editorModal("edit member", []editField{{"member", text, false}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				if vals[0] == text {
					return nil
				}
				if err := keyview.DelSetMember(ctx, c.Client, p.key, item[0]); err != nil {
					return err
				}
				return keyview.SaveSetMember(ctx, c.Client, p.key, enc(vals[0]))
			})
		})
	case kindZSet:
		text, enc := decodeField(item[0])
		a.editorModal("edit member", []editField{{"member", text, false}, {"score", item[1], false}}, func(vals []string) {
			score, err := strconv.ParseFloat(vals[1], 64)
			a.runMutation(func(ctx context.Context) error {
				if err != nil {
					return err
				}
				if vals[0] != text {
					if e := keyview.DelZSetMember(ctx, c.Client, p.key, item[0]); e != nil {
						return e
					}
				}
				return keyview.SaveZSetMember(ctx, c.Client, p.key, enc(vals[0]), score)
			})
		})
	case kindStream:
		a.flash("stream entries are immutable (d to delete)", a.th.Dim)
	}
}

// editJSONNode edits just the selected subtree of a decoded JSON payload
// (string/ReJSON keys): the node's own value opens in the editor and is
// merged back into the full document on save.
func (a *App) editJSONNode(node *jtree.Node) {
	p := a.valPage
	c := a.rc.Load()
	if c == nil || p.tree == nil {
		return
	}
	initial := node.Value
	if node.IsBranch() {
		initial = jtree.Marshal(node)
	}
	title := strings.Join(jtree.Path(p.tree, node), " › ")
	// Object members can be renamed; array elements/roots cannot.
	parent := jtree.FindParent(p.tree, node)
	rename := parent != nil && parent.Kind == jtree.KindObject
	fields := []editField{{"value", initial, true}}
	if rename {
		fields = []editField{{"field", node.Label, false}, {"value", initial, true}}
	}
	a.editorModal("edit · "+title, fields, func(vals []string) {
		name, text := node.Label, vals[0]
		if rename {
			name, text = vals[0], vals[1]
			if strings.TrimSpace(name) == "" {
				a.flash("edit: field name must not be empty", a.th.Error)
				return
			}
			if name != node.Label {
				for _, sib := range parent.Children {
					if sib != node && sib.Label == name {
						a.flash("edit: duplicate field "+name, a.th.Error)
						return
					}
				}
			}
		}
		var repl *jtree.Node
		if node.IsBranch() {
			if repl = jtree.FromJSON(text); repl == nil {
				a.flash("edit: invalid JSON for "+title, a.th.Error)
				return
			}
		} else {
			var err error
			if repl, err = scalarFromEdit(node.Kind, text); err != nil {
				a.flash("edit · "+title+": "+err.Error(), a.th.Error)
				return
			}
		}
		repl = repl.WithItem(node.ItemIdx)
		if !p.tree.Replace(node, repl) {
			a.flash("edit: row moved, retry", a.th.Warn)
			return
		}
		repl.Label = name
		if p.kind != kindString && p.kind != kindJSON {
			a.saveMemberDoc(p, c, repl)
			return
		}
		merged := jtree.Marshal(p.tree)
		if p.kind == kindJSON {
			a.runMutation(func(ctx context.Context) error {
				return keyview.SaveJSON(ctx, c.Client, p.key, merged)
			})
			return
		}
		raw := merged
		if _, used, err := encode.Format([]byte(p.raw), a.valueCodec()); err == nil && used != nil {
			if b, e := used.Encode(merged); e == nil {
				raw = string(b)
			} else {
				a.flash("encode: "+e.Error()+" (saving as text)", a.th.Warn)
			}
		}
		a.saveStringKeepTTL(p.key, raw)
	})
}

// editNestedNode reports whether the cursor sits inside a JSON document
// nested in a container member (hash field, list item, set member); if so
// it opens the row-scoped editor for just that node and returns true.
func (a *App) editNestedNode() bool {
	p := a.valPage
	if p.tree == nil {
		return false
	}
	n := a.selectedTreeNode()
	if n == nil || n == p.tree || jtree.FindParent(p.tree, n) == p.tree {
		return false // root or whole-member row: the member editor handles it
	}
	a.editJSONNode(n)
	return true
}

// saveMemberDoc merges an edited node back into the container member
// holding it: climb to the member's own subtree, marshal it whole, re-encode
// through that member's codec, and save just that one member.
func (a *App) saveMemberDoc(p *valuePage, c *conn.Conn, n *jtree.Node) {
	for {
		up := jtree.FindParent(p.tree, n)
		if up == nil || up == p.tree {
			break
		}
		n = up
	}
	idx := n.ItemIdx
	if idx < 0 || idx >= len(p.rows) {
		a.flash("edit: row moved, retry", a.th.Warn)
		return
	}
	row := p.rows[idx]
	switch p.kind {
	case kindHash:
		doc := jtree.Marshal(n)
		_, enc := decodeField(row[1])
		a.runMutation(func(ctx context.Context) error {
			return keyview.SaveHashField(ctx, c.Client, p.key, row[0], enc(doc))
		})
	case kindList:
		index, err := strconv.ParseInt(row[0], 10, 64)
		if err != nil {
			a.flash("edit: bad list index", a.th.Error)
			return
		}
		doc := jtree.Marshal(n)
		_, enc := decodeField(row[1])
		a.runMutation(func(ctx context.Context) error {
			return keyview.SaveListItem(ctx, c.Client, p.key, index, enc(doc))
		})
	case kindSet:
		doc := jtree.Marshal(n)
		_, enc := decodeField(row[0])
		newRaw := enc(doc)
		a.runMutation(func(ctx context.Context) error {
			if err := keyview.DelSetMember(ctx, c.Client, p.key, row[0]); err != nil {
				return err
			}
			return keyview.SaveSetMember(ctx, c.Client, p.key, newRaw)
		})
	}
}

// scalarFromEdit rebuilds a leaf from editor text, keeping the original
// JSON type: strings are edited unquoted, numbers/bools validated as-is.
func scalarFromEdit(k jtree.Kind, text string) (*jtree.Node, error) {
	switch k {
	case jtree.KindString, jtree.KindText:
		return jtree.Leaf("", text, jtree.KindString), nil
	case jtree.KindNumber:
		if !json.Valid([]byte(text)) {
			return nil, fmt.Errorf("want a number")
		}
		return jtree.Leaf("", text, jtree.KindNumber), nil
	case jtree.KindBool:
		if text != "true" && text != "false" {
			return nil, fmt.Errorf("want true or false")
		}
		return jtree.Leaf("", text, jtree.KindBool), nil
	}
	if n := jtree.FromJSON(text); n != nil {
		return n, nil
	}
	return nil, fmt.Errorf("want a JSON value")
}

// decodeField decodes a container field for editing and returns an encoder
// that writes edits back through the same codec (raw passthrough when the
// codec cannot encode). Keeps binary fields lossless through the editor.
func decodeField(raw string) (string, func(string) string) {
	text, used, err := encode.Format([]byte(raw), nil)
	if err != nil || used == nil {
		return raw, func(s string) string { return s }
	}
	return text, func(s string) string {
		if b, err := used.Encode(s); err == nil {
			return string(b)
		}
		return s
	}
}

// newValueItem adds an item (n key on the value pane).
func (a *App) newValueItem() {
	p := a.valPage
	if p == nil {
		return
	}
	c := a.rc.Load()
	if c == nil {
		return
	}
	switch p.kind {
	case kindString:
		a.flash("strings are edited with e", a.th.Dim)
	case kindHash:
		a.editorModal("add field · "+p.key, []editField{{"field", "", false}, {"value", "", true}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				return keyview.SaveHashField(ctx, c.Client, p.key, vals[0], vals[1])
			})
		})
	case kindList:
		a.editorModal("push item · "+p.key, []editField{{"value", "", true}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				return keyview.AddListItem(ctx, c.Client, p.key, vals[0], false)
			})
		})
	case kindSet:
		a.editorModal("add member · "+p.key, []editField{{"member", "", false}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				return keyview.SaveSetMember(ctx, c.Client, p.key, vals[0])
			})
		})
	case kindZSet:
		a.editorModal("add member · "+p.key, []editField{{"member", "", false}, {"score", "0", false}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				score, err := strconv.ParseFloat(vals[1], 64)
				if err != nil {
					return err
				}
				return keyview.SaveZSetMember(ctx, c.Client, p.key, vals[0], score)
			})
		})
	case kindStream:
		a.editorModal("append entry · "+p.key, []editField{{"field", "", false}, {"value", "", true}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				return keyview.AddStreamEntry(ctx, c.Client, p.key, vals[0], vals[1])
			})
		})
	}
}

// delValueItem deletes the selected row (with confirmation).
func (a *App) delValueItem() {
	p := a.valPage
	if p == nil {
		return
	}
	c := a.rc.Load()
	if c == nil {
		return
	}
	if isTextKind(p.kind) {
		a.deleteKey(p.key) // deleting the body == deleting the key
		return
	}
	item, _, ok := a.valueSelectedRow()
	if !ok {
		return
	}
	run := func() {
		a.runMutation(func(ctx context.Context) error {
			switch p.kind {
			case kindHash:
				return keyview.DelHashField(ctx, c.Client, p.key, item[0])
			case kindSet:
				return keyview.DelSetMember(ctx, c.Client, p.key, item[0])
			case kindZSet:
				return keyview.DelZSetMember(ctx, c.Client, p.key, item[0])
			case kindList:
				return keyview.DelListItem(ctx, c.Client, p.key, item[1])
			case kindStream:
				return keyview.DelStreamEntry(ctx, c.Client, p.key, item[0])
			}
			return nil
		})
	}
	if c.P.SkipDeleteConfirm {
		run()
		return
	}
	a.confirmModal(i18n.T("delete item"), []string{p.key + " · " + item[0]}, "delete", run)
}

// saveStringKeepTTL writes a string and re-applies its previous TTL (SET
// clears TTL — see keyview package doc).
func (a *App) saveStringKeepTTL(key, val string) {
	c := a.rc.Load()
	if c == nil {
		return
	}
	a.runMutation(func(ctx context.Context) error {
		ttl := time.Duration(-1)
		if m, ok := a.scan.meta[key]; ok {
			ttl = m.TTL
		}
		if err := keyview.SaveString(ctx, c.Client, key, val); err != nil {
			return err
		}
		if ttl > 0 {
			return c.Client.Expire(ctx, key, ttl).Err()
		}
		return nil
	})
}

// runAsync executes a Redis write off the UI thread and reports the result
// back on it: on error it flashes label+err, on success it calls onOK. The
// three key-level mutations (delete/ttl/rename) share this shape.
func (a *App) runAsync(label string, fn func(ctx context.Context) error, onOK func()) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := fn(ctx)
		a.tapp.QueueUpdateDraw(func() {
			if err != nil {
				a.flash(label+err.Error(), a.th.Error)
				return
			}
			onOK()
		})
	}()
}

// reloadMeta drops the cached size/TTL for key and refetches it.
func (a *App) reloadMeta(key string) {
	if a.scan != nil {
		delete(a.scan.meta, key) // size/TTL changed: force refetch
	}
	a.loadMeta(key, -1)
}

// runMutation executes a write in the background, then refreshes value+meta.
func (a *App) runMutation(fn func(ctx context.Context) error) {
	a.runAsync("", fn, a.refreshValueAndMeta)
}
