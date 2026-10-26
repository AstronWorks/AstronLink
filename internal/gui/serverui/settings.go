package serverui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"math"
	"reflect"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/gui"
)

// Settings tabs.
const (
	tabConn     = "conn"
	tabListen   = "listen"
	tabSecurity = "security"
	tabVPN      = "vpn"
	tabLimits   = "limits"
	tabGeneral  = "general"
)

var settingsTabs = []struct{ key, label string }{
	{tabConn, "연결"}, {tabListen, "리스너"}, {tabSecurity, "보안"},
	{tabVPN, "VPN"}, {tabLimits, "제한 · 파일 수신"}, {tabGeneral, "일반"},
}

func tabOf(field string) string {
	switch {
	case field == fAuthToken || strings.HasPrefix(field, "client."):
		return tabConn
	case field == fDataAddr || field == fHTTPAddr || field == fHTTPSAddr || field == fAdminAddr:
		return tabListen
	case field == fAdminToken || field == fClientCA:
		return tabSecurity
	case strings.HasPrefix(field, "vpn."):
		return tabVPN
	}
	return tabLimits
}

const (
	digits      = "0123456789"
	decimal     = "0123456789."
	defHTTP     = ":8080"
	defHTTPS    = ":8445"
	defAdmin    = ":9090"
	mib         = 1 << 20
	tokenLength = 32
)

// newToken returns a random 256-bit hex token.
func newToken() string {
	b := make([]byte, tokenLength)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type clientRow struct {
	name, addr, token *gui.TextField
	del, gen          widget.Clickable
}

// settingsForm edits every functional ServerConfig field. Fields the console
// does not show are carried over from base unchanged.
type settingsForm struct {
	base *config.ServerConfig

	tab  widget.Enum
	list widget.List

	authToken         *gui.TextField
	genAuth, copyAuth widget.Clickable
	clients           []*clientRow
	addClientBtn      widget.Clickable

	dataAddr, httpAddr, httpsAddr, adminAddr *gui.TextField
	httpOn, httpsOn, adminOn                 widget.Bool

	insecure, allowPrivate widget.Bool
	clientCA               *gui.TextField
	adminToken             *gui.TextField
	genAdmin               widget.Clickable

	vpnOn                     widget.Bool
	vpnSubnet, vpnMTU, vpnDNS *gui.TextField

	rate, burst, concurrent *gui.TextField

	ftOn                          widget.Bool
	ftAddr, ftDir, ftToken, ftMax *gui.TextField
	genFT                         widget.Clickable

	logLevel  widget.Enum
	autostart widget.Bool

	save, revert widget.Clickable

	// Computed each frame.
	errs     fieldErrors
	dirty    bool
	showErrs bool
}

func field(filter string) *gui.TextField {
	f := gui.NewTextField(false)
	f.Filter = filter
	return f
}

func secret() *gui.TextField { return gui.NewTextField(true) }

func newSettingsForm(cfg *config.ServerConfig, autostart bool) *settingsForm {
	f := &settingsForm{
		authToken: secret(),
		dataAddr:  field(""), httpAddr: field(""), httpsAddr: field(""), adminAddr: field(""),
		clientCA: field(""), adminToken: secret(),
		vpnSubnet: field(""), vpnMTU: field(digits), vpnDNS: field(""),
		rate: field(decimal), burst: field(digits), concurrent: field(digits),
		ftAddr: field(""), ftDir: field(""), ftToken: secret(), ftMax: field(decimal),
	}
	f.tab.Value = tabConn
	f.list.Axis = layout.Vertical
	f.autostart.Value = autostart
	f.load(cfg)
	return f
}

func cloneConfig(c *config.ServerConfig) *config.ServerConfig {
	cp := *c
	cp.Clients = append([]config.ClientTarget(nil), c.Clients...)
	cp.VPN.DNS = append([]string(nil), c.VPN.DNS...)
	return &cp
}

// normalize makes equivalent configs compare equal (nil vs empty slices,
// level spelling).
func normalize(c *config.ServerConfig) *config.ServerConfig {
	c = cloneConfig(c)
	if len(c.Clients) == 0 {
		c.Clients = nil
	}
	if len(c.VPN.DNS) == 0 {
		c.VPN.DNS = nil
	}
	c.LogLevel = levelOf(c.LogLevel)
	return c
}

func setOptional(on *widget.Bool, ed *gui.TextField, v, def string) {
	on.Value = v != ""
	if v == "" {
		v = def
	}
	ed.SetText(v)
}

func formatFloat(v float64) string {
	if v == 0 {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// load resets every field from cfg.
func (f *settingsForm) load(cfg *config.ServerConfig) {
	f.base = normalize(cfg)
	c := f.base
	f.authToken.SetText(c.AuthToken)
	f.clients = nil
	for _, ct := range c.Clients {
		f.addClient(ct)
	}
	f.dataAddr.SetText(c.DataAddr)
	setOptional(&f.httpOn, f.httpAddr, c.HTTPAddr, defHTTP)
	setOptional(&f.httpsOn, f.httpsAddr, c.HTTPSAddr, defHTTPS)
	setOptional(&f.adminOn, f.adminAddr, c.AdminAddr, defAdmin)
	f.insecure.Value = c.Insecure
	f.allowPrivate.Value = c.AllowPrivateTargets
	f.clientCA.SetText(c.ClientCACert)
	f.adminToken.SetText(c.AdminToken)
	f.vpnOn.Value = c.VPN.Enabled
	f.vpnSubnet.SetText(c.VPN.Subnet)
	mtu := ""
	if c.VPN.MTU != 0 {
		mtu = strconv.Itoa(c.VPN.MTU)
	}
	f.vpnMTU.SetText(mtu)
	f.vpnDNS.SetText(strings.Join(c.VPN.DNS, ", "))
	f.rate.SetText(formatFloat(c.MaxConnRate))
	f.burst.SetText(strconv.Itoa(c.MaxConnBurst))
	f.concurrent.SetText(strconv.FormatInt(c.MaxConcurrent, 10))
	f.ftOn.Value = c.FileTransfer.Enabled
	f.ftAddr.SetText(c.FileTransfer.ReceiveAddr)
	f.ftDir.SetText(c.FileTransfer.DropDir)
	f.ftToken.SetText(c.FileTransfer.Token)
	f.ftMax.SetText(formatFloat(float64(c.FileTransfer.MaxFileSize) / mib))
	f.logLevel.Value = c.LogLevel
	f.showErrs = false
}

func (f *settingsForm) addClient(ct config.ClientTarget) {
	r := &clientRow{name: field(""), addr: field(""), token: secret()}
	r.name.SetText(ct.Name)
	r.addr.SetText(ct.Address)
	r.token.SetText(ct.AuthToken)
	f.clients = append(f.clients, r)
}

func text(f *gui.TextField) string { return strings.TrimSpace(f.Text()) }

// collect builds the config the form describes and validates it.
func (f *settingsForm) collect() (*config.ServerConfig, fieldErrors) {
	cfg := cloneConfig(f.base)
	perr := fieldErrors{}
	parseInt := func(ed *gui.TextField, key string) int64 {
		s := text(ed)
		if s == "" {
			return 0
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			perr[key] = "정수를 입력하십시오"
		}
		return v
	}
	parseFloat := func(ed *gui.TextField, key string) float64 {
		s := text(ed)
		if s == "" {
			return 0
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			perr[key] = "숫자를 입력하십시오"
			return 0
		}
		return v
	}
	opt := func(on bool, ed *gui.TextField) string {
		if !on {
			return ""
		}
		return text(ed)
	}

	cfg.AuthToken = text(f.authToken)
	cfg.Clients = nil
	for _, r := range f.clients {
		cfg.Clients = append(cfg.Clients, config.ClientTarget{Name: text(r.name), Address: text(r.addr), AuthToken: text(r.token)})
	}
	cfg.DataAddr = text(f.dataAddr)
	cfg.HTTPAddr = opt(f.httpOn.Value, f.httpAddr)
	cfg.HTTPSAddr = opt(f.httpsOn.Value, f.httpsAddr)
	cfg.AdminAddr = opt(f.adminOn.Value, f.adminAddr)
	cfg.Insecure = f.insecure.Value
	cfg.AllowPrivateTargets = f.allowPrivate.Value
	cfg.ClientCACert = text(f.clientCA)
	cfg.AdminToken = text(f.adminToken)
	cfg.VPN.Enabled = f.vpnOn.Value
	cfg.VPN.Subnet = text(f.vpnSubnet)
	cfg.VPN.MTU = int(parseInt(f.vpnMTU, fVPNMTU))
	cfg.VPN.DNS = gui.SplitList(f.vpnDNS.Text())
	cfg.MaxConnRate = parseFloat(f.rate, fRate)
	cfg.MaxConnBurst = int(parseInt(f.burst, fBurst))
	cfg.MaxConcurrent = parseInt(f.concurrent, fConcurrent)
	cfg.FileTransfer.Enabled = f.ftOn.Value
	cfg.FileTransfer.ReceiveAddr = text(f.ftAddr)
	cfg.FileTransfer.DropDir = text(f.ftDir)
	cfg.FileTransfer.Token = text(f.ftToken)
	cfg.FileTransfer.MaxFileSize = int64(math.Round(parseFloat(f.ftMax, fFTMax) * mib))
	cfg.LogLevel = levelOf(f.logLevel.Value)

	errs := validate(cfg)
	for k, v := range perr {
		errs[k] = v
	}
	return cfg, errs
}

// errFor returns the message to show under ed for key: only once the user
// tried to save or typed something, so a fresh empty row is not all red.
func (f *settingsForm) errFor(key string, ed *gui.TextField) string {
	msg := f.errs[key]
	if msg == "" {
		return ""
	}
	if f.showErrs || ed == nil || text(ed) != "" {
		return msg
	}
	return ""
}

func (f *settingsForm) visibleErrs() map[string]int {
	counts := map[string]int{}
	for k := range f.errs {
		if f.showErrs || f.fieldText(k) != "" {
			counts[tabOf(k)]++
		}
	}
	return counts
}

func (f *settingsForm) fieldText(key string) string {
	var i int
	switch {
	case key == fAuthToken:
		return text(f.authToken)
	case key == fDataAddr:
		return text(f.dataAddr)
	case key == fHTTPAddr:
		return text(f.httpAddr)
	case key == fHTTPSAddr:
		return text(f.httpsAddr)
	case key == fAdminAddr:
		return text(f.adminAddr)
	case key == fVPNSubnet:
		return text(f.vpnSubnet)
	case key == fVPNMTU:
		return text(f.vpnMTU)
	case key == fVPNDNS:
		return text(f.vpnDNS)
	case key == fFTAddr:
		return text(f.ftAddr)
	case key == fFTDir:
		return text(f.ftDir)
	case strings.HasPrefix(key, "client."):
		if _, err := fmt.Sscanf(key[len("client."):], "%d", &i); err == nil && i < len(f.clients) {
			r := f.clients[i]
			switch {
			case strings.HasSuffix(key, ".name"):
				return text(r.name)
			case strings.HasSuffix(key, ".address"):
				return text(r.addr)
			}
			return text(r.token)
		}
	}
	return "x" // numeric fields always hold text
}

// update handles the form's buttons and recomputes errors and dirtiness.
func (f *settingsForm) update(gtx C, u *UI) {
	if f.genAuth.Clicked(gtx) {
		f.authToken.SetText(newToken())
		u.toast.Show("새 토큰을 만들었습니다. 저장한 뒤 클라이언트에도 넣으십시오", gui.KindInfo)
	}
	if f.copyAuth.Clicked(gtx) {
		gui.CopyText(gtx, text(f.authToken))
		u.toast.Show("토큰을 복사했습니다", gui.KindSuccess)
	}
	if f.genAdmin.Clicked(gtx) {
		f.adminToken.SetText(newToken())
	}
	if f.genFT.Clicked(gtx) {
		f.ftToken.SetText(newToken())
	}
	if f.addClientBtn.Clicked(gtx) {
		f.addClient(config.ClientTarget{})
	}
	for i := 0; i < len(f.clients); i++ {
		r := f.clients[i]
		if r.gen.Clicked(gtx) {
			r.token.SetText(newToken())
		}
		if r.del.Clicked(gtx) {
			f.clients = append(f.clients[:i], f.clients[i+1:]...)
			i--
		}
	}
	// Re-enabling an optional listener with an empty address restores the
	// default.
	for _, o := range []struct {
		on  *widget.Bool
		ed  *gui.TextField
		def string
	}{{&f.httpOn, f.httpAddr, defHTTP}, {&f.httpsOn, f.httpsAddr, defHTTPS}, {&f.adminOn, f.adminAddr, defAdmin}} {
		if o.on.Update(gtx) && o.on.Value && text(o.ed) == "" {
			o.ed.SetText(o.def)
		}
	}
	if f.autostart.Update(gtx) {
		if err := u.be.SetAutostart(f.autostart.Value); err != nil {
			f.autostart.Value = !f.autostart.Value
			u.toast.Show("자동 실행을 바꾸지 못했습니다: "+err.Error(), gui.KindDanger)
		} else if f.autostart.Value {
			u.toast.Show("로그온 시 자동으로 실행합니다", gui.KindSuccess)
		} else {
			u.toast.Show("자동 실행을 껐습니다", gui.KindNeutral)
		}
	}

	if f.revert.Clicked(gtx) {
		f.load(u.saved)
		u.toast.Show("저장된 설정으로 되돌렸습니다", gui.KindNeutral)
	}

	cfg, errs := f.collect()
	f.errs = errs
	f.dirty = !reflect.DeepEqual(normalize(cfg), f.base)

	if f.save.Clicked(gtx) {
		if len(errs) > 0 {
			f.showErrs = true
			first := ""
			for _, tb := range settingsTabs {
				for k := range errs {
					if tabOf(k) == tb.key && first == "" {
						first = tb.key
					}
				}
			}
			f.tab.Value = first
			u.toast.Show(fmt.Sprintf("입력값 %d개를 확인하십시오", len(errs)), gui.KindDanger)
			return
		}
		title, body, confirm := "설정을 저장할까요?", "server.yaml 에 저장합니다. 서버가 중지되어 있으므로 다음 시작부터 적용됩니다.", "저장"
		if u.running() {
			title = "저장하고 서버를 다시 시작할까요?"
			body = "server.yaml 에 저장한 뒤 서버를 다시 시작합니다. 연결된 클라이언트는 잠시 끊겼다가 다시 연결됩니다."
			confirm = "저장 후 재시작"
		}
		u.modal.Show(title, body, confirm, false, func() {
			if err := u.be.SaveConfig(cfg); err != nil {
				u.toast.Show("저장하지 못했습니다: "+err.Error(), gui.KindDanger)
				return
			}
			u.saved = cloneConfig(cfg)
			f.load(cfg)
			f.dirty, f.errs = false, fieldErrors{}
			u.stateAt = u.stateAt.AddDate(-1, 0, 0)
			if u.running() {
				u.toast.Show("저장했습니다. 서버를 다시 시작합니다", gui.KindSuccess)
			} else {
				u.toast.Show("저장했습니다", gui.KindSuccess)
			}
		})
	}
}

func (f *settingsForm) layout(gtx C, u *UI) D {
	t := u.th
	f.update(gtx, u)
	counts := f.visibleErrs()
	items := make([]gui.TabItem, len(settingsTabs))
	for i, tb := range settingsTabs {
		items[i] = gui.TabItem{Key: tb.key, Label: tb.label}
		if n := counts[tb.key]; n > 0 {
			items[i].Badge, items[i].BadgeKind = strconv.Itoa(n), gui.KindDanger
		}
		if tb.key == tabConn && len(f.clients) > 0 && items[i].Badge == "" {
			items[i].Badge, items[i].BadgeKind = strconv.Itoa(len(f.clients)), gui.KindNeutral
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 28, Right: 28}.Layout(gtx, func(gtx C) D { return t.Tabs(gtx, &f.tab, items) })
		}),
		gui.VSpace(18),
		layout.Flexed(1, func(gtx C) D {
			return u.scroll(gtx, &f.list, f.tabContent(u)...)
		}),
		layout.Rigid(func(gtx C) D { return f.saveBar(gtx, u) }))
}

func (f *settingsForm) tabContent(u *UI) []layout.Widget {
	switch f.tab.Value {
	case tabListen:
		return []layout.Widget{f.listenCard(u)}
	case tabSecurity:
		return []layout.Widget{f.securityCards(u)}
	case tabVPN:
		return []layout.Widget{f.vpnCard(u)}
	case tabLimits:
		return []layout.Widget{f.limitsCard(u), f.fileCard(u)}
	case tabGeneral:
		return []layout.Widget{f.generalCard(u)}
	}
	return []layout.Widget{f.tokenCard(u), f.clientsCard(u)}
}

func (f *settingsForm) input(t *gui.Theme, ed *gui.TextField, key, hint string, mono bool, extra ...layout.FlexChild) layout.Widget {
	return func(gtx C) D {
		return ed.Layout(gtx, t, hint, f.errFor(key, ed) != "", mono, extra...)
	}
}

func (f *settingsForm) row(t *gui.Theme, title, desc, key string, ed *gui.TextField, hint string, mono bool, extra ...layout.FlexChild) layout.Widget {
	return func(gtx C) D {
		return t.FormRow(gtx, title, desc, f.errFor(key, ed), f.input(t, ed, key, hint, mono, extra...))
	}
}

// toggledRow is an optional listener: a switch and the address it enables.
func (f *settingsForm) toggledRow(t *gui.Theme, title, desc, key string, on *widget.Bool, ed *gui.TextField, hint string) layout.Widget {
	return func(gtx C) D {
		return t.FormRow(gtx, title, desc, f.errFor(key, ed), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return t.Switch(gtx, on) }),
				gui.HSpace(12),
				layout.Flexed(1, func(gtx C) D {
					if !on.Value {
						gtx = gtx.Disabled()
					}
					return f.input(t, ed, key, hint, true)(gtx)
				}))
		})
	}
}

func (f *settingsForm) tokenCard(u *UI) layout.Widget {
	t := u.th
	return gui.FormCard(t, "인증 토큰", "서버가 클라이언트에 접속할 때 보내는 공유 비밀입니다. 클라이언트 설정의 auth_token 과 같아야 합니다.",
		f.row(t, "기본 토큰", "클라이언트별 토큰을 비워 두면 이 값을 보냅니다.", fAuthToken, f.authToken, "토큰을 입력하거나 생성하십시오", true,
			gui.IconAction(t, &f.copyAuth, gui.IconCopy, ""),
			gui.IconAction(t, &f.genAuth, gui.IconGenerate, "생성")))
}

func (f *settingsForm) clientsCard(u *UI) layout.Widget {
	t := u.th
	return func(gtx C) D {
		return t.Card(gtx, func(gtx C) D {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return t.SectionTitle(gtx, "클라이언트", "서버가 접속할 클라이언트입니다. 클라이언트 PC 에서 reversproxy-client 가 실행 중이어야 합니다.")
						}),
						gui.HSpace(16),
						layout.Rigid(t.Btn(&f.addClientBtn, gui.BtnSecondary, "추가", gui.IconAdd).Layout))
				}),
				gui.VSpace(14),
			}
			if len(f.clients) == 0 {
				children = append(children, layout.Rigid(func(gtx C) D {
					return t.EmptyState(gtx, gui.IconPeople, "등록된 클라이언트가 없습니다", "[추가]를 눌러 이름과 주소를 입력하십시오.")
				}))
			}
			for i, r := range f.clients {
				if i > 0 {
					children = append(children, gui.VSpace(12))
				}
				children = append(children, layout.Rigid(func(gtx C) D { return f.clientEditor(gtx, t, i, r) }))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
}

func (f *settingsForm) clientEditor(gtx C, t *gui.Theme, i int, r *clientRow) D {
	kName, kAddr, kTok := clientKey(fClientName, i), clientKey(fClientAddr, i), clientKey(fClientToken, i)
	title := text(r.name)
	if title == "" {
		title = fmt.Sprintf("클라이언트 %d", i+1)
	}
	return gui.Box{Bg: t.P.SurfaceAlt, Border: t.P.Border, Radius: 10, Expand: true, Inset: layout.UniformInset(16)}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(gui.IconW(gui.IconComputer, 18, t.P.TextMuted)),
					gui.HSpace(8),
					layout.Flexed(1, t.Label(14, title, t.P.Text, font.SemiBold).Layout),
					layout.Rigid(t.Btn(&r.del, gui.BtnGhost, "삭제", gui.IconDelete).WithSmall().Layout))
			}),
			gui.VSpace(12),
			layout.Rigid(func(gtx C) D {
				return gui.Grid(gtx, 240, 14,
					gui.Labeled(t, "이름", "클라이언트 설정의 name", f.errFor(kName, r.name), f.input(t, r.name, kName, "my-pc", false)),
					gui.Labeled(t, "주소", "클라이언트 IP:listen 포트", f.errFor(kAddr, r.addr), f.input(t, r.addr, kAddr, "192.168.1.10:8443", true)))
			}),
			gui.VSpace(12),
			layout.Rigid(gui.Labeled(t, "개별 토큰 (선택)", "비워 두면 기본 토큰을 보냅니다", f.errFor(kTok, r.token),
				f.input(t, r.token, kTok, "기본 토큰 사용", true, gui.IconAction(t, &r.gen, gui.IconGenerate, "생성")))))
	})
}

func (f *settingsForm) listenCard(u *UI) layout.Widget {
	t := u.th
	return gui.FormCard(t, "리스너 주소", "‘:포트’는 이 PC 의 모든 주소에서 받습니다. 외부에서 쓰려면 Windows 방화벽에서 포트를 허용하십시오.",
		f.row(t, "데이터 연결", "클라이언트가 터널 데이터 연결을 여는 주소입니다. 필수입니다.", fDataAddr, f.dataAddr, ":8444", true),
		f.toggledRow(t, "HTTP 프록시", "Host 헤더를 보고 HTTP 터널로 분배합니다.", fHTTPAddr, &f.httpOn, f.httpAddr, defHTTP),
		f.toggledRow(t, "HTTPS 프록시 (SNI)", "TLS SNI 를 보고 HTTPS 터널로 분배합니다.", fHTTPSAddr, &f.httpsOn, f.httpsAddr, defHTTPS),
		f.toggledRow(t, "관리 API", "JSON API 와 웹 대시보드입니다. ‘:9090’처럼 호스트를 생략하면 이 PC(127.0.0.1)에서만 접속됩니다.", fAdminAddr, &f.adminOn, f.adminAddr, defAdmin))
}

func (f *settingsForm) securityCards(u *UI) layout.Widget {
	t := u.th
	cert := gui.FormCard(t, "클라이언트 인증서", "",
		func(gtx C) D {
			return t.SwitchRow(gtx, "인증서 검증 생략 (insecure)",
				"클라이언트는 자체 서명 인증서를 씁니다. CA 인증서를 지정하지 않으면 켜 두어야 연결됩니다. 켜 두면 약한 토큰도 허용됩니다.", &f.insecure)
		},
		func(gtx C) D {
			if f.insecure.Value {
				gtx = gtx.Disabled()
			}
			return f.row(t, "클라이언트 CA 인증서", "검증할 때 쓸 CA 파일(PEM) 경로입니다. 비우면 Windows 루트 인증서를 씁니다.", fClientCA, f.clientCA, `C:\certs\client-ca.pem`, true)(gtx)
		})
	egress := gui.FormCard(t, "출구 정책", "",
		func(gtx C) D {
			return t.SwitchRow(gtx, "사설 · 루프백 주소 접근 허용",
				"SOCKS5 · HTTP CONNECT · 포트 포워드 · VPN 으로 이 서버의 LAN(192.168.x.x 등)과 127.0.0.1 에 접속하도록 허용합니다.", &f.allowPrivate)
		})
	admin := gui.FormCard(t, "관리 API 인증", "",
		f.row(t, "관리 토큰", "설정하면 /api 요청에 Authorization: Bearer 토큰이 필요합니다. 웹 대시보드는 토큰을 보내지 않으므로 데이터를 불러오지 못합니다.",
			fAdminToken, f.adminToken, "비우면 인증 없음", true, gui.IconAction(t, &f.genAdmin, gui.IconGenerate, "생성")))
	return func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(cert), gui.VSpace(16), layout.Rigid(egress), gui.VSpace(16), layout.Rigid(admin))
	}
}

func (f *settingsForm) vpnCard(u *UI) layout.Widget {
	t := u.th
	off := func(w layout.Widget) layout.Widget {
		return func(gtx C) D {
			if !f.vpnOn.Value {
				gtx = gtx.Disabled()
			}
			return w(gtx)
		}
	}
	return gui.FormCard(t, "VPN 모드", "클라이언트가 가상 네트워크 어댑터로 접속하면 이 서버가 트래픽을 대신 내보냅니다. 클라이언트가 요청할 때만 세션이 생깁니다.",
		func(gtx C) D {
			return t.SwitchRow(gtx, "VPN 허용", "끄면 클라이언트의 VPN 요청을 거절합니다.", &f.vpnOn)
		},
		off(f.row(t, "가상 네트워크", "첫 주소는 서버(게이트웨이), 둘째 주소는 클라이언트에 줍니다. IPv4, /30 이하.", fVPNSubnet, f.vpnSubnet, "10.77.0.0/24", true)),
		off(f.row(t, "MTU", "비우면 1500. 576–65535.", fVPNMTU, f.vpnMTU, "1500", true)),
		off(f.row(t, "DNS 서버", "쉼표로 구분합니다. 비우면 서버 내장 리졸버(게이트웨이 주소)가 이 서버 기준으로 이름을 풉니다.", fVPNDNS, f.vpnDNS, "비우면 내장 리졸버", true)))
}

func (f *settingsForm) limitsCard(u *UI) layout.Widget {
	t := u.th
	return gui.FormCard(t, "연결 제한", "HTTP · HTTPS 프록시 리스너에만 적용됩니다. 0 이면 제한하지 않습니다.",
		f.row(t, "IP 별 초당 연결 수", "", fRate, f.rate, "0", true),
		f.row(t, "IP 별 버스트", "0 이면 10 을 씁니다.", fBurst, f.burst, "0", true),
		f.row(t, "전체 동시 연결 상한", "", fConcurrent, f.concurrent, "0", true))
}

func (f *settingsForm) fileCard(u *UI) layout.Widget {
	t := u.th
	off := func(w layout.Widget) layout.Widget {
		return func(gtx C) D {
			if !f.ftOn.Value {
				gtx = gtx.Disabled()
			}
			return w(gtx)
		}
	}
	return gui.FormCard(t, "파일 수신", "피어가 터널로 보낸 파일을 이 PC 의 폴더에 저장합니다.",
		func(gtx C) D { return t.SwitchRow(gtx, "파일 수신 사용", "", &f.ftOn) },
		off(f.row(t, "수신 주소", "터널로 노출할 로컬 HTTP 주소입니다.", fFTAddr, f.ftAddr, "127.0.0.1:8089", true)),
		off(f.row(t, "저장 폴더", "상대 경로는 프로그램 폴더 기준입니다.", fFTDir, f.ftDir, "received", true)),
		off(f.row(t, "업로드 토큰", "비우면 인증 없이 받습니다.", "", f.ftToken, "비우면 인증 없음", true, gui.IconAction(t, &f.genFT, gui.IconGenerate, "생성"))),
		off(f.row(t, "최대 파일 크기 (MB)", "0 이면 제한하지 않습니다.", fFTMax, f.ftMax, "0", true)))
}

func (f *settingsForm) generalCard(u *UI) layout.Widget {
	t := u.th
	return gui.FormCard(t, "일반", "",
		func(gtx C) D {
			return t.FormRow(gtx, "로그 수준", "debug 는 VPN 다이얼 실패 같은 세부 내용까지 기록합니다.", "", func(gtx C) D {
				return t.Segmented(gtx, &f.logLevel, []gui.SegOption{
					{Key: "debug", Label: "debug"}, {Key: "info", Label: "info"},
					{Key: "warn", Label: "warn"}, {Key: "error", Label: "error"},
				})
			})
		},
		func(gtx C) D {
			return t.SwitchRow(gtx, "로그온 시 자동 실행", "Windows 에 로그온하면 트레이에서 서버를 시작합니다. 바로 적용되며 저장할 필요가 없습니다.", &f.autostart)
		})
}

func (f *settingsForm) saveBar(gtx C, u *UI) D {
	t := u.th
	return layout.Background{}.Layout(gtx,
		func(gtx C) D {
			sz := gtx.Constraints.Min
			paint.FillShape(gtx.Ops, t.P.Surface, clip.Rect{Max: sz}.Op())
			paint.FillShape(gtx.Ops, t.P.Border, clip.Rect{Max: image.Pt(sz.X, max(1, gtx.Dp(1)))}.Op())
			return D{Size: sz}
		},
		func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Top: 14, Bottom: 14, Left: 28, Right: 28}.Layout(gtx, func(gtx C) D {
				saveLabel := "저장"
				if u.running() {
					saveLabel = "저장 후 재시작"
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						if !f.dirty {
							return t.Muted("저장된 설정과 같습니다.").Layout(gtx)
						}
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return t.Dot(gtx, t.P.Warning, 8) }),
							gui.HSpace(8),
							layout.Rigid(t.Label(13.5, "저장하지 않은 변경 사항이 있습니다", t.P.Text, font.Medium).Layout))
					}),
					layout.Rigid(t.Btn(&f.revert, gui.BtnSecondary, "되돌리기", gui.IconUndo).WithDisabled(!f.dirty).Layout),
					gui.HSpace(8),
					layout.Rigid(t.Btn(&f.save, gui.BtnPrimary, saveLabel, gui.IconSave).WithDisabled(!f.dirty).Layout))
			})
		})
}
