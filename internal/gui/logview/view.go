package logview

import (
	"hash/crc32"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/AstronWorks/AstronLink/internal/gui"
)

const (
	readBytes    = 512 << 10
	maxLines     = 4000
	readInterval = 1500 * time.Millisecond
)

// Source returns up to max bytes from the end of the log.
type Source func(max int) ([]byte, error)

// View is a filterable, auto-scrolling log list fed by a Source.
type View struct {
	src     Source
	lines   []Line
	readErr string
	readAt  time.Time
	sum     uint32

	level    widget.Enum
	search   *gui.TextField
	follow   widget.Bool
	copyBtn  widget.Clickable
	list     widget.List
	filtered []int
	fLevel   string
	fQuery   string
	fCount   int
	viewH    int
}

// New returns a view reading from src.
func New(src Source) *View {
	v := &View{src: src, search: gui.NewTextField(false)}
	v.level.Value = "all"
	v.follow.Value = true
	v.list.Axis = layout.Vertical
	v.list.ScrollToEnd = true
	return v
}

// Refresh re-reads the log when the read interval has passed.
func (v *View) Refresh(now time.Time) {
	if now.Sub(v.readAt) < readInterval {
		return
	}
	v.readAt = now
	data, err := v.src(readBytes)
	if err != nil {
		v.readErr = err.Error()
		v.lines, v.sum = nil, 0
		return
	}
	v.readErr = ""
	sum := crc32.ChecksumIEEE(data)
	if sum == v.sum && v.lines != nil {
		return
	}
	v.sum = sum
	v.lines = Parse(data, len(data) >= readBytes, maxLines)
	v.fCount = -1 // force refilter
}

// Recent returns up to n of the newest lines at or above minRank, newest last.
func (v *View) Recent(n, minRank int) []Line {
	var out []Line
	for i := len(v.lines) - 1; i >= 0 && len(out) < n; i-- {
		if v.lines[i].Rank() >= minRank {
			out = append(out, v.lines[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (v *View) refilter() {
	q := strings.ToLower(strings.TrimSpace(v.search.Text()))
	if v.level.Value == v.fLevel && q == v.fQuery && v.fCount == len(v.lines) {
		return
	}
	v.fLevel, v.fQuery, v.fCount = v.level.Value, q, len(v.lines)
	min := map[string]int{"all": 0, "info": 1, "warn": 2, "error": 3}[v.level.Value]
	v.filtered = v.filtered[:0]
	for i, ln := range v.lines {
		if ln.Rank() < min {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(ln.Raw), q) {
			continue
		}
		v.filtered = append(v.filtered, i)
	}
}

// Layout draws the toolbar and the list, filling the constraints.
func (v *View) Layout(gtx gui.C, t *gui.Theme) gui.D {
	v.Refresh(gtx.Now)
	if v.follow.Update(gtx) && v.follow.Value {
		v.list.Position.BeforeEnd = false
	}
	// Anchor to the end only once the lines overflow the view; otherwise a
	// short log would sit at the bottom of an empty card.
	v.list.ScrollToEnd = v.follow.Value && v.list.Position.Length > v.viewH
	v.refilter()
	if v.copyBtn.Clicked(gtx) {
		var b strings.Builder
		from := max(0, len(v.filtered)-1000)
		for _, i := range v.filtered[from:] {
			b.WriteString(v.lines[i].Raw)
			b.WriteString("\r\n")
		}
		gui.CopyText(gtx, b.String())
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx gui.C) gui.D { return v.toolbar(gtx, t) }),
		gui.VSpace(12),
		layout.Flexed(1, func(gtx gui.C) gui.D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return t.CardFlush(gtx, func(gtx gui.C) gui.D {
				gtx.Constraints.Min = gtx.Constraints.Max
				switch {
				case v.readErr != "":
					return t.EmptyState(gtx, gui.IconError, "로그를 읽을 수 없습니다", v.readErr)
				case len(v.filtered) == 0:
					return t.EmptyState(gtx, gui.IconLogs, "표시할 로그가 없습니다", "필터를 바꾸거나 잠시 기다리십시오.")
				}
				ls := material.List(t.Theme, &v.list)
				ls.Indicator.Color = gui.WithAlpha(t.P.TextSubtle, 0x70)
				ls.Indicator.HoverColor = t.P.TextSubtle
				return layout.Inset{Top: 6, Bottom: 6}.Layout(gtx, func(gtx gui.C) gui.D {
					v.viewH = gtx.Constraints.Max.Y
					return ls.Layout(gtx, len(v.filtered), func(gtx gui.C, i int) gui.D {
						return v.line(gtx, t, v.lines[v.filtered[i]])
					})
				})
			})
		}))
}

func (v *View) toolbar(gtx gui.C, t *gui.Theme) gui.D {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx gui.C) gui.D {
			return t.Segmented(gtx, &v.level, []gui.SegOption{
				{Key: "all", Label: "전체"}, {Key: "info", Label: "정보 이상"},
				{Key: "warn", Label: "경고 이상"}, {Key: "error", Label: "오류"},
			})
		}),
		gui.HSpace(12),
		layout.Flexed(1, func(gtx gui.C) gui.D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(360))
			return v.search.Layout(gtx, t, "메시지·주소·오류 검색", false, false)
		}),
		gui.HSpace(12),
		layout.Rigid(func(gtx gui.C) gui.D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx gui.C) gui.D { return t.Switch(gtx, &v.follow) }),
				gui.HSpace(8),
				layout.Rigid(t.Label(13, "자동 스크롤", t.P.TextMuted, font.Medium).Layout))
		}),
		gui.HSpace(12),
		layout.Rigid(t.Btn(&v.copyBtn, gui.BtnSecondary, "복사", gui.IconCopy).Layout))
}

func (v *View) line(gtx gui.C, t *gui.Theme, ln Line) gui.D {
	return layout.Inset{Top: 5, Bottom: 5, Left: 16, Right: 16}.Layout(gtx, func(gtx gui.C) gui.D {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(fixed(112, func(gtx gui.C) gui.D {
				l := t.Code(ln.Time)
				l.TextSize, l.Color = 12, t.P.TextSubtle
				return layout.Inset{Top: 2}.Layout(gtx, l.Layout)
			})),
			layout.Rigid(fixed(64, func(gtx gui.C) gui.D {
				if ln.Level == "" {
					return gui.D{}
				}
				return t.Badge(gtx, levelKind(ln.Level), ln.Level)
			})),
			layout.Flexed(1, func(gtx gui.C) gui.D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(t.Label(13.5, ln.Msg, t.P.Text, font.Normal).Layout),
					layout.Rigid(func(gtx gui.C) gui.D {
						if ln.Attrs == "" {
							return gui.D{}
						}
						l := t.Code(ln.Attrs)
						l.TextSize, l.Color = 12, t.P.TextMuted
						return layout.Inset{Top: 2}.Layout(gtx, l.Layout)
					}))
			}))
	})
}

func fixed(w unit.Dp, wd layout.Widget) layout.Widget {
	return func(gtx gui.C) gui.D {
		px := gtx.Dp(w)
		gtx.Constraints.Min.X, gtx.Constraints.Max.X = px, px
		d := wd(gtx)
		d.Size.X = px
		return d
	}
}

func levelKind(level string) gui.Kind {
	switch level {
	case "WARN", "WARNING":
		return gui.KindWarning
	case "ERROR":
		return gui.KindDanger
	case "DEBUG":
		return gui.KindNeutral
	}
	return gui.KindInfo
}

// LineWidget renders ln compactly (for summaries outside the view).
func LineWidget(t *gui.Theme, ln Line) layout.Widget {
	return func(gtx gui.C) gui.D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(fixed(64, func(gtx gui.C) gui.D {
				if ln.Level == "" {
					return gui.D{}
				}
				return t.Badge(gtx, levelKind(ln.Level), ln.Level)
			})),
			layout.Flexed(1, func(gtx gui.C) gui.D {
				return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(func(gtx gui.C) gui.D {
						l := t.Label(13, ln.Msg, t.P.Text, font.Medium)
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
					layout.Flexed(1, func(gtx gui.C) gui.D {
						if ln.Attrs == "" {
							return gui.D{}
						}
						l := t.Code(ln.Attrs)
						l.TextSize, l.Color, l.MaxLines = 12, t.P.TextMuted, 1
						return layout.Inset{Left: 8}.Layout(gtx, l.Layout)
					}))
			}),
			gui.HSpace(12),
			layout.Rigid(func(gtx gui.C) gui.D {
				l := t.Code(ln.Time)
				l.TextSize, l.Color = 12, t.P.TextSubtle
				return l.Layout(gtx)
			}))
	}
}
