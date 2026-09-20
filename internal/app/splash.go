package app

import (
	"fmt"
	"math"
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
	card.SetBackgroundColor(tcell.ColorBlack)
	card.SetBorder(true).
		SetTitle(" tedis ").
		SetTitleColor(a.th.Title).
		SetBorderColor(a.th.Title)
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

	a.openModals["splash"] = true
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
	delete(a.openModals, "splash")
	a.pages.RemovePage("splash")
	a.splash = nil
	a.pages.ShowPage("main")
	a.tapp.SetFocus(a.keys) // boot overlay always exits to panel-selection mode
	a.applyFocusStyles()
}

// renderSplash paints the animated card: a rainbow wordmark, a live progress
// bar, and an opencode-style braille spinner that recolors as it turns.
func (a *App) renderSplash(p float64, tick int) {
	th := a.th
	const bw = 28
	fill := int(p * float64(bw))
	bar := strings.Repeat("▓", fill) + strings.Repeat("░", bw-fill)

	pal := []tcell.Color{th.Title, th.TypeHash, th.TypeSet, th.Write, th.TypeString}
	spin := spinFrames[tick%len(spinFrames)]
	spinCol := hex(pal[(tick/2)%len(pal)])

	content := fmt.Sprintf(`%s

[%s]the Redis GUI that lives in your terminal[-]
[%s]no Electron · no Chromium · no regrets[-]

[%s]%s %s[-]
[%s]%c[-]  [%s]%s[-]

[%s]crafted by itbaby[-] · [%s]github.com/itbaby/tedis[-]`,
		gradient(splashLogo),
		hex(th.Title),
		hex(th.SelFg),
		hex(th.OK), bar, fmt.Sprintf("%3d%%", int(p*100)),
		spinCol, spin,
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

// spinFrames is the braille dots spinner popularized by opencode / Copilot CLI.
var spinFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// gradient wraps every glyph of s in a truecolor code that sweeps the hue
// across the whole block, giving the wordmark a colorful rainbow.
func gradient(s string) string {
	runes := []rune(s)
	total := len(runes)
	var b strings.Builder
	for i, r := range runes {
		if r == ' ' || r == '\n' {
			b.WriteRune(r)
			continue
		}
		fmt.Fprintf(&b, "[#%06x]%c[-]", hsl2rgb(float64(i)/float64(total), 0.9, 0.66), r)
	}
	return b.String()
}

// hsl2rgb converts HSL (h, s, l in [0,1]) to a packed 0xRRGGBB integer.
func hsl2rgb(h, s, l float64) int {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h*6, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 1.0/6:
		r, g, b = c, x, 0
	case h < 2.0/6:
		r, g, b = x, c, 0
	case h < 3.0/6:
		r, g, b = 0, c, x
	case h < 4.0/6:
		r, g, b = 0, x, c
	case h < 5.0/6:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return int((r+m)*255+.5)<<16 | int((g+m)*255+.5)<<8 | int((b+m)*255+.5)
}
