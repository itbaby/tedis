// Package theme centralizes all UI colors. No primitive should hardcode a
// color; everything goes through a Theme so dark/light stay consistent.
package theme

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Theme is the set of color tokens used across the app.
type Theme struct {
	Border      tcell.Color // pane borders (unfocused)
	BorderFocus tcell.Color // pane borders (focused)
	Title       tcell.Color // pane titles
	Dim         tcell.Color // metadata, hints, ttl, counts
	Text        tcell.Color // primary text
	SelBg       tcell.Color // selection background
	SelFg       tcell.Color // selection foreground

	TypeString tcell.Color
	TypeHash   tcell.Color
	TypeList   tcell.Color
	TypeSet    tcell.Color
	TypeZSet   tcell.Color
	TypeStream tcell.Color

	Read  tcell.Color // readonly commands (query view)
	Write tcell.Color // write commands (query view)
	Error tcell.Color
	Warn  tcell.Color
	OK    tcell.Color

	JSONKey    tcell.Color
	JSONString tcell.Color
	JSONNumber tcell.Color
	JSONBool   tcell.Color
	JSONNull   tcell.Color
}

// Dark is the default palette (validated in the layout prototype).
var Dark = Theme{
	Border:      tcell.NewRGBColor(59, 66, 86),    // #3b4256
	BorderFocus: tcell.NewRGBColor(84, 94, 118),   // #545e76
	Title:       tcell.NewRGBColor(137, 180, 250), // #89b4fa
	Dim:         tcell.NewRGBColor(110, 118, 135), // #6e7687
	Text:        tcell.NewRGBColor(220, 224, 232), // #dce0e8
	SelBg:       tcell.NewRGBColor(49, 66, 89),    // #314259
	SelFg:       tcell.ColorWhite,

	TypeString: tcell.NewRGBColor(229, 192, 123), // #e5c07b
	TypeHash:   tcell.NewRGBColor(198, 120, 221), // #c678dd
	TypeList:   tcell.NewRGBColor(152, 195, 121), // #98c379
	TypeSet:    tcell.NewRGBColor(86, 182, 194),  // #56b6c2
	TypeZSet:   tcell.NewRGBColor(97, 175, 239),  // #61afef
	TypeStream: tcell.NewRGBColor(224, 108, 117), // #e06c75

	Read:  tcell.NewRGBColor(229, 192, 123),
	Write: tcell.NewRGBColor(97, 175, 239),
	Error: tcell.NewRGBColor(224, 108, 117),
	Warn:  tcell.NewRGBColor(229, 192, 123),
	OK:    tcell.NewRGBColor(152, 195, 121),

	JSONKey:    tcell.NewRGBColor(97, 175, 239),  // #61afef
	JSONString: tcell.NewRGBColor(152, 195, 121), // #98c379
	JSONNumber: tcell.NewRGBColor(209, 154, 102), // #d19a66
	JSONBool:   tcell.NewRGBColor(198, 120, 221), // #c678dd
	JSONNull:   tcell.NewRGBColor(86, 182, 194),  // #56b6c2
}

// Light is a light-background variant.
var Light = Theme{
	Border:      tcell.NewRGBColor(200, 204, 214),
	BorderFocus: tcell.NewRGBColor(150, 156, 172),
	Title:       tcell.NewRGBColor(61, 108, 186),
	Dim:         tcell.NewRGBColor(120, 126, 140),
	Text:        tcell.NewRGBColor(40, 44, 52),
	SelBg:       tcell.NewRGBColor(215, 224, 240),
	SelFg:       tcell.ColorBlack,

	TypeString: tcell.NewRGBColor(160, 110, 20),
	TypeHash:   tcell.NewRGBColor(140, 60, 170),
	TypeList:   tcell.NewRGBColor(80, 120, 50),
	TypeSet:    tcell.NewRGBColor(20, 110, 120),
	TypeZSet:   tcell.NewRGBColor(40, 100, 180),
	TypeStream: tcell.NewRGBColor(180, 60, 70),

	Read:  tcell.NewRGBColor(160, 110, 20),
	Write: tcell.NewRGBColor(40, 100, 180),
	Error: tcell.NewRGBColor(180, 50, 60),
	Warn:  tcell.NewRGBColor(160, 110, 20),
	OK:    tcell.NewRGBColor(60, 120, 40),

	JSONKey:    tcell.NewRGBColor(44, 90, 160),
	JSONString: tcell.NewRGBColor(61, 122, 61),
	JSONNumber: tcell.NewRGBColor(160, 90, 28),
	JSONBool:   tcell.NewRGBColor(122, 61, 158),
	JSONNull:   tcell.NewRGBColor(28, 122, 128),
}

// ByName returns the named theme (default: dark).
func ByName(name string) Theme {
	if name == "light" {
		return Light
	}
	return Dark
}

// Apply installs the theme globally: tview styles, borders, and the
// single-line focus border (tview's default is double-line, too heavy).
// Note: NO_COLOR is honored by tcell itself at render time; no special
// handling needed here (colors degrade to attributes).
func Apply(t Theme) {
	// Compact chrome: focus is indicated by border color, never double lines.
	tview.Borders.TopLeftFocus = tview.Borders.TopLeft
	tview.Borders.TopRightFocus = tview.Borders.TopRight
	tview.Borders.BottomLeftFocus = tview.Borders.BottomLeft
	tview.Borders.BottomRightFocus = tview.Borders.BottomRight
	tview.Borders.HorizontalFocus = tview.Borders.Horizontal
	tview.Borders.VerticalFocus = tview.Borders.Vertical

	tview.Styles.PrimaryTextColor = t.Text
	tview.Styles.SecondaryTextColor = t.Dim
	tview.Styles.TertiaryTextColor = t.Dim
	tview.Styles.TitleColor = t.Title
	tview.Styles.ContrastBackgroundColor = t.SelBg
	tview.Styles.MoreContrastBackgroundColor = t.SelBg
	tview.Styles.InverseTextColor = t.SelFg
	tview.Styles.BorderColor = t.Border
	tview.Styles.GraphicsColor = t.Dim
	// Background is the terminal's own: never paint a palette color, so the
	// app inherits the user's terminal theme (painting "black" would render
	// as whatever their palette slot 0 is — often gray).
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
}
