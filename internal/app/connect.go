package app

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tedis/internal/config"
	"tedis/internal/conn"
)

// ConnectAsync dials in the background and updates the UI when done.
func (a *App) ConnectAsync(p *config.Profile) {
	a.flash("connecting to "+netAddr(p)+"…", a.th.Dim)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		c, err := conn.Connect(ctx, p)
		a.tapp.QueueUpdateDraw(func() {
			if err != nil {
				a.flash("connect failed: "+err.Error(), a.th.Error)
				return
			}
			if old := a.rc.Swap(c); old != nil {
				_ = old.Close()
			}
			a.onConnected(c)
		})
	}()
}

// onConnected switches the UI into connected state (phase 1: status + pane
// titles; phase 2 fills the panes with real data).
func (a *App) onConnected(c *conn.Conn) {
	p := c.P
	a.ns.SetTitle(" " + p.Name + " ")
	n, err := c.DBSize(context.Background())
	if err != nil {
		n = -1
	}
	lat, _ := c.Ping(context.Background())
	a.setStatusConnected(p, c, n, lat)
	a.startScan("*")
	a.tapp.SetFocus(a.keys)
	a.applyFocusStyles()
}

// openConnect shows the connection manager: profile list on the left,
// editor form on the right.
func (a *App) openConnect() {
	list := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	list.SetBorder(true).SetTitle(" profiles ").SetTitleColor(a.th.Title).
		SetTitleAlign(tview.AlignLeft)
	list.SetSelectedStyle(tcell.StyleDefault.Background(a.th.SelBg).Foreground(a.th.SelFg))

	form := tview.NewForm().SetButtonsAlign(tview.AlignCenter)
	form.SetItemPadding(0) // compact: no blank row between fields
	form.SetBorder(true).SetTitle(" profile ").SetTitleColor(a.th.Title).
		SetTitleAlign(tview.AlignLeft)
	form.SetFieldBackgroundColor(tcell.ColorDefault)
	form.SetLabelColor(a.th.Dim)

	reload := func() {
		list.Clear()
		list.SetCell(0, 0, tview.NewTableCell("name").SetTextColor(a.th.Dim).SetSelectable(false))
		list.SetCell(0, 1, tview.NewTableCell("address").SetTextColor(a.th.Dim).SetSelectable(false).SetExpansion(1))
		list.SetCell(0, 2, tview.NewTableCell("flags").SetTextColor(a.th.Dim).SetSelectable(false))
		row := 1
		for _, name := range slices.Sorted(maps.Keys(a.cfg.Profiles)) {
			p, _ := a.cfg.Get(name)
			flags := ""
			if p.Cluster {
				flags += "cluster "
			}
			if p.TLS.Enabled {
				flags += "tls "
			}
			if p.SSH != nil && p.SSH.Host != "" {
				flags += "ssh"
			}
			list.SetCell(row, 0, tview.NewTableCell(name).SetTextColor(a.th.Text))
			list.SetCell(row, 1, tview.NewTableCell(netAddr(p)).SetTextColor(a.th.Dim).SetExpansion(1))
			list.SetCell(row, 2, tview.NewTableCell(flags).SetTextColor(a.th.Dim))
			row++
		}
		if row > 1 {
			list.Select(1, 0)
		}
	}
	reload()

	// ---- form helpers -------------------------------------------------
	var fName, fHost, fPort, fUser, fPass, fDB string
	var fTLS, fInsecure, fCluster bool
	var fSSHHost, fSSHPort, fSSHUser, fSSHKey, fSSHPass string

	resetForm := func(p *config.Profile) {
		if p == nil {
			p = &config.Profile{Port: 6379}
		}
		fName, fHost, fPort = p.Name, p.Host, strconv.Itoa(p.Port)
		if fPort == "0" {
			fPort = "6379"
		}
		fUser, fPass = p.Username, p.Password
		fDB = strconv.Itoa(p.DB)
		fTLS, fInsecure, fCluster = p.TLS.Enabled, p.TLS.Insecure, p.Cluster
		fSSHHost, fSSHPort, fSSHUser, fSSHKey, fSSHPass = "", "", "", "", ""
		if p.SSH != nil {
			fSSHHost, fSSHUser, fSSHKey, fSSHPass = p.SSH.Host, p.SSH.User, p.SSH.KeyPath, p.SSH.Password
			if p.SSH.Port != 0 {
				fSSHPort = strconv.Itoa(p.SSH.Port)
			}
		}
	}

	buildForm := func() {
		form.Clear(false)
		a.addInput(form, "name", fName, 16, func(s string) { fName = s })
		a.addInput(form, "host", fHost, 20, func(s string) { fHost = s })
		a.addInput(form, "port", fPort, 6, func(s string) { fPort = s })
		a.addInput(form, "db", fDB, 3, func(s string) { fDB = s })
		a.addInput(form, "user", fUser, 12, func(s string) { fUser = s })
		a.addPassword(form, "password", fPass, 12, func(s string) { fPass = s })
		form.AddCheckbox("tls", fTLS, func(b bool) { fTLS = b })
		form.AddCheckbox("tls insecure", fInsecure, func(b bool) { fInsecure = b })
		form.AddCheckbox("cluster", fCluster, func(b bool) { fCluster = b })
		a.addInput(form, "ssh host", fSSHHost, 16, func(s string) { fSSHHost = s })
		a.addInput(form, "ssh port", fSSHPort, 5, func(s string) { fSSHPort = s })
		a.addInput(form, "ssh user", fSSHUser, 10, func(s string) { fSSHUser = s })
		a.addInput(form, "ssh key", fSSHKey, 14, func(s string) { fSSHKey = s })
		a.addPassword(form, "ssh pass", fSSHPass, 10, func(s string) { fSSHPass = s })
	}

	collect := func() (*config.Profile, error) {
		port, err := strconv.Atoi(fPort)
		if err != nil || port <= 0 || port > 65535 {
			return nil, errBad("port")
		}
		db := 0
		if fDB != "" {
			if db, err = strconv.Atoi(fDB); err != nil || db < 0 {
				return nil, errBad("db")
			}
		}
		if fHost == "" {
			return nil, errBad("host")
		}
		p := &config.Profile{
			Name: fName, Host: fHost, Port: port, DB: db,
			Username: fUser, Password: fPass,
			TLS:     config.TLS{Enabled: fTLS, Insecure: fInsecure},
			Cluster: fCluster,
		}
		if fSSHHost != "" {
			sp := 0
			if fSSHPort != "" {
				if sp, err = strconv.Atoi(fSSHPort); err != nil || sp <= 0 {
					return nil, errBad("ssh port")
				}
			}
			p.SSH = &config.SSH{Host: fSSHHost, Port: sp, User: fSSHUser, KeyPath: fSSHKey, Password: fSSHPass}
		}
		return p, nil
	}

	save := func() (*config.Profile, error) {
		p, err := collect()
		if err != nil {
			return nil, err
		}
		if p.Name == "" {
			return nil, errBad("name")
		}
		a.cfg.UpsertProfile(p)
		if err := config.Save(a.cfgPath, a.cfg); err != nil {
			return nil, err
		}
		reload()
		return p, nil
	}

	// actions live in a fixed button row below the form so they stay
	// visible no matter how tall the field list gets
	doConnect := func() {
		p, err := save()
		if err != nil {
			a.flash("profile: "+err.Error(), a.th.Error)
			return
		}
		a.closeModal("connect")
		a.ConnectAsync(p)
	}
	doSave := func() {
		if _, err := save(); err != nil {
			a.flash("profile: "+err.Error(), a.th.Error)
		} else {
			a.flash("saved "+fName, a.th.OK)
		}
	}
	doDelete := func() {
		if fName == "" {
			return
		}
		delete(a.cfg.Profiles, fName)
		if err := config.Save(a.cfgPath, a.cfg); err != nil {
			a.flash("delete: "+err.Error(), a.th.Error)
		}
		reload()
		resetForm(nil)
		buildForm()
	}

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyTab { // hop into the editor; it cycles on its own
			a.tapp.SetFocus(form)
			return nil
		}
		return ev
	})
	list.SetSelectionChangedFunc(func(row, _ int) {
		if row <= 0 {
			return
		}
		if name := list.GetCell(row, 0).Text; name != "" {
			if p, ok := a.cfg.Get(name); ok {
				resetForm(p)
				buildForm()
			}
		}
	})
	list.SetSelectedFunc(func(row, _ int) {
		if row <= 0 {
			return
		}
		if name := list.GetCell(row, 0).Text; name != "" {
			if p, ok := a.cfg.Get(name); ok {
				a.closeModal("connect")
				a.ConnectAsync(p)
			}
		}
	})
	resetForm(nil)
	buildForm()

	bar := buttonBar(a, []barBtn{
		{label: " connect ", fn: doConnect},
		{label: " save ", fn: doSave},
		{label: " delete ", fn: doDelete},
		{label: " close ", fn: func() { a.closeModal("connect") }},
	})
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewFlex().
			AddItem(list, 32, 1, true).
			AddItem(form, 44, 1, false), 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(bar, 1, 0, false)
	body.SetBorder(true).
		SetTitle(" connections · ⏎ connect · esc close ").
		SetTitleColor(a.th.Title).SetTitleAlign(tview.AlignLeft)
	body.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc {
			a.closeModal("connect")
			return nil
		}
		return ev
	})
	a.showModal("connect", body, 82, 22)
}

// barBtn is one entry of a one-line button bar.
type barBtn struct {
	label string
	fn    func()
}

// buttonBar lays out buttons on a single row (always visible, no scrolling).
func buttonBar(a *App, btns []barBtn) *tview.Flex {
	bar := tview.NewFlex()
	for _, b := range btns {
		btn := tview.NewButton(b.label).SetSelectedFunc(b.fn)
		btn.SetBackgroundColorActivated(a.th.SelBg)
		bar.AddItem(btn, len(b.label)+2, 1, false)
		bar.AddItem(nil, 2, 0, false)
	}
	return bar
}

type badField string

func (b badField) Error() string { return "invalid " + string(b) }

func errBad(f string) error { return badField(f) }
