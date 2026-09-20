package app

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// openInfo shows a server INFO page: version/memory/clients/stats/keyspace
// in a compact table. 0-9 switch databases when standalone.
func (a *App) openInfo() {
	c := a.rc.Load()
	if c == nil {
		a.flash("not connected", a.th.Error)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := c.Client.Info(ctx).Result()
		a.tapp.QueueUpdateDraw(func() {
			if err != nil {
				a.flash("info: "+err.Error(), a.th.Error)
				return
			}
			a.showInfo(raw)
		})
	}()
}

func (a *App) showInfo(raw string) {
	tv := tview.NewTable().SetSelectable(false, false)
	tv.SetBorder(true).SetTitle(" info ").SetTitleColor(a.th.Title).SetTitleAlign(tview.AlignLeft)

	sections := parseInfo(raw)
	order := []string{"Server", "Memory", "Clients", "Stats", "Keyspace"}
	row := 0
	seen := map[string]bool{}
	for _, sec := range order {
		kv, ok := sections[sec]
		if !ok {
			continue
		}
		seen[sec] = true
		tv.SetCell(row, 0, tview.NewTableCell(strings.ToLower(sec)).
			SetTextColor(a.th.Title).SetSelectable(false))
		row++
		for _, k := range slices.Sorted(maps.Keys(kv)) {
			tv.SetCell(row, 0, tview.NewTableCell(" "+k).SetTextColor(a.th.Dim).SetSelectable(false))
			tv.SetCell(row, 1, tview.NewTableCell(kv[k]).SetTextColor(a.th.Text).
				SetSelectable(false).SetExpansion(1))
			row++
		}
	}
	for _, sec := range slices.Sorted(maps.Keys(sections)) {
		if seen[sec] {
			continue
		}
		tv.SetCell(row, 0, tview.NewTableCell(strings.ToLower(sec)).
			SetTextColor(a.th.Title).SetSelectable(false))
		row++
		for _, k := range slices.Sorted(maps.Keys(sections[sec])) {
			tv.SetCell(row, 0, tview.NewTableCell(" "+k).SetTextColor(a.th.Dim).SetSelectable(false))
			tv.SetCell(row, 1, tview.NewTableCell(sections[sec][k]).SetTextColor(a.th.Text).
				SetSelectable(false).SetExpansion(1))
			row++
		}
	}
	tv.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == 'i' ||
			ev.Rune() == 'q' {
			a.closeModal("info")
			return nil
		}
		return ev
	})
	a.showModal("info", tv, 84, 30)
}

func parseInfo(raw string) map[string]map[string]string {
	out := map[string]map[string]string{}
	var cur string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			cur = strings.TrimSpace(line[2:])
			if _, ok := out[cur]; !ok {
				out[cur] = map[string]string{}
			}
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok && cur != "" {
			out[cur][k] = v
		}
	}
	return out
}

// switchDB selects another database and restarts browsing (standalone only).
func (a *App) switchDB(db int) {
	c := a.rc.Load()
	if c == nil {
		return
	}
	if c.P.Cluster {
		a.flash("cluster mode only exposes db 0", a.th.Dim)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := c.Client.Do(ctx, "select", db).Err(); err != nil {
			a.tapp.QueueUpdateDraw(func() { a.flash("select: "+err.Error(), a.th.Error) })
			return
		}
		a.tapp.QueueUpdateDraw(func() {
			c.P.DB = db
			a.flash(fmt.Sprintf("db%d", db), a.th.OK)
			n, _ := c.DBSize(ctx)
			lat, _ := c.Ping(ctx)
			a.setStatusConnected(c.P, c, n, lat)
			a.startScan("*")
		})
	}()
}

// dbKey handles digits 0-9 for database switching.
func (a *App) dbKey(r rune) bool {
	if r < '0' || r > '9' {
		return false
	}
	db, _ := strconv.Atoi(string(r))
	a.switchDB(db)
	return true
}
