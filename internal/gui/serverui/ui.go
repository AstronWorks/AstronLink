package serverui

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
	PageClients
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
	{PageOverview, "개요", gui.IconDashboard, "서버 상태와 리스너, 최근 경고를 한눈에 봅니다."},
	{PageClients, "클라이언트", gui.IconPeople, "서버가 접속하는 클라이언트와 연결 상태입니다."},
	{PageTunnels, "터널", gui.IconSwap, "클라이언트가 등록한 공개 포트와 호스트 이름입니다."},
	{PageLogs, "로그", gui.IconLogs, "server.log 를 실시간으로 보여 줍니다."},
	{PageSettings, "설정", gui.IconSettings, "모든 설정을 여기서 바꿉니다. 저장하면 server.yaml 에 기록됩니다."},
	{PageAbout, "정보", gui.IconInfo, "버전과 파일 위치입니다."},
}

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
	saved   *config.ServerConfig
	loadErr string

	toast gui.Toast
	modal gui.Modal

	start, stop, restart widget.Clickable

	logs  *logview.View
	ov    overview
	cl    clientsPage
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
	u := &UI{th: th, be: be}
	u.logs = logview.New(be.ReadLog)
	u.reload()
	return u
}

// reload re-reads the saved configuration and resets the settings form.
func (u *UI) reload() {
	cfg, err := u.be.LoadConfig()
	u.loadErr = ""
	if err != nil {
		u.loadErr = err.Error()
		cfg = config.DefaultServerConfig()
	}
	u.saved = cfg
	u.set = newSettingsForm(cfg, u.be.Autostart())
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
func (u *UI) cfg() *config.ServerConfig {
	if u.state.Config != nil {
		return u.state.Config
	}
	return u.saved
}

func (u *UI) running() bool { return u.state.Phase == Running || u.state.Phase == Starting }

// Layout draws the whole window.
func (u *UI) Layout(gtx C) D {
	u.drain()
	if gtx.Now.Sub(u.stateAt) >= 700*time.Millisecond {
		u.state = u.be.State()
		u.stateAt = gtx.Now
	}
	u.logs.Refresh(gtx.Now)
	// Live data (uptime, connections, log tail) refreshes once a second.
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
	if u.start.Clicked(gtx) {
		u.be.Start()
		u.stateAt = time.Time{}
		u.toast.Show("서버를 시작합니다", gui.KindInfo)
	}
	if u.restart.Clicked(gtx) {
		u.be.Restart()
		u.stateAt = time.Time{}
		u.toast.Show("서버를 다시 시작합니다", gui.KindInfo)
	}
	if u.stop.Clicked(gtx) {
		body := "클라이언트에 접속하지 않으며, 트레이 메뉴나 [시작]으로 다시 실행할 수 있습니다."
		if n := len(u.state.Clients); n > 0 {
			body = fmt.Sprintf("연결된 클라이언트 %d개와의 연결이 끊어집니다. ", n) + body
		}
		u.modal.Show("서버를 중지할까요?", body, "중지", true, func() {
			u.be.Stop()
			u.stateAt = time.Time{}
			u.toast.Show("서버를 중지했습니다", gui.KindNeutral)
		})
	}
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
			case PageClients:
				if n := len(u.state.Clients); n > 0 {
					badge = strconv.Itoa(n)
				}
			case PageTunnels:
				if n := len(u.state.Tunnels); n > 0 {
					badge = strconv.Itoa(n)
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
				return layout.Inset{Left: 12}.Layout(gtx, t.Caption(u.be.Info().Version).Layout)
			}))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (u *UI) brand(gtx C) D {
	t := u.th
	return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return t.Logo(gtx, 36, gui.RGB(0x6366F1), gui.RGB(0x9333EA), gui.IconDNS)
			}),
			gui.HSpace(12),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(t.Label(15.5, "Astron Link", t.P.Text, font.SemiBold).Layout),
					layout.Rigid(t.Caption("서버 관리 콘솔").Layout))
			}))
	})
}

func phaseView(p Phase) (gui.Kind, string) {
	switch p {
	case Running:
		return gui.KindSuccess, "실행 중"
	case Starting:
		return gui.KindInfo, "시작하는 중"
	case Failed:
		return gui.KindDanger, "시작 실패"
	}
	return gui.KindNeutral, "중지됨"
}

func (u *UI) statusCard(gtx C) D {
	t := u.th
	k, label := phaseView(u.state.Phase)
	sub := "트레이 메뉴나 [시작]으로 실행합니다"
	switch u.state.Phase {
	case Running:
		sub = "가동 " + gui.HumanDuration(gtx.Now.Sub(u.state.StartedAt))
	case Starting:
		sub = "리스너를 여는 중입니다"
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
			case PageClients:
				return u.cl.layout(gtx, u)
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
			layout.Rigid(u.serverControls))
	})
}

func (u *UI) serverControls(gtx C) D {
	t := u.th
	if !u.running() {
		return t.Btn(&u.start, gui.BtnPrimary, "서버 시작", gui.IconPlay).Layout(gtx)
	}
	return layout.Flex{}.Layout(gtx,
		layout.Rigid(t.Btn(&u.restart, gui.BtnSecondary, "재시작", gui.IconRefresh).Layout),
		gui.HSpace(8),
		layout.Rigid(t.Btn(&u.stop, gui.BtnSoftDanger, "중지", gui.IconStop).Layout))
}

// scroll lays out items as a padded, scrollable page body.
func (u *UI) scroll(gtx C, l *widget.List, items ...layout.Widget) D {
	return gui.Scroll(gtx, u.th, l, items...)
}
