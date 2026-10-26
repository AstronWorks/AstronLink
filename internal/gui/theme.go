// Package gui is a small widget kit on top of Gio for the Windows management
// consoles: a light/dark palette, typography, cards, buttons, form controls,
// tables, banners, toasts and modals. It has no platform dependencies, so the
// consoles can also be rendered headlessly (see cmd/uipreview).
package gui

import (
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// Palette holds every color the kit uses.
type Palette struct {
	Bg, Surface, SurfaceAlt, Sidebar   color.NRGBA
	Border, BorderStrong, Divider      color.NRGBA
	Text, TextMuted, TextSubtle        color.NRGBA
	Primary, PrimaryHover, PrimarySoft color.NRGBA
	OnPrimary                          color.NRGBA
	Success, SuccessSoft               color.NRGBA
	Warning, WarningSoft               color.NRGBA
	Danger, DangerSoft                 color.NRGBA
	Info, InfoSoft                     color.NRGBA
	InputBg, Hover, Pressed, Scrim     color.NRGBA
	Inverse, OnInverse                 color.NRGBA
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// RGB returns an opaque color from 0xRRGGBB.
func RGB(v uint32) color.NRGBA { return rgb(v) }

func argb(v uint32) color.NRGBA {
	return color.NRGBA{A: uint8(v >> 24), R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}
}

// WithAlpha returns c with its alpha replaced.
func WithAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

// LightPalette is the default light scheme.
func LightPalette() Palette {
	return Palette{
		Bg:           rgb(0xF5F6F8),
		Surface:      rgb(0xFFFFFF),
		SurfaceAlt:   rgb(0xF1F3F6),
		Sidebar:      rgb(0xFBFBFC),
		Border:       rgb(0xE4E7EC),
		BorderStrong: rgb(0xCDD3DC),
		Divider:      rgb(0xEEF0F3),
		Text:         rgb(0x101828),
		TextMuted:    rgb(0x475467),
		TextSubtle:   rgb(0x8A94A6),
		Primary:      rgb(0x4F46E5),
		PrimaryHover: rgb(0x4338CA),
		PrimarySoft:  rgb(0xEEF0FF),
		OnPrimary:    rgb(0xFFFFFF),
		Success:      rgb(0x079455),
		SuccessSoft:  rgb(0xE7F8EF),
		Warning:      rgb(0xB54708),
		WarningSoft:  rgb(0xFEF4E6),
		Danger:       rgb(0xD92D20),
		DangerSoft:   rgb(0xFEEEEC),
		Info:         rgb(0x1570EF),
		InfoSoft:     rgb(0xEAF2FE),
		InputBg:      rgb(0xFFFFFF),
		Hover:        argb(0x0A101828),
		Pressed:      argb(0x14101828),
		Scrim:        argb(0x66101828),
		Inverse:      rgb(0x1D2433),
		OnInverse:    rgb(0xF5F6F8),
	}
}

// DarkPalette is the dark scheme.
func DarkPalette() Palette {
	return Palette{
		Bg:           rgb(0x0E1117),
		Surface:      rgb(0x161B24),
		SurfaceAlt:   rgb(0x1E2530),
		Sidebar:      rgb(0x12161E),
		Border:       rgb(0x262E3B),
		BorderStrong: rgb(0x364152),
		Divider:      rgb(0x1F2632),
		Text:         rgb(0xEEF1F5),
		TextMuted:    rgb(0xA5AEBD),
		TextSubtle:   rgb(0x6B7586),
		Primary:      rgb(0x7C83F7),
		PrimaryHover: rgb(0x9398FA),
		PrimarySoft:  rgb(0x252A4A),
		OnPrimary:    rgb(0x0E1117),
		Success:      rgb(0x3CCB7F),
		SuccessSoft:  rgb(0x15291F),
		Warning:      rgb(0xF4A340),
		WarningSoft:  rgb(0x2E2213),
		Danger:       rgb(0xF97066),
		DangerSoft:   rgb(0x341A19),
		Info:         rgb(0x6AA7F8),
		InfoSoft:     rgb(0x16243A),
		InputBg:      rgb(0x11151C),
		Hover:        argb(0x10FFFFFF),
		Pressed:      argb(0x1CFFFFFF),
		Scrim:        argb(0x99000000),
		Inverse:      rgb(0xEEF1F5),
		OnInverse:    rgb(0x161B24),
	}
}

// Theme is the material theme plus the kit's palette and typefaces.
type Theme struct {
	*material.Theme
	P    Palette
	Dark bool
	Mono font.Typeface
}

// NewTheme builds a theme using the fonts found by Fonts.
func NewTheme(dark bool) *Theme {
	mt := material.NewTheme()
	mt.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(Fonts()))
	mt.Face = SansTypeface
	mt.TextSize = 14
	t := &Theme{Theme: mt, Mono: MonoTypeface}
	t.SetDark(dark)
	return t
}

// SetDark switches between the light and dark palettes.
func (t *Theme) SetDark(dark bool) {
	t.Dark = dark
	if dark {
		t.P = DarkPalette()
	} else {
		t.P = LightPalette()
	}
	t.Palette = material.Palette{Bg: t.P.Surface, Fg: t.P.Text, ContrastBg: t.P.Primary, ContrastFg: t.P.OnPrimary}
}

// Label returns a single-style label.
func (t *Theme) Label(size unit.Sp, s string, col color.NRGBA, weight font.Weight) material.LabelStyle {
	l := material.Label(t.Theme, size, s)
	l.Color = col
	l.Font.Weight = weight
	l.SelectionColor = WithAlpha(t.P.Primary, 0x50)
	return l
}

// Title is a page title.
func (t *Theme) Title(s string) material.LabelStyle {
	return t.Label(22, s, t.P.Text, font.SemiBold)
}

// Heading is a card or section heading.
func (t *Theme) Heading(s string) material.LabelStyle {
	return t.Label(15, s, t.P.Text, font.SemiBold)
}

// Body is regular text.
func (t *Theme) Body(s string) material.LabelStyle {
	return t.Label(14, s, t.P.Text, font.Normal)
}

// Muted is secondary text.
func (t *Theme) Muted(s string) material.LabelStyle {
	return t.Label(13, s, t.P.TextMuted, font.Normal)
}

// Caption is small, low-emphasis text.
func (t *Theme) Caption(s string) material.LabelStyle {
	return t.Label(12, s, t.P.TextSubtle, font.Normal)
}

// Code is monospaced text.
func (t *Theme) Code(s string) material.LabelStyle {
	l := t.Label(13, s, t.P.Text, font.Normal)
	l.Font.Typeface = t.Mono
	return l
}

// Kind selects the semantic color of banners, badges and toasts.
type Kind int

const (
	KindNeutral Kind = iota
	KindInfo
	KindSuccess
	KindWarning
	KindDanger
	KindPrimary
)

// KindColors returns the strong and soft colors of k.
func (t *Theme) KindColors(k Kind) (fg, soft color.NRGBA) {
	switch k {
	case KindInfo:
		return t.P.Info, t.P.InfoSoft
	case KindSuccess:
		return t.P.Success, t.P.SuccessSoft
	case KindWarning:
		return t.P.Warning, t.P.WarningSoft
	case KindDanger:
		return t.P.Danger, t.P.DangerSoft
	case KindPrimary:
		return t.P.Primary, t.P.PrimarySoft
	}
	return t.P.TextMuted, t.P.SurfaceAlt
}
