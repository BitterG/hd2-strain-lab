package uiapp

import (
	"github.com/egoist/mygo/ui"
	"hd2-mygo-cpv/internal/catalog"
)

var (
	BgDeep      = ui.Hex("#080B12")
	BgMid       = ui.Hex("#0E1420")
	BgPanel     = ui.Hex("#111827")
	BgCard      = ui.Hex("#172033")
	BgCardHover = ui.Hex("#1E2A42")
	BorderSub   = ui.RGBA(255, 255, 255, 0.11)
	BorderHi    = ui.RGBA(255, 255, 255, 0.24)

	TextPrimary = ui.Hex("#E2E8F0")
	TextDim     = ui.Hex("#94A3B8")
	TextMute    = ui.Hex("#64748B")

	HD2Gold     = ui.Hex("#F5C518")
	HD2GoldDim  = ui.RGBA(245, 197, 24, 0.18)
	CyanActive  = ui.Hex("#51D9C7")
	CyanBright  = ui.Hex("#65F0DB")
	BlueAccent  = ui.Hex("#3B82F6")
	GreenOK     = ui.Hex("#22C55E")
	RedDanger   = ui.Hex("#EF4444")

	ColorHumans     = ui.Hex("#5BA3D0")
	ColorTerminids  = ui.Hex("#F59E0B")
	ColorAutomatons = ui.Hex("#EF4444")
	ColorIlluminate = ui.Hex("#AA44FF")
	ColorUnknown    = ui.Hex("#64748B")
)

func FactionColor(faction int) ui.Color {
	switch faction {
	case catalog.FactionHumans:
		return ColorHumans
	case catalog.FactionTerminids:
		return ColorTerminids
	case catalog.FactionAutomatons:
		return ColorAutomatons
	case catalog.FactionIlluminate:
		return ColorIlluminate
	default:
		return ColorUnknown
	}
}

func NewHD2Theme() *ui.Theme {
	t := ui.DarkTheme()
	t.Background = BgDeep
	t.Surface = BgCard
	t.SurfaceHover = BgCardHover
	t.SurfacePressed = ui.Hex("#253452")
	t.Border = ui.Hex("#253349")
	t.Text = TextPrimary
	t.TextMuted = TextDim
	t.Accent = HD2Gold
	t.AccentHover = ui.Hex("#FAD44B")
	t.AccentPressed = ui.Hex("#D9AC11")
	t.AccentText = ui.Hex("#14140A")
	t.Danger = RedDanger
	t.Warning = ColorTerminids
	t.Success = GreenOK
	t.Selection = ui.RGBA(91, 163, 208, 0.35)
	t.Focus = ui.RGBA(245, 197, 24, 0.55)
	t.Radius = 7
	t.Spacing = 4
	t.FontSize = 13
	t.Font = "Microsoft YaHei UI"
	return t
}
