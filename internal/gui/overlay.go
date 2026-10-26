package gui

import (
	"io"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
)

// CopyText puts s on the system clipboard.
func CopyText(gtx C, s string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
}

// Toast is a transient message shown at the bottom of the window.
type Toast struct {
	msg   string
	kind  Kind
	until time.Time
}

// Show displays msg for a few seconds.
func (ts *Toast) Show(msg string, k Kind) {
	ts.msg, ts.kind, ts.until = msg, k, time.Now().Add(3*time.Second)
}

// Layout draws the toast anchored to the bottom center of the constraints.
func (ts *Toast) Layout(gtx C, t *Theme) D {
	if ts.msg == "" {
		return D{}
	}
	if !gtx.Now.Before(ts.until) {
		ts.msg = ""
		return D{}
	}
	gtx.Execute(op.InvalidateCmd{At: ts.until})
	// The toast uses the inverse surface, so take the accent from the
	// opposite palette to keep the icon legible.
	inv := DarkPalette()
	if t.Dark {
		inv = LightPalette()
	}
	ic, fg := IconInfo, inv.Info
	switch ts.kind {
	case KindSuccess:
		ic, fg = IconCheck, inv.Success
	case KindWarning:
		ic, fg = IconWarning, inv.Warning
	case KindDanger:
		ic, fg = IconError, inv.Danger
	}
	gtx.Constraints.Min = gtx.Constraints.Max
	return layout.S.Layout(gtx, func(gtx C) D {
		return layout.Inset{Bottom: 28}.Layout(gtx, func(gtx C) D {
			return Box{Bg: t.P.Inverse, Radius: 10, Inset: layout.Inset{Top: 11, Bottom: 11, Left: 14, Right: 18}}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(IconW(ic, 18, fg)),
					HSpace(10),
					layout.Rigid(t.Label(13.5, ts.msg, t.P.OnInverse, font.Medium).Layout))
			})
		})
	})
}

// Modal is a confirmation dialog over a dimmed window.
type Modal struct {
	visible   bool
	title     string
	body      string
	confirmBy string
	danger    bool
	onConfirm func()

	confirm, cancel, scrim, panel widget.Clickable
}

// Show opens the dialog; fn runs when the user confirms.
func (m *Modal) Show(title, body, confirmLabel string, danger bool, fn func()) {
	m.visible, m.title, m.body, m.confirmBy, m.danger, m.onConfirm = true, title, body, confirmLabel, danger, fn
}

// Visible reports whether the dialog is open.
func (m *Modal) Visible() bool { return m.visible }

// Layout draws the dialog when open. It must be laid out above the content.
func (m *Modal) Layout(gtx C, t *Theme) D {
	if !m.visible {
		return D{}
	}
	if m.cancel.Clicked(gtx) || m.scrim.Clicked(gtx) {
		m.visible = false
	}
	if m.confirm.Clicked(gtx) {
		m.visible = false
		if m.onConfirm != nil {
			m.onConfirm()
		}
	}
	m.panel.Clicked(gtx) // swallow clicks on the panel itself
	sz := gtx.Constraints.Max
	m.scrim.Layout(gtx, func(gtx C) D {
		paint.FillShape(gtx.Ops, t.P.Scrim, clip.Rect{Max: sz}.Op())
		return D{Size: sz}
	})
	gtx.Constraints.Min = sz
	return layout.Center.Layout(gtx, func(gtx C) D {
		w := min(gtx.Dp(440), sz.X-gtx.Dp(32))
		gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
		return m.panel.Layout(gtx, func(gtx C) D {
			return Box{Bg: t.P.Surface, Border: t.P.Border, Radius: 14, Inset: layout.UniformInset(24), Expand: true}.Layout(gtx, func(gtx C) D {
				kind := BtnPrimary
				if m.danger {
					kind = BtnDanger
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(t.Label(17, m.title, t.P.Text, font.SemiBold).Layout),
					VSpace(8),
					layout.Rigid(t.Label(13.5, m.body, t.P.TextMuted, font.Normal).Layout),
					VSpace(22),
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.E.Layout(gtx, func(gtx C) D {
							return layout.Flex{}.Layout(gtx,
								layout.Rigid(t.Btn(&m.cancel, BtnSecondary, "취소", nil).Layout),
								HSpace(8),
								layout.Rigid(t.Btn(&m.confirm, kind, m.confirmBy, nil).Layout))
						})
					}))
			})
		})
	})
}

// TextField is a single-line editor with an optional reveal toggle for
// secrets.
type TextField struct {
	widget.Editor
	Secret   bool
	revealed bool
	reveal   widget.Clickable
}

// NewTextField returns a single-line field.
func NewTextField(secret bool) *TextField {
	f := &TextField{Secret: secret}
	f.SingleLine = true
	return f
}

// Layout draws the field with extra trailing buttons.
func (f *TextField) Layout(gtx C, t *Theme, hint string, invalid, mono bool, extra ...layout.FlexChild) D {
	if f.reveal.Clicked(gtx) {
		f.revealed = !f.revealed
	}
	f.Mask = 0
	var trailing []layout.FlexChild
	if f.Secret {
		ic := IconVisible
		if f.revealed {
			ic = IconHidden
		} else {
			f.Mask = '•'
		}
		trailing = append(trailing, layout.Rigid(t.Btn(&f.reveal, BtnGhost, "", ic).WithSmall().Layout))
	}
	trailing = append(trailing, extra...)
	return t.Input(gtx, &f.Editor, InputStyle{Hint: hint, Invalid: invalid, Mono: mono, Trailing: trailing})
}

// Confirm presses the dialog's confirm button (keyboard shortcuts, tests).
func (m *Modal) Confirm() { m.confirm.Click() }
