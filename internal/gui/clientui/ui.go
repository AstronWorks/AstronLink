package clientui

import (
	"fmt"
	"image"
	"strconv"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/gui"
	"github.com/AstronWorks/AstronLink/internal/gui/logview"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// Page identifies a sidebar page.
type Page int

const (
	PageOverview Page = iota
	PageTunnels
	PageLogs
	PageSettings
	PageAbout
	pageCount
)

var navEntries = []struct {
	page  Page
	label string
	icon  *widget.Icon
	sub   string
}{
	{PageOverview, "개요", gui.IconDashboard, "서버 연결, 로컬 프록시, VPN 상태를 한눈에 봅니다."},
	{PageTunnels, "터널", gui.IconSwap, "서버에 등록하는 터널과 포트 포워드입니다."},
	{PageLogs, "로그", gui.IconLogs, "winclient.log 를 실시간으로 보여 줍니다."},
	{PageSettings, "설정", gui.IconSettings, "모든 설정을 여기서 바꿉니다. 저장하면 config.yaml 에 기록됩니다."},
	{PageAbout, "정보", gui.IconInfo, "버전과 파일 위치입니다."},
}

// Brand colors of the client mark.
var (
	brandFrom = gui.RGB(0x0EA5E9)
	brandTo   = gui.RGB(0x14B8A6)
)

// UI is the console. Lay it out from the window goroutine only; other
// goroutines reach it through Do and Show.
type UI struct {
	th *gui.Theme
	be Backend

	page  Page
	nav   [pageCount]widget.Clickable
	theme widget.Clickable

	state   State
	stateAt time.Time
	info    Info
	saved   *config.ClientConfig
	loadErr string

	toast gui.Toast
	modal gui.Modal

	connect, disconnect, reconnect widget.Clickable

	logs  *logview.View
	ov    overview
	tn    tunnelsPage
	set   *settingsForm
	about aboutPage

	mu   sync.Mutex
	reqs []func(*UI)

	// OnTheme is called after the user switches the color scheme.
	OnTheme func(dark bool)
}

// New returns a console over be.
func New(th *gui.Theme, be Backend) *UI {
	u := &UI{th: th, be: be, info: be.Info()}
	u.logs = logview.New(be.ReadLog)
	u.reload()
	return u
}

func (u *UI) reload() {
	cfg, err := u.be.LoadConfig()
	u.loadErr = ""
	if err != nil {
		u.loadErr = err.Error()
		cfg = config.DefaultClientConfig()
	}
	u.saved = cfg
	u.set = newSettingsForm(cfg, u.be.Autostart(), u.be.ContextMenu())
}

// Do queues fn to run on the UI goroutine before the next frame.
func (u *UI) Do(fn func(*UI)) {
	u.mu.Lock()
	u.reqs = append(u.reqs, fn)
	u.mu.Unlock()
}

// Show switches to page p.
func (u *UI) Show(p Page) { u.Do(func(u *UI) { u.page = p }) }

func (u *UI) drain() {
	u.mu.Lock()
	reqs := u.reqs
	u.reqs = nil
	u.mu.Unlock()
	for _, fn := range reqs {
		fn(u)
	}
}

// cfg is the configuration to describe: the running one, else the saved one.
func (u *UI) cfg() *config.ClientConfig {
	if u.state.Config != nil {
		return u.state.Config
	}
	return u.saved
}

func (u *UI) active() bool { return u.state.Phase == Waiting || u.state.Phase == Connected }

// Layout draws the whole window.
func (u *UI) Layout(gtx C) D {
	u.drain()
	if gtx.Now.Sub(u.stateAt) >= 700*time.Millisecond {
		u.state = u.be.State()
		u.stateAt = gtx.Now
	}
	u.logs.Refresh(gtx.Now)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(time.Second)})

	for _, e := range navEntries {
		if u.nav[e.page].Clicked(gtx) {
			u.page = e.page
		}
	}
	if u.theme.Clicked(gtx) {
		u.th.SetDark(!u.th.Dark)
		if u.OnTheme != nil {
			u.OnTheme(u.th.Dark)
		}
	}
	u.handleControls(gtx)

	paint.Fill(gtx.Ops, u.th.P.Bg)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(u.sidebar),
				layout.Flexed(1, u.content))
		}),
		layout.Expanded(func(gtx C) D { return u.toast.Layout(gtx, u.th) }),
		layout.Expanded(func(gtx C) D { return u.modal.Layout(gtx, u.th) }),
	)
}

func (u *UI) handleControls(gtx C) {
	if u.connect.Clicked(gtx) {
		u.be.Connect()
		u.stateAt = time.Time{}
		u.toast.Show("서버의 접속을 기다립니다", gui.KindInfo)
	}
	if u.reconnect.Clicked(gtx) {
		u.be.Reconnect()
		u.stateAt = time.Time{}
		u.toast.Show("다시 연결합니다", gui.KindInfo)
	}
	if u.disconnect.Clicked(gtx) {
		body := "서버의 접속을 더 받지 않으며, 로컬 프록시와 VPN 도 멈춥니다."
		if n := len(u.state.Servers); n > 0 {
			body = fmt.Sprintf("연결된 서버 %d개와의 연결이 끊어집니다. ", n) + body
		}
		u.modal.Show("연결을 해제할까요?", body, "연결 해제", true, func() {
			u.be.Disconnect()
			u.stateAt = time.Time{}
			u.toast.Show("연결을 해제했습니다", gui.KindNeutral)
		})
	}
}

func phaseView(p Phase) (gui.Kind, string) {
	switch p {
	case Connected:
		return gui.KindSuccess, "연결됨"
	case Waiting:
		return gui.KindInfo, "서버 대기 중"
	case Failed:
		return gui.KindDanger, "시작 실패"
	}
	return gui.KindNeutral, "연결 안 됨"
}

func (u *UI) sidebar(gtx C) D {
	t := u.th
	w := gtx.Dp(244)
	size := image.Pt(w, gtx.Constraints.Max.Y)
	gtx.Constraints = layout.Exact(size)
	paint.FillShape(gtx.Ops, t.P.Sidebar, clip.Rect{Max: size}.Op())
	paint.FillShape(gtx.Ops, t.P.Border, clip.Rect{Min: image.Pt(w-max(1, gtx.Dp(1)), 0), Max: size}.Op())
	return layout.Inset{Top: 20, Bottom: 16, Left: 14, Right: 15}.Layout(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(u.brand),
			gui.VSpace(20),
			layout.Rigid(u.statusCard),
			gui.VSpace(18),
		}
		for _, e := range navEntries {
			badge := ""
			switch e.page {
			case PageTunnels:
				if c := u.cfg(); len(c.Tunnels)+len(c.PortForwards) > 0 {
					badge = strconv.Itoa(len(c.Tunnels) + len(c.PortForwards))
				}
			case PageSettings:
				if u.set.dirty {
					badge = "변경됨"
				}
			}
			children = append(children,
				layout.Rigid(func(gtx C) D {
					return t.NavItem(gtx, &u.nav[e.page], e.icon, e.label, u.page == e.page, badge)
				}),
				gui.VSpace(2))
		}
		children = append(children,
			layout.Flexed(1, layout.Spacer{}.Layout),
			layout.Rigid(func(gtx C) D {
				label, ic := "다크 모드", gui.IconMoon
				if t.Dark {
					label, ic = "라이트 모드", gui.IconSun
				}
				return t.NavItem(gtx, &u.theme, ic, label, false, "")
			}),
			gui.VSpace(10),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 12}.Layout(gtx, t.Caption(u.info.Version).Layout)
			}))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (u *UI) brand(gtx C) D {
	t := u.th
	return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return t.Logo(gtx, 36, brandFrom, brandTo, gui.IconComputer) }),
			gui.HSpace(12),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(t.Label(15.5, "Astron Link", t.P.Text, font.SemiBold).Layout),
					layout.Rigid(t.Caption("클라이언트").Layout))
			}))
	})
}

func (u *UI) statusCard(gtx C) D {
	t := u.th
	k, label := phaseView(u.state.Phase)
	sub := "[연결]을 누르면 서버의 접속을 기다립니다"
	switch u.state.Phase {
	case Connected:
		sub = fmt.Sprintf("서버 %d개 · %s", len(u.state.Servers), u.cfg().Name)
		if u.state.VPN.Active {
			sub += " · VPN"
		}
	case Waiting:
		sub = u.cfg().ListenAddr + " 에서 대기 · " + gui.HumanDuration(gtx.Now.Sub(u.state.Since))
	case Failed:
		sub = u.state.Err
	}
	return gui.Box{Bg: t.P.Surface, Border: t.P.Border, Radius: 10, Inset: layout.UniformInset(12), Expand: true}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return t.StatusPill(gtx, k, label) }),
			gui.VSpace(8),
			layout.Rigid(func(gtx C) D {
				l := t.Caption(sub)
				l.MaxLines = 2
				return l.Layout(gtx)
			}))
	})
}

func (u *UI) content(gtx C) D {
	e := navEntries[u.page]
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.header(gtx, e.label, e.sub) }),
		layout.Flexed(1, func(gtx C) D {
			switch u.page {
			case PageTunnels:
				return u.tn.layout(gtx, u)
			case PageLogs:
				return layout.Inset{Left: 28, Right: 28, Bottom: 24}.Layout(gtx, func(gtx C) D {
					return u.logs.Layout(gtx, u.th)
				})
			case PageSettings:
				return u.set.layout(gtx, u)
			case PageAbout:
				return u.about.layout(gtx, u)
			}
			return u.ov.layout(gtx, u)
		}))
}

func (u *UI) header(gtx C, title, sub string) D {
	t := u.th
	return layout.Inset{Top: 26, Bottom: 20, Left: 28, Right: 28}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(t.Title(title).Layout),
					gui.VSpace(4),
					layout.Rigid(t.Muted(sub).Layout))
			}),
			layout.Rigid(u.controls))
	})
}

func (u *UI) controls(gtx C) D {
	t := u.th
	if !u.active() {
		return t.Btn(&u.connect, gui.BtnPrimary, "연결", gui.IconPlay).Layout(gtx)
	}
	return layout.Flex{}.Layout(gtx,
		layout.Rigid(t.Btn(&u.reconnect, gui.BtnSecondary, "재연결", gui.IconRefresh).Layout),
		gui.HSpace(8),
		layout.Rigid(t.Btn(&u.disconnect, gui.BtnSoftDanger, "연결 해제", gui.IconStop).Layout))
}

func (u *UI) scroll(gtx C, l *widget.List, items ...layout.Widget) D {
	return gui.Scroll(gtx, u.th, l, items...)
}
