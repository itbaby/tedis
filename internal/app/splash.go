package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// splashLogo is a plain-ASCII wordmark: letterforms stay monospace-safe in
// every terminal, so centering never drifts.
const splashLogo = `TTTTTT EEEEEE DDDDDD   IIIIII SSSSSS
  TT   EE     DD   DD     II   SS
  TT   EEEE   DD   DD     II   SSSSS
  TT   EE     DD   DD     II       SS
  TT   EEEEEE DDDDDD   IIIIII SSSSSS`

// splashStatus rotates through loading lines by progress bucket — the joke is
// that there is nothing actually slow here, we just enjoy the drama.
func splashStatus(p float64) string {
	lines := []string{
		"waking the keyspace…",
		"teaching SCAN to mind its manners…",
		"evicting Electron from memory…",
		"compressing a GUI into one binary…",
		"tuning the µs-latency dial…",
		"polishing the namespace tree…",
		"hiding from the garbage collector…",
		"brewing coffee, compiling nothing…",
	}
	i := int(p * float64(len(lines)))
	if i >= len(lines) {
		i = len(lines) - 1
	}
	return lines[i]
}

// showSplash paints a full-screen boot card and dismisses it after splashDur,
// keeping the app blocked (and focus held) until it is gone.
const splashDur = 3 * time.Second

func (a *App) showSplash() {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(a.th.Text).
		SetTextAlign(tview.AlignCenter)
	tv.SetBackgroundColor(a.th.SelBg)
	tv.SetBorder(true).
		SetTitle(" tedis · redis in your terminal ").
		SetTitleColor(a.th.Title).
		SetBorderColor(a.th.Title)
	tv.SetInputCapture(func(*tcell.EventKey) *tcell.EventKey { return nil }) // eat everything
	a.splash = tv

	a.openModals["splash"] = true
	a.pages.AddPage("splash", tv, true, true)

	a.renderSplash(0)
	go func() {
		start := time.Now()
		const frame = time.Second / 20
		for {
			p := float64(time.Since(start)) / float64(splashDur)
			if p > 1 {
				p = 1
			}
			a.tapp.QueueUpdateDraw(func() {
				a.tapp.SetFocus(a.splash) // hold focus against auto-connect
				a.renderSplash(p)
			})
			if p >= 1 {
				break
			}
			time.Sleep(frame)
		}
		a.tapp.QueueUpdateDraw(a.dismissSplash)
	}()
}

func (a *App) dismissSplash() {
	delete(a.openModals, "splash")
	a.pages.RemovePage("splash")
	a.splash = nil
	a.pages.ShowPage("main")
	a.tapp.SetFocus(a.keys) // boot overlay always exits to panel-selection mode
	a.applyFocusStyles()
}

// renderSplash centers the boot card inside the terminal and animates the bar.
func (a *App) renderSplash(p float64) {
	th := a.th
	const bw = 28
	fill := int(p * float64(bw))
	bar := strings.Repeat("▓", fill) + strings.Repeat("░", bw-fill)

	content := fmt.Sprintf(`[%s]%s[-]

[%s]the Redis GUI that lives in your terminal[-]
[%s]no Electron · no Chromium · no regrets[-]

[%s]%s %s[-]
[%s]  %s[-]

[%s]crafted by itbaby[-] · [%s]github.com/itbaby/tedis[-]`,
		hex(th.Title), splashLogo,
		hex(th.SelFg),
		hex(th.Dim),
		hex(th.OK), bar, fmt.Sprintf("%3d%%", int(p*100)),
		hex(th.Warn), splashStatus(p),
		hex(th.Text), hex(th.Dim))

	lines := strings.Count(content, "\n") + 1
	_, _, _, ih := a.splash.GetInnerRect()
	pad := (ih - lines) / 2
	if pad < 0 {
		pad = 0
	}
	a.splash.SetText(strings.Repeat("\n", pad) + content)
}
