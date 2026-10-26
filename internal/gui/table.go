package gui

import (
	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

// Column describes one table column. Weight shares the width left after the
// fixed (Width > 0) columns.
type Column struct {
	Title  string
	Weight float32
	Width  unit.Dp
}

// Table renders a header and rows inside a flush card. Rows are not
// virtualized; it is meant for tens of rows.
type Table struct {
	hovers []gesture.Hover
}

// Cell renders one cell of row i, column j.
type Cell func(gtx C, row, col int) D

// Layout draws rows rows using cell. empty is shown when rows == 0.
func (tb *Table) Layout(gtx C, t *Theme, cols []Column, rows int, cell Cell, empty layout.Widget) D {
	for len(tb.hovers) < rows {
		tb.hovers = append(tb.hovers, gesture.Hover{})
	}
	return t.CardFlush(gtx, func(gtx C) D {
		children := make([]layout.FlexChild, 0, 2+rows*2)
		children = append(children, layout.Rigid(func(gtx C) D {
			return Box{Bg: t.P.SurfaceAlt, Radius: 11, Expand: true, Inset: layout.Inset{Top: 10, Bottom: 10, Left: 18, Right: 18}}.Layout(gtx, func(gtx C) D {
				return tb.row(gtx, cols, func(gtx C, j int) D {
					l := t.Label(12, cols[j].Title, t.P.TextMuted, font.SemiBold)
					l.MaxLines = 1
					return l.Layout(gtx)
				})
			})
		}))
		if rows == 0 && empty != nil {
			children = append(children, layout.Rigid(empty))
		}
		for i := 0; i < rows; i++ {
			if i > 0 {
				children = append(children, layout.Rigid(t.Divider))
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				h := &tb.hovers[i]
				hovered := h.Update(gtx.Source)
				box := Box{Expand: true, Inset: layout.Inset{Top: 11, Bottom: 11, Left: 18, Right: 18}}
				if hovered {
					box.Bg = t.P.Hover
				}
				// The hover area wraps the row so the cells' own handlers
				// (nested areas) keep receiving clicks.
				m := op.Record(gtx.Ops)
				dims := box.Layout(gtx, func(gtx C) D {
					return tb.row(gtx, cols, func(gtx C, j int) D { return cell(gtx, i, j) })
				})
				call := m.Stop()
				area := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
				h.Add(gtx.Ops)
				call.Add(gtx.Ops)
				area.Pop()
				return dims
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (tb *Table) row(gtx C, cols []Column, cell func(gtx C, j int) D) D {
	children := make([]layout.FlexChild, len(cols))
	for j, c := range cols {
		w := func(gtx C) D {
			return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D { return cell(gtx, j) })
		}
		if c.Width > 0 {
			children[j] = layout.Rigid(func(gtx C) D {
				px := gtx.Dp(c.Width)
				gtx.Constraints.Min.X, gtx.Constraints.Max.X = px, px
				d := w(gtx)
				d.Size.X = px
				return d
			})
			continue
		}
		weight := c.Weight
		if weight <= 0 {
			weight = 1
		}
		// Flex places the next column after the size a cell returns, so
		// pad every cell to its share to keep columns aligned with the header.
		children[j] = layout.Flexed(weight, func(gtx C) D {
			d := w(gtx)
			d.Size.X = gtx.Constraints.Min.X
			return d
		})
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}
