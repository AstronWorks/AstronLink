package gui

import (
	"fmt"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Scroll lays out items as a padded, scrollable page body.
func Scroll(gtx C, t *Theme, l *widget.List, items ...layout.Widget) D {
	l.Axis = layout.Vertical
	ls := material.List(t.Theme, l)
	ls.Indicator.Color = WithAlpha(t.P.TextSubtle, 0x70)
	ls.Indicator.HoverColor = t.P.TextSubtle
	return ls.Layout(gtx, len(items), func(gtx C, i int) D {
		in := layout.Inset{Left: 28, Right: 28, Bottom: 16}
		if i == len(items)-1 {
			in.Bottom = 28
		}
		return in.Layout(gtx, items[i])
	})
}

// Grid lays out ws in equal columns that wrap below minWidth each.
func Grid(gtx C, minWidth, gap unit.Dp, ws ...layout.Widget) D {
	if len(ws) == 0 {
		return D{}
	}
	cols := (gtx.Constraints.Max.X + gtx.Dp(gap)) / (gtx.Dp(minWidth) + gtx.Dp(gap))
	cols = max(1, min(cols, len(ws)))
	var rows []layout.FlexChild
	for r := 0; r < len(ws); r += cols {
		if r > 0 {
			rows = append(rows, VSpace(gap))
		}
		row := make([]layout.FlexChild, 0, cols*2)
		for c := 0; c < cols; c++ {
			if c > 0 {
				row = append(row, HSpace(gap))
			}
			w := layout.Widget(layout.Spacer{}.Layout)
			if r+c < len(ws) {
				w = ws[r+c]
			}
			row = append(row, layout.Flexed(1, w))
		}
		rows = append(rows, layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx, row...)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

// HumanDuration formats d compactly in Korean ("2시간 5분").
func HumanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	days := int(d / (24 * time.Hour))
	h := int(d/time.Hour) % 24
	m := int(d/time.Minute) % 60
	s := int(d/time.Second) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%d일 %d시간", days, h)
	case h > 0:
		return fmt.Sprintf("%d시간 %d분", h, m)
	case m > 0:
		return fmt.Sprintf("%d분 %d초", m, s)
	}
	return fmt.Sprintf("%d초", s)
}

// FormCard is a card with a heading and divider-separated rows.
func FormCard(t *Theme, title, desc string, rows ...layout.Widget) layout.Widget {
	return func(gtx C) D {
		return t.Card(gtx, func(gtx C) D {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx C) D { return t.SectionTitle(gtx, title, desc) }),
				VSpace(8),
			}
			for _, r := range rows {
				children = append(children, layout.Rigid(t.Divider), layout.Rigid(r))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
}

// Labeled stacks a small label, the control and its error or help text.
func Labeled(t *Theme, label, help, errText string, w layout.Widget) layout.Widget {
	return func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(t.Label(12.5, label, t.P.TextMuted, font.Medium).Layout),
			VSpace(6),
			layout.Rigid(w),
			layout.Rigid(func(gtx C) D {
				switch {
				case errText != "":
					return layout.Inset{Top: 5}.Layout(gtx, t.Label(12, errText, t.P.Danger, font.Medium).Layout)
				case help != "":
					return layout.Inset{Top: 5}.Layout(gtx, t.Caption(help).Layout)
				}
				return D{}
			}))
	}
}

// CardHeader is a card title with an optional action on the right.
func CardHeader(t *Theme, title string, action layout.Widget) layout.Widget {
	return func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, t.Heading(title).Layout),
			layout.Rigid(func(gtx C) D {
				if action == nil {
					return D{}
				}
				return action(gtx)
			}))
	}
}

// KV is a padded KeyValue row.
func KV(t *Theme, key string, keyWidth unit.Dp, w layout.Widget) layout.Widget {
	return func(gtx C) D {
		return layout.Inset{Top: 7, Bottom: 7}.Layout(gtx, func(gtx C) D { return t.KeyValue(gtx, key, keyWidth, w) })
	}
}

// OnOffBadge shows one of two badges depending on on.
func OnOffBadge(t *Theme, on bool, onText, offText string, onKind, offKind Kind) layout.Widget {
	return func(gtx C) D {
		if on {
			return t.Badge(gtx, onKind, onText)
		}
		return t.Badge(gtx, offKind, offText)
	}
}

// IconAction is a compact ghost button for inline field actions.
func IconAction(t *Theme, clk *widget.Clickable, ic *widget.Icon, label string) layout.FlexChild {
	return layout.Rigid(t.Btn(clk, BtnGhost, label, ic).WithSmall().Layout)
}
