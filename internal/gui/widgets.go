package gui

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// FillRRect paints a rounded rectangle covering r. The radius is clamped to
// half the shorter side, so a large radius yields a pill.
func FillRRect(gtx C, r image.Rectangle, radius int, col color.NRGBA) {
	if col.A == 0 || r.Empty() {
		return
	}
	if m := min(r.Dx(), r.Dy()) / 2; radius > m {
		radius = m
	}
	paint.FillShape(gtx.Ops, col, clip.UniformRRect(r, radius).Op(gtx.Ops))
}

// FillCircle paints the ellipse inscribed in r.
func FillCircle(gtx C, r image.Rectangle, col color.NRGBA) {
	paint.FillShape(gtx.Ops, col, clip.Ellipse(r).Op(gtx.Ops))
}

// Box draws a rounded background, an optional 1dp border and an inset
// behind its content. Border needs an opaque Bg (the fill covers the border's
// interior).
type Box struct {
	Bg, Border color.NRGBA
	Radius     unit.Dp
	Inset      layout.Inset
	// Expand stretches the box to the maximum width.
	Expand bool
	// Cursor is shown while the pointer is over the box.
	Cursor pointer.Cursor
}

func (b Box) Layout(gtx C, w layout.Widget) D {
	if b.Expand {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
	}
	return layout.Background{}.Layout(gtx,
		func(gtx C) D {
			sz := gtx.Constraints.Min
			rect := image.Rectangle{Max: sz}
			r := gtx.Dp(b.Radius)
			if b.Border.A > 0 {
				bw := max(1, gtx.Dp(1))
				FillRRect(gtx, rect, r, b.Border)
				FillRRect(gtx, rect.Inset(bw), max(r-bw, 0), b.Bg)
			} else {
				FillRRect(gtx, rect, r, b.Bg)
			}
			if b.Cursor != pointer.CursorDefault {
				defer clip.Rect(rect).Push(gtx.Ops).Pop()
				b.Cursor.Add(gtx.Ops)
			}
			return D{Size: sz}
		},
		func(gtx C) D {
			return b.Inset.Layout(gtx, func(gtx C) D {
				d := w(gtx)
				if b.Expand {
					d.Size.X = max(d.Size.X, gtx.Constraints.Min.X)
				}
				return d
			})
		})
}

// Card is the standard content container.
func (t *Theme) Card(gtx C, w layout.Widget) D {
	return Box{Bg: t.P.Surface, Border: t.P.Border, Radius: 12, Inset: layout.UniformInset(20), Expand: true}.Layout(gtx, w)
}

// CardFlush is a card without padding, for tables and lists.
func (t *Theme) CardFlush(gtx C, w layout.Widget) D {
	return Box{Bg: t.P.Surface, Border: t.P.Border, Radius: 12, Inset: layout.UniformInset(1), Expand: true}.Layout(gtx, w)
}

// Divider is a full-width hairline.
func (t *Theme) Divider(gtx C) D {
	sz := image.Pt(gtx.Constraints.Max.X, max(1, gtx.Dp(1)))
	paint.FillShape(gtx.Ops, t.P.Divider, clip.Rect{Max: sz}.Op())
	return D{Size: sz}
}

// VSpace and HSpace are fixed gaps.
func VSpace(v unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Height: v}.Layout) }
func HSpace(v unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Width: v}.Layout) }

// Nothing is an empty widget.
func Nothing(C) D { return D{} }

// IconW renders ic at a fixed size.
func IconW(ic *widget.Icon, size unit.Dp, col color.NRGBA) layout.Widget {
	return func(gtx C) D {
		s := gtx.Dp(size)
		gtx.Constraints = layout.Exact(image.Pt(s, s))
		return ic.Layout(gtx, col)
	}
}

// ButtonKind selects a button's emphasis.
type ButtonKind int

const (
	BtnPrimary ButtonKind = iota
	BtnSecondary
	BtnGhost
	BtnDanger
	BtnSoftDanger
)

// Button is a clickable with the kit's look.
type Button struct {
	th       *Theme
	clk      *widget.Clickable
	Text     string
	Icon     *widget.Icon
	Kind     ButtonKind
	Disabled bool
	Small    bool
}

// Btn creates a button; ic may be nil and label may be empty (icon-only).
func (t *Theme) Btn(clk *widget.Clickable, kind ButtonKind, label string, ic *widget.Icon) Button {
	return Button{th: t, clk: clk, Text: label, Icon: ic, Kind: kind}
}

// WithDisabled returns b disabled when d is true.
func (b Button) WithDisabled(d bool) Button { b.Disabled = d; return b }

// WithSmall returns a compact b.
func (b Button) WithSmall() Button { b.Small = true; return b }

func (b Button) Layout(gtx C) D {
	t := b.th
	var bg, fg, border color.NRGBA
	switch b.Kind {
	case BtnPrimary:
		bg, fg = t.P.Primary, t.P.OnPrimary
		if b.clk.Hovered() && !b.Disabled {
			bg = t.P.PrimaryHover
		}
	case BtnSecondary:
		bg, fg, border = t.P.Surface, t.P.Text, t.P.BorderStrong
	case BtnGhost:
		fg = t.P.TextMuted
		if b.clk.Hovered() {
			fg = t.P.Text
		}
	case BtnDanger:
		bg, fg = t.P.Danger, rgb(0xFFFFFF)
	case BtnSoftDanger:
		bg, fg = t.P.DangerSoft, t.P.Danger
	}
	if b.Disabled {
		gtx = gtx.Disabled()
		bg.A, fg.A, border.A = bg.A/2, fg.A/2, border.A/2
	}
	gtx.Constraints.Min = image.Point{}
	return b.clk.Layout(gtx, func(gtx C) D {
		return layout.Background{}.Layout(gtx,
			func(gtx C) D {
				sz := gtx.Constraints.Min
				rect := image.Rectangle{Max: sz}
				r := gtx.Dp(8)
				if border.A > 0 {
					FillRRect(gtx, rect, r, border)
					FillRRect(gtx, rect.Inset(max(1, gtx.Dp(1))), r-1, bg)
				} else {
					FillRRect(gtx, rect, r, bg)
				}
				if !b.Disabled {
					switch {
					case b.clk.Pressed():
						FillRRect(gtx, rect, r, t.P.Pressed)
					case b.clk.Hovered() && b.Kind != BtnPrimary:
						FillRRect(gtx, rect, r, t.P.Hover)
					}
					defer clip.Rect(rect).Push(gtx.Ops).Pop()
					pointer.CursorPointer.Add(gtx.Ops)
				}
				return D{Size: sz}
			},
			func(gtx C) D {
				in := layout.Inset{Top: 8, Bottom: 8, Left: 14, Right: 14}
				iconSize := unit.Dp(17)
				if b.Small {
					in = layout.Inset{Top: 5, Bottom: 5, Left: 10, Right: 10}
					iconSize = 16
				}
				if b.Text == "" {
					in.Left, in.Right = in.Top, in.Top
				}
				return in.Layout(gtx, func(gtx C) D {
					children := make([]layout.FlexChild, 0, 3)
					if b.Icon != nil {
						children = append(children, layout.Rigid(IconW(b.Icon, iconSize, fg)))
					}
					if b.Icon != nil && b.Text != "" {
						children = append(children, HSpace(6))
					}
					if b.Text != "" {
						l := t.Label(13.5, b.Text, fg, font.SemiBold)
						l.MaxLines = 1
						children = append(children, layout.Rigid(l.Layout))
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
				})
			})
	})
}

// Badge is a small colored tag.
func (t *Theme) Badge(gtx C, k Kind, s string) D {
	gtx.Constraints.Min = image.Point{}
	fg, soft := t.KindColors(k)
	return Box{Bg: soft, Radius: 6, Inset: layout.Inset{Top: 2, Bottom: 2, Left: 8, Right: 8}}.Layout(gtx, func(gtx C) D {
		l := t.Label(12, s, fg, font.SemiBold)
		l.MaxLines = 1
		return l.Layout(gtx)
	})
}

// StatusPill is a rounded badge with a leading dot.
func (t *Theme) StatusPill(gtx C, k Kind, s string) D {
	gtx.Constraints.Min = image.Point{}
	fg, soft := t.KindColors(k)
	return Box{Bg: soft, Radius: 20, Inset: layout.Inset{Top: 4, Bottom: 4, Left: 10, Right: 12}}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return t.Dot(gtx, fg, 7) }),
			HSpace(7),
			layout.Rigid(func(gtx C) D {
				l := t.Label(12.5, s, fg, font.SemiBold)
				l.MaxLines = 1
				return l.Layout(gtx)
			}))
	})
}

// Dot is a filled circle.
func (t *Theme) Dot(gtx C, col color.NRGBA, size unit.Dp) D {
	s := gtx.Dp(size)
	FillCircle(gtx, image.Rect(0, 0, s, s), col)
	return D{Size: image.Pt(s, s)}
}

// Switch is a toggle bound to b.
func (t *Theme) Switch(gtx C, b *widget.Bool) D {
	w, h := gtx.Dp(38), gtx.Dp(22)
	return b.Layout(gtx, func(gtx C) D {
		rect := image.Rect(0, 0, w, h)
		track := t.P.BorderStrong
		if b.Value {
			track = t.P.Primary
		}
		thumb := rgb(0xFFFFFF)
		if !gtx.Enabled() {
			track.A, thumb.A = track.A/2, 0xB0
		}
		FillRRect(gtx, rect, h/2, track)
		if b.Hovered() && gtx.Enabled() {
			FillRRect(gtx, rect, h/2, t.P.Hover)
		}
		pad := gtx.Dp(3)
		d := h - 2*pad
		x := pad
		if b.Value {
			x = w - pad - d
		}
		FillCircle(gtx, image.Rect(x, pad, x+d, pad+d), thumb)
		if gtx.Enabled() {
			defer clip.Rect(rect).Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
		}
		return D{Size: rect.Max}
	})
}

// SegOption is one choice of a segmented control.
type SegOption struct{ Key, Label string }

// Segmented is a compact single-choice control bound to e.
func (t *Theme) Segmented(gtx C, e *widget.Enum, opts []SegOption) D {
	return Box{Bg: t.P.SurfaceAlt, Border: t.P.Border, Radius: 9, Inset: layout.UniformInset(3)}.Layout(gtx, func(gtx C) D {
		children := make([]layout.FlexChild, 0, len(opts)*2)
		hk, hov := e.Hovered()
		for i, o := range opts {
			if i > 0 {
				children = append(children, HSpace(2))
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				return e.Layout(gtx, o.Key, func(gtx C) D {
					sel := e.Value == o.Key
					box := Box{Radius: 7, Inset: layout.Inset{Top: 5, Bottom: 5, Left: 12, Right: 12}, Cursor: pointer.CursorPointer}
					fg, w := t.P.TextMuted, font.Medium
					switch {
					case sel:
						box.Bg, box.Border, fg, w = t.P.Surface, t.P.Border, t.P.Text, font.SemiBold
					case hov && hk == o.Key:
						box.Bg, fg = t.P.Hover, t.P.Text
					}
					return box.Layout(gtx, func(gtx C) D {
						l := t.Label(13, o.Label, fg, w)
						l.MaxLines = 1
						return l.Layout(gtx)
					})
				})
			}))
		}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// InputStyle configures Input.
type InputStyle struct {
	Hint     string
	Invalid  bool
	Mono     bool
	Trailing []layout.FlexChild
}

// Input lays out a bordered text editor filling the available width.
func (t *Theme) Input(gtx C, ed *widget.Editor, st InputStyle) D {
	border := t.P.BorderStrong
	switch {
	case st.Invalid:
		border = t.P.Danger
	case gtx.Focused(ed):
		border = t.P.Primary
	}
	box := Box{Bg: t.P.InputBg, Border: border, Radius: 8, Expand: true,
		Inset: layout.Inset{Top: 8, Bottom: 8, Left: 12, Right: 12}}
	if len(st.Trailing) > 0 {
		box.Inset = layout.Inset{Top: 4, Bottom: 4, Left: 12, Right: 4}
	}
	if !gtx.Enabled() || ed.ReadOnly {
		box.Bg = t.P.SurfaceAlt
	}
	return box.Layout(gtx, func(gtx C) D {
		children := make([]layout.FlexChild, 0, 1+len(st.Trailing))
		children = append(children, layout.Flexed(1, func(gtx C) D {
			in := layout.Inset{}
			if len(st.Trailing) > 0 {
				in = layout.Inset{Top: 4, Bottom: 4, Right: 6}
			}
			return in.Layout(gtx, func(gtx C) D {
				es := material.Editor(t.Theme, ed, st.Hint)
				es.Color, es.HintColor = t.P.Text, t.P.TextSubtle
				es.SelectionColor = WithAlpha(t.P.Primary, 0x40)
				es.TextSize = 14
				if st.Mono {
					es.Font.Typeface = t.Mono
					es.TextSize = 13.5
				}
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return es.Layout(gtx)
			})
		}))
		children = append(children, st.Trailing...)
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// FormRow lays out one setting: title and description on the left, the
// control (and its validation error) on the right; stacked when narrow.
func (t *Theme) FormRow(gtx C, title, desc, errText string, control layout.Widget) D {
	label := t.formLabel(title, desc)
	ctl := func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(control),
			layout.Rigid(func(gtx C) D {
				if errText == "" {
					return D{}
				}
				return layout.Inset{Top: 6}.Layout(gtx, t.Label(12.5, errText, t.P.Danger, font.Medium).Layout)
			}))
	}
	return layout.Inset{Top: 14, Bottom: 14}.Layout(gtx, func(gtx C) D {
		if gtx.Constraints.Max.X < gtx.Dp(600) {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(label), VSpace(8), layout.Rigid(ctl))
		}
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(0.44, func(gtx C) D {
				return layout.Inset{Right: 28, Top: 2}.Layout(gtx, label)
			}),
			layout.Flexed(0.56, ctl))
	})
}

// SwitchRow is a setting whose control is a toggle aligned to the right.
func (t *Theme) SwitchRow(gtx C, title, desc string, b *widget.Bool) D {
	return layout.Inset{Top: 14, Bottom: 14}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				return layout.Inset{Right: 24}.Layout(gtx, t.formLabel(title, desc))
			}),
			layout.Rigid(func(gtx C) D { return t.Switch(gtx, b) }))
	})
}

func (t *Theme) formLabel(title, desc string) layout.Widget {
	return func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(t.Label(14, title, t.P.Text, font.Medium).Layout),
			layout.Rigid(func(gtx C) D {
				if desc == "" {
					return D{}
				}
				return layout.Inset{Top: 3}.Layout(gtx, t.Label(12.5, desc, t.P.TextMuted, font.Normal).Layout)
			}))
	}
}

// SectionTitle is a heading with an optional description above a group.
func (t *Theme) SectionTitle(gtx C, title, desc string) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(t.Heading(title).Layout),
		layout.Rigid(func(gtx C) D {
			if desc == "" {
				return D{}
			}
			return layout.Inset{Top: 4}.Layout(gtx, t.Muted(desc).Layout)
		}))
}

// NavItem is a sidebar entry.
func (t *Theme) NavItem(gtx C, clk *widget.Clickable, ic *widget.Icon, label string, selected bool, badge string) D {
	return clk.Layout(gtx, func(gtx C) D {
		box := Box{Radius: 8, Inset: layout.Inset{Top: 9, Bottom: 9, Left: 12, Right: 10}, Expand: true, Cursor: pointer.CursorPointer}
		fg, icCol, w := t.P.TextMuted, t.P.TextSubtle, font.Medium
		switch {
		case selected:
			box.Bg, fg, icCol, w = t.P.PrimarySoft, t.P.Primary, t.P.Primary, font.SemiBold
		case clk.Hovered():
			box.Bg, fg, icCol = t.P.Hover, t.P.Text, t.P.TextMuted
		}
		return box.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(IconW(ic, 19, icCol)),
				HSpace(12),
				layout.Flexed(1, func(gtx C) D {
					l := t.Label(14, label, fg, w)
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D {
					if badge == "" {
						return D{}
					}
					k := KindNeutral
					if selected {
						k = KindPrimary
					}
					return t.Badge(gtx, k, badge)
				}))
		})
	})
}

// StatTile is a KPI card: icon, title, big value and a sub line.
func (t *Theme) StatTile(gtx C, ic *widget.Icon, k Kind, title, value, sub string) D {
	fg, soft := t.KindColors(k)
	return Box{Bg: t.P.Surface, Border: t.P.Border, Radius: 12, Expand: true,
		Inset: layout.Inset{Top: 16, Bottom: 16, Left: 18, Right: 18}}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return Box{Bg: soft, Radius: 8, Inset: layout.UniformInset(7)}.Layout(gtx, IconW(ic, 18, fg))
					}),
					HSpace(10),
					layout.Flexed(1, func(gtx C) D {
						l := t.Label(13, title, t.P.TextMuted, font.Medium)
						l.MaxLines = 1
						return l.Layout(gtx)
					}))
			}),
			VSpace(14),
			layout.Rigid(func(gtx C) D {
				l := t.Label(24, value, t.P.Text, font.SemiBold)
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			VSpace(2),
			layout.Rigid(func(gtx C) D {
				l := t.Caption(sub)
				l.MaxLines = 1
				return l.Layout(gtx)
			}))
	})
}

// Banner is an inline alert with optional actions on the right.
func (t *Theme) Banner(gtx C, k Kind, title, body string, actions ...layout.FlexChild) D {
	fg, soft := t.KindColors(k)
	ic := IconInfo
	switch k {
	case KindWarning:
		ic = IconWarning
	case KindDanger:
		ic = IconError
	case KindSuccess:
		ic = IconCheck
	}
	return Box{Bg: soft, Border: WithAlpha(fg, 0x55), Radius: 10, Expand: true,
		Inset: layout.Inset{Top: 12, Bottom: 12, Left: 14, Right: 14}}.Layout(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(IconW(ic, 19, fg)),
			HSpace(10),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(t.Label(13.5, title, t.P.Text, font.SemiBold).Layout),
					layout.Rigid(func(gtx C) D {
						if body == "" {
							return D{}
						}
						return layout.Inset{Top: 2}.Layout(gtx, t.Label(13, body, t.P.TextMuted, font.Normal).Layout)
					}))
			}),
		}
		for _, a := range actions {
			children = append(children, HSpace(8), a)
		}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// KeyValue is a label/value line with a fixed key column.
func (t *Theme) KeyValue(gtx C, key string, keyWidth unit.Dp, value layout.Widget) D {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			w := gtx.Dp(keyWidth)
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
			l := t.Label(13, key, t.P.TextMuted, font.Normal)
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Flexed(1, value))
}

// EmptyState is a centered placeholder for empty lists.
func (t *Theme) EmptyState(gtx C, ic *widget.Icon, title, body string) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Inset{Top: 36, Bottom: 36, Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return Box{Bg: t.P.SurfaceAlt, Radius: 24, Inset: layout.UniformInset(12)}.Layout(gtx, IconW(ic, 24, t.P.TextSubtle))
			}),
			VSpace(12),
			layout.Rigid(func(gtx C) D {
				l := t.Label(14.5, title, t.P.Text, font.SemiBold)
				l.Alignment = text.Middle
				return l.Layout(gtx)
			}),
			VSpace(4),
			layout.Rigid(func(gtx C) D {
				l := t.Muted(body)
				l.Alignment = text.Middle
				return l.Layout(gtx)
			}))
	})
}

// TabItem is one tab of Tabs. Badge, when set, is shown next to the label.
type TabItem struct {
	Key, Label string
	Badge      string
	BadgeKind  Kind
}

// Tabs is an underlined tab bar bound to e.
func (t *Theme) Tabs(gtx C, e *widget.Enum, items []TabItem) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Stack{Alignment: layout.SW}.Layout(gtx,
		layout.Expanded(func(gtx C) D {
			h := max(1, gtx.Dp(1))
			sz := gtx.Constraints.Min
			paint.FillShape(gtx.Ops, t.P.Border, clip.Rect{Min: image.Pt(0, sz.Y-h), Max: sz}.Op())
			return D{Size: sz}
		}),
		layout.Stacked(func(gtx C) D {
			children := make([]layout.FlexChild, 0, len(items)*2)
			hk, hov := e.Hovered()
			for i, it := range items {
				if i > 0 {
					children = append(children, HSpace(24))
				}
				children = append(children, layout.Rigid(func(gtx C) D {
					return e.Layout(gtx, it.Key, func(gtx C) D {
						sel := e.Value == it.Key
						fg, w := t.P.TextMuted, font.Medium
						switch {
						case sel:
							fg, w = t.P.Text, font.SemiBold
						case hov && hk == it.Key:
							fg = t.P.Text
						}
						dims := layout.Inset{Top: 4, Bottom: 11}.Layout(gtx, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(t.Label(14, it.Label, fg, w).Layout),
								layout.Rigid(func(gtx C) D {
									if it.Badge == "" {
										return D{}
									}
									return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D { return t.Badge(gtx, it.BadgeKind, it.Badge) })
								}))
						})
						if sel {
							h := gtx.Dp(2)
							FillRRect(gtx, image.Rect(0, dims.Size.Y-h, dims.Size.X, dims.Size.Y), h/2, t.P.Primary)
						}
						defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
						pointer.CursorPointer.Add(gtx.Ops)
						return dims
					})
				}))
			}
			return layout.Flex{}.Layout(gtx, children...)
		}))
}

// Logo draws the app mark: a rounded square with a diagonal gradient and a
// white glyph.
func (t *Theme) Logo(gtx C, size unit.Dp, from, to color.NRGBA, glyph *widget.Icon) D {
	s := gtx.Dp(size)
	rect := image.Rect(0, 0, s, s)
	area := clip.UniformRRect(rect, s*28/100).Push(gtx.Ops)
	paint.LinearGradientOp{Stop1: f32.Pt(0, 0), Color1: from, Stop2: f32.Pt(float32(s), float32(s)), Color2: to}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	area.Pop()
	g := s * 58 / 100
	if s < 32 { // small icons need a bolder glyph to stay legible
		g = s * 72 / 100
	}
	off := op.Offset(image.Pt((s-g)/2, (s-g)/2)).Push(gtx.Ops)
	gg := gtx
	gg.Constraints = layout.Exact(image.Pt(g, g))
	glyph.Layout(gg, rgb(0xFFFFFF))
	off.Pop()
	return D{Size: rect.Max}
}
