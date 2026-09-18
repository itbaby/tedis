package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

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

	cancel := tview.NewButton(" cancel ").SetSelectedFunc(func() { a.closeModal("confirm") })
	ok := tview.NewButton(" " + confirmLabel + " ").SetSelectedFunc(func() {
		a.closeModal("confirm")
		fn()
	})
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
	onOK := func() { a.closeModal("confirm"); fn() }
	onCancel := func() { a.closeModal("confirm") }
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

// editorModal edits multi-line text with an optional extra numeric field
// (e.g. zset score). onSave receives the field values.
func (a *App) editorModal(title string, fields []editField, onSave func(vals []string)) {
	form := tview.NewForm().SetButtonsAlign(tview.AlignCenter).SetFieldBackgroundColor(tcell.ColorDefault)
	form.SetLabelColor(a.th.Dim)
	vals := make([]string, len(fields))
	for i, f := range fields {
		i, f := i, f
		vals[i] = f.initial // changed callbacks fire on edits, not on init
		if f.multiline {
			form.AddTextArea(f.label, f.initial, 48, 6, 0, func(s string) { vals[i] = s })
		} else {
			form.AddInputField(f.label, f.initial, 40, nil, func(s string) { vals[i] = s })
		}
	}
	save := func() {
		a.closeModal("editor")
		onSave(vals)
	}
	form.AddButton("save", save)
	form.AddButton("cancel", func() { a.closeModal("editor") })
	form.SetCancelFunc(func() { a.closeModal("editor") })
	// Inside TextArea, Tab/Enter are text keys — offer explicit save/abort.
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyCtrlS:
			save()
			return nil
		case tcell.KeyEscape:
			a.closeModal("editor")
			return nil
		}
		return ev
	})
	form.SetBorder(true).SetTitle(" " + title + " ").SetTitleColor(a.th.Title)
	// focus the value field first: edits are far more common than renames
	for i, f := range fields {
		if f.multiline {
			form.SetFocus(i)
			break
		}
	}
	// TextAreas swallow keys, so ctrl+s/esc must be bound on each of them
	for i := 0; i < form.GetFormItemCount(); i++ {
		ta, ok := form.GetFormItem(i).(*tview.TextArea)
		if !ok {
			continue
		}
		ta.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			switch ev.Key() {
			case tcell.KeyCtrlS:
				save()
				return nil
			case tcell.KeyEscape:
				a.closeModal("editor")
				return nil
			}
			return ev
		})
	}
	a.showModal("editor", form, 58, 12+len(fields))
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
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := c.Client.Del(ctx, key).Err()
			a.tapp.QueueUpdateDraw(func() {
				if err != nil {
					a.flash("del: "+err.Error(), a.th.Error)
					return
				}
				a.dropKeyLocal(key)
				a.flash("deleted "+key, a.th.OK)
			})
		}()
	}
	if skip {
		do()
		return
	}
	a.confirmModal("delete key", []string{key}, "delete", do)
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
	for i, k := range s.keys {
		if k == key {
			s.keys = append(s.keys[:i], s.keys[i+1:]...)
			break
		}
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
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var err error
			var msg string
			if v == "" {
				err = c.Client.Persist(ctx, key).Err()
				msg = "ttl removed " + key
			} else {
				secs, e := strconv.Atoi(v)
				if e != nil || secs <= 0 {
					a.tapp.QueueUpdateDraw(func() { a.flash("ttl: want positive seconds", a.th.Error) })
					return
				}
				err = c.Client.Expire(ctx, key, time.Duration(secs)*time.Second).Err()
				msg = "ttl set " + key + " → " + v + "s"
			}
			a.tapp.QueueUpdateDraw(func() {
				if err != nil {
					a.flash("ttl: "+err.Error(), a.th.Error)
					return
				}
				a.flash(msg, a.th.OK)
				if m, ok := a.scan.meta[key]; ok {
					m.TTL = -1
					a.scan.meta[key] = m
				}
				a.loadMeta(key, -1)
			})
		}()
	})
}

// renameKey opens the rename prompt and updates local state on success.
func (a *App) renameKey(key string) {
	a.promptModal("rename · "+key, "new name (was "+key+")", "", func(name string) {
		c := a.rc.Load()
		if c == nil || name == "" || name == key {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := c.Client.Rename(ctx, key, name).Err()
			a.tapp.QueueUpdateDraw(func() {
				if err != nil {
					a.flash("rename: "+err.Error(), a.th.Error)
					return
				}
				s := a.scan
				if s != nil {
					s.tree.Remove(key)
					s.tree.Add(name)
					delete(s.seen, key)
					delete(s.meta, key)
					for i, k := range s.keys {
						if k == key {
							s.keys[i] = name
							break
						}
					}
					a.renderTree()
					a.renderKeyList()
				}
				a.loadMeta(name, -1)
				a.flash("renamed → "+name, a.th.OK)
			})
		}()
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

func (a *App) valueSelectedRow() ([2]string, int, bool) {
	if a.valPage == nil {
		return [2]string{}, 0, false
	}
	row, _ := a.value.GetSelection()
	if row <= 0 || row > len(a.valPage.rows) {
		return [2]string{}, 0, false
	}
	return a.valPage.rows[row-1], row - 1, true
}

func (a *App) refreshValueAndMeta() {
	if a.valPage == nil {
		return
	}
	a.fetchValuePage()
	a.loadMeta(a.valPage.key, -1)
}

// editValueItem edits the selected row according to the key type.
func (a *App) editValueItem() {
	p := a.valPage
	if p == nil {
		return
	}
	item, idx, ok := a.valueSelectedRow()
	if !ok {
		return
	}
	c := a.rc.Load()
	if c == nil {
		return
	}
	switch p.kind {
	case "string":
		v, err := keyview.LoadString(context.Background(), c.Client, p.key)
		if err != nil {
			a.flash(err.Error(), a.th.Error)
			return
		}
		a.editorModal("edit string · "+p.key, []editField{{"value", v, true}}, func(vals []string) {
			a.saveStringKeepTTL(p.key, vals[0])
		})
	case "hash":
		a.editorModal("edit field · "+item[0], []editField{{"field", item[0], false}, {"value", item[1], true}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				if vals[0] != item[0] {
					if err := keyview.DelHashField(ctx, c.Client, p.key, item[0]); err != nil {
						return err
					}
				}
				return keyview.SaveHashField(ctx, c.Client, p.key, vals[0], vals[1])
			})
		})
	case "list":
		index, _ := strconv.ParseInt(item[0], 10, 64)
		a.editorModal("edit item", []editField{{"value", item[1], true}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				return keyview.SaveListItem(ctx, c.Client, p.key, index, vals[0])
			})
		})
	case "set":
		a.editorModal("edit member", []editField{{"member", item[0], false}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				if vals[0] == item[0] {
					return nil
				}
				if err := keyview.DelSetMember(ctx, c.Client, p.key, item[0]); err != nil {
					return err
				}
				return keyview.SaveSetMember(ctx, c.Client, p.key, vals[0])
			})
		})
	case "zset":
		a.editorModal("edit member", []editField{{"member", item[0], false}, {"score", item[1], false}}, func(vals []string) {
			score, err := strconv.ParseFloat(vals[1], 64)
			a.runMutation(func(ctx context.Context) error {
				if err != nil {
					return err
				}
				if vals[0] != item[0] {
					if e := keyview.DelZSetMember(ctx, c.Client, p.key, item[0]); e != nil {
						return e
					}
				}
				return keyview.SaveZSetMember(ctx, c.Client, p.key, vals[0], score)
			})
		})
	case "stream":
		a.flash("stream entries are immutable (d to delete)", a.th.Dim)
	}
	_ = idx
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
	case "string":
		a.flash("strings are edited with e", a.th.Dim)
	case "hash":
		a.editorModal("add field · "+p.key, []editField{{"field", "", false}, {"value", "", true}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				return keyview.SaveHashField(ctx, c.Client, p.key, vals[0], vals[1])
			})
		})
	case "list":
		a.editorModal("push item · "+p.key, []editField{{"value", "", true}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				return keyview.AddListItem(ctx, c.Client, p.key, vals[0], false)
			})
		})
	case "set":
		a.editorModal("add member · "+p.key, []editField{{"member", "", false}}, func(vals []string) {
			a.runMutation(func(ctx context.Context) error {
				return keyview.SaveSetMember(ctx, c.Client, p.key, vals[0])
			})
		})
	case "zset":
		a.editorModal("add member · "+p.key, []editField{{"member", "", false}, {"score", "0", false}}, func(vals []string) {
			score, _ := strconv.ParseFloat(vals[1], 64)
			a.runMutation(func(ctx context.Context) error {
				return keyview.SaveZSetMember(ctx, c.Client, p.key, vals[0], score)
			})
		})
	case "stream":
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
	item, _, ok := a.valueSelectedRow()
	if !ok {
		return
	}
	if p.kind == "string" {
		a.deleteKey(p.key) // deleting the body == deleting the key
		return
	}
	run := func() {
		a.runMutation(func(ctx context.Context) error {
			switch p.kind {
			case "hash":
				return keyview.DelHashField(ctx, c.Client, p.key, item[0])
			case "set":
				return keyview.DelSetMember(ctx, c.Client, p.key, item[0])
			case "zset":
				return keyview.DelZSetMember(ctx, c.Client, p.key, item[0])
			case "list":
				return keyview.DelListItem(ctx, c.Client, p.key, item[1])
			case "stream":
				return keyview.DelStreamEntry(ctx, c.Client, p.key, item[0])
			}
			return nil
		})
	}
	if c.P.SkipDeleteConfirm {
		run()
		return
	}
	a.confirmModal("delete item", []string{p.key + " · " + item[0]}, "delete", run)
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

// runMutation executes a write in the background, then refreshes value+meta.
func (a *App) runMutation(fn func(ctx context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := fn(ctx)
		a.tapp.QueueUpdateDraw(func() {
			if err != nil {
				a.flash(err.Error(), a.th.Error)
				return
			}
			a.refreshValueAndMeta()
		})
	}()
}
