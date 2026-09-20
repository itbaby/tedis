package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// splashLogo is the ANSI-Shadow wordmark. Every row is padded to the same 36
// columns so AlignCenter keeps the block perfectly stacked (per-line centering
// would otherwise shear rows of differing width).
const splashLogo = `████████╗███████╗██████╗ ██╗███████╗
╚══██╔══╝██╔════╝██╔══██╗██║██╔════╝
   ██║   █████╗  ██║  ██║██║███████╗
   ██║   ██╔══╝  ██║  ██║██║╚════██║
   ██║   ███████╗██████╔╝██║███████║
   ╚═╝   ╚══════╝╚═════╝ ╚═╝╚══════╝`

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

// spinFrames is the braille dots spinner popularized by opencode / Copilot CLI.
var spinFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// showSplash paints a small, centered boot card (the main page is hidden while
// it runs) and dismisses it after splashDur, keeping the app blocked — and
// focus held — until it is gone.
const (
	splashDur = 3 * time.Second
	splashW   = 52
	splashH   = 16
)

func (a *App) showSplash() {
	card := tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(a.th.Text).
		SetTextAlign(tview.AlignCenter)
	card.SetBackgroundColor(tcell.ColorDefault)                                // no filled box — inherit the terminal
	card.SetBorder(false)                                                      // wordmark carries the name; skip the box
	card.SetInputCapture(func(*tcell.EventKey) *tcell.EventKey { return nil }) // eat everything
	a.splash = card

	// Center a fixed-size card. A Flex is transparent, so we hide the main page
	// for the duration of the splash to keep the panes from bleeding through.
	mid := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(card, splashW, 0, true).
		AddItem(nil, 0, 1, false)
	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(mid, splashH, 0, false).
		AddItem(nil, 0, 1, false)

	a.pushModal("splash")
	a.pages.HidePage("main")
	a.pages.AddPage("splash", root, true, true)

	a.renderSplash(0, 0)
	go func() {
		start := time.Now()
		const frame = time.Second / 20
		tick := 0
		for {
			p := float64(time.Since(start)) / float64(splashDur)
			if p > 1 {
				p = 1
			}
			t := tick
			a.tapp.QueueUpdateDraw(func() {
				a.tapp.SetFocus(a.splash) // hold focus against auto-connect
				a.renderSplash(p, t)
			})
			if p >= 1 {
				break
			}
			tick++
			time.Sleep(frame)
		}
		a.tapp.QueueUpdateDraw(a.dismissSplash)
	}()
}

func (a *App) dismissSplash() {
	a.popModal("splash")
	a.pages.RemovePage("splash")
	a.splash = nil
	a.pages.ShowPage("main")
	a.tapp.SetFocus(a.keys) // boot overlay always exits to panel-selection mode
	a.applyFocusStyles()
}

// renderSplash paints the boot card in a quiet grayscale palette (white
// foreground on grays): wordmark, slogan, a two-tone progress bar, a braille
// spinner, and the sign-off.
func (a *App) renderSplash(p float64, tick int) {
	th := a.th
	bar := loadBar(p, hex(th.Text), hex(th.Border))
	pct := fmt.Sprintf("%3d%%", int(p*100))
	spin := spinFrames[tick%len(spinFrames)]

	content := fmt.Sprintf(`[%s]%s[-]

[%s]the Redis GUI that lives in your terminal[-]
[%s]no Electron · no Chromium · no regrets[-]

%s [%s]%s[-]
[%s]%c[-]  [%s]%s[-]

[%s]crafted by itbaby[-] · [%s]github.com/itbaby/tedis[-]`,
		hex(th.Text), splashLogo,
		hex(th.Text),
		hex(th.Dim),
		bar, hex(th.Dim), pct,
		hex(th.Text), spin,
		hex(th.Dim), splashStatus(p),
		hex(th.Dim), hex(th.Dim))

	lines := strings.Count(content, "\n") + 1
	_, _, _, ih := a.splash.GetInnerRect()
	pad := (ih - lines) / 2
	if pad < 0 {
		pad = 0
	}
	a.splash.SetText(strings.Repeat("\n", pad) + content)
}

// loadBar renders a bw-cell track: filled cells in fg, the remainder in bg.
func loadBar(p float64, fg, bg string) string {
	const bw = 30
	fill := int(p*float64(bw) + 0.5)
	return "[" + fg + "]" + strings.Repeat("▰", fill) +
		"[-][" + bg + "]" + strings.Repeat("▱", bw-fill) + "[-]"
}
