package app

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"strconv"

	"tedis/internal/config"
	"tedis/internal/encode"
	"tedis/internal/i18n"
	"tedis/internal/theme"
)

// openSettings edits app-wide settings: theme, language, scan batch size,
// key separator and fold level. Saved to config.toml and applied live.
func (a *App) openSettings() {
	s := a.cfg.Settings
	themeIdx, langIdx := 0, 0
	if s.Theme == "light" {
		themeIdx = 1
	}
	if s.Language == "zh-CN" {
		langIdx = 1
	}
	scan, sep, fold := s.ScanCount, s.Separator, s.MaxFoldLevel

	form := tview.NewForm().SetButtonsAlign(tview.AlignCenter).
		SetFieldBackgroundColor(tcell.ColorDefault)
	form.SetLabelColor(a.th.Dim)
	form.AddDropDown("theme", []string{"dark", "light"}, themeIdx, func(opt string, _ int) {
		if opt != "" {
			s.Theme = opt
		}
	})
	form.AddDropDown("language", []string{"en", "zh-CN"}, langIdx, func(opt string, _ int) {
		if opt != "" {
			s.Language = opt
		}
	})
	a.addInput(form, "scan count", strconv.Itoa(scan), 6, func(v string) {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			scan = n
		}
	})
	a.addInput(form, "separator", sep, 4, func(v string) {
		if v != "" {
			sep = v
		}
	})
	a.addInput(form, "max fold level", strconv.Itoa(fold), 4, func(v string) {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			fold = n
		}
	})
	form.AddButton("save", func() {
		a.cfg.Settings = config.Settings{
			Theme: s.Theme, Language: s.Language,
			ScanCount: scan, Separator: sep, MaxFoldLevel: fold,
		}
		if err := config.Save(a.cfgPath, a.cfg); err != nil {
			a.flash("save: "+err.Error(), a.th.Error)
			return
		}
		a.closeModal("settings")
		a.applySettings()
	})
	form.AddButton("cancel", func() { a.closeModal("settings") })
	form.SetCancelFunc(func() { a.closeModal("settings") })
	form.SetBorder(true).SetTitle(" settings ").SetTitleColor(a.th.Title)
	a.showModal("settings", form, 56, 14)
}

// applySettings activates settings that have runtime effects.
func (a *App) applySettings() {
	i18n.Set(a.cfg.Settings.Language)
	a.th = theme.ByName(a.cfg.Settings.Theme)
	theme.Apply(a.th)
	encode.ClearExternalCache() // user may have dropped in new encoder scripts
	a.setHints()
	a.flash("settings saved", a.th.OK)
	// separator/fold/scan affect browsing: rescan with fresh profile defaults
	if c := a.rc.Load(); c != nil {
		if p, ok := a.cfg.Get(c.P.Name); ok {
			if p.Name == "" {
				p = c.P // unsaved ad-hoc profile: keep runtime fields
				p.Separator, p.MaxFoldLevel, p.ScanCount = a.cfg.Settings.Separator, a.cfg.Settings.MaxFoldLevel, a.cfg.Settings.ScanCount
			}
			c.P = p
		}
		a.startScan(a.scanPattern())
	}
}

func (a *App) scanPattern() string {
	if a.scan != nil {
		return a.scan.pattern
	}
	return "*"
}
