package clientui

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

	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/gui"
)

// Settings tabs.
const (
	tabConn     = "conn"
	tabProxy    = "proxy"
	tabTunnels  = "tunnels"
	tabForwards = "forwards"
	tabVPN      = "vpn"
	tabFiles    = "files"
	tabGeneral  = "general"
)

var settingsTabs = []struct{ key, label string }{
	{tabConn, "연결"}, {tabProxy, "프록시"}, {tabTunnels, "터널"}, {tabForwards, "포트 포워드"},
	{tabVPN, "VPN"}, {tabFiles, "파일 전송"}, {tabGeneral, "일반"},
}

func tabOf(field string) string {
	switch {
	case field == fName || field == fListen || field == fToken:
		return tabConn
	case field == fSOCKSAddr || field == fSOCKSAuth || field == fHTTPAddr:
		return tabProxy
	case strings.HasPrefix(field, tunnelPrefix):
		return tabTunnels
	case strings.HasPrefix(field, pfPrefix):
		return tabForwards
	case strings.HasPrefix(field, "vpn."):
		return tabVPN
	case strings.HasPrefix(field, "ft."):
		return tabFiles
	}
	return tabGeneral
}

const (
	digits   = "0123456789"
	decimal  = "0123456789."
	defSOCKS = "127.0.0.1:1080"
	defHTTP  = "127.0.0.1:8080"
	mib      = 1 << 20
)

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type tunnelRow struct {
	typ                    widget.Enum
	host, port, pub, hname *gui.TextField
	del                    widget.Clickable
}

type forwardRow struct {
	local, host, remote, bind *gui.TextField
	del                       widget.Clickable
}

// settingsForm edits every functional ClientConfig field. Fields the console
// does not show (insecure, http_proxy_user/pass, which have no effect) are
// carried over from base unchanged.
type settingsForm struct {
	base *config.ClientConfig

	tab  widget.Enum
	list widget.List

	name, listen, token       *gui.TextField
	genToken, copyToken       widget.Clickable
	socksOn, httpOn           widget.Bool
	socksAddr, socksUser      *gui.TextField
	socksPass, httpAddr       *gui.TextField
	tunnels                   []*tunnelRow
	addTunnel                 widget.Clickable
	forwards                  []*forwardRow
	addForward                widget.Clickable
	vpnOn                     widget.Bool
	vpnIface, vpnRoutes       *gui.TextField
	vpnBypass, vpnDNS, vpnMTU *gui.TextField
	vpnMode                   widget.Enum

	ftOn                          widget.Bool
	ftAddr, ftDir, ftToken, ftMax *gui.TextField
	ftEndpoint, ftControl         *gui.TextField
	genFT, openDrop               widget.Clickable
	ctxMenu                       widget.Bool
	logLevel                      widget.Enum
	autostart                     widget.Bool
	certPath, keyPath             *gui.TextField
	save, revert                  widget.Clickable
	errs                          fieldErrors
	dirty, showErrs               bool
}

func field(filter string) *gui.TextField {
	f := gui.NewTextField(false)
	f.Filter = filter
	return f
}

func newSettingsForm(cfg *config.ClientConfig, autostart, ctxMenu bool) *settingsForm {
	f := &settingsForm{
		name: field(""), listen: field(""), token: gui.NewTextField(true),
		socksAddr: field(""), socksUser: field(""), socksPass: gui.NewTextField(true), httpAddr: field(""),
		vpnIface: field(""), vpnRoutes: field(""), vpnBypass: field(""), vpnDNS: field(""), vpnMTU: field(digits),
		ftAddr: field(""), ftDir: field(""), ftToken: gui.NewTextField(true), ftMax: field(decimal),
		ftEndpoint: field(""), ftControl: field(""),
		certPath: field(""), keyPath: field(""),
	}
	f.tab.Value = tabConn
	f.list.Axis = layout.Vertical
	f.autostart.Value = autostart
	f.ctxMenu.Value = ctxMenu
	f.load(cfg)
	return f
}

func cloneConfig(c *config.ClientConfig) *config.ClientConfig {
	cp := *c
	cp.Tunnels = append([]config.TunnelConfig(nil), c.Tunnels...)
	cp.PortForwards = append([]config.PortForwardConfig(nil), c.PortForwards...)
	cp.VPN.Routes = append([]string(nil), c.VPN.Routes...)
	cp.VPN.Bypass = append([]string(nil), c.VPN.Bypass...)
	cp.VPN.DNS = append([]string(nil), c.VPN.DNS...)
	return &cp
}

func levelOf(s string) string {
	switch l := strings.ToLower(strings.TrimSpace(s)); l {
	case "debug", "warn", "error":
		return l
	}
	return "info"
}

// normalize makes equivalent configs compare equal.
func normalize(c *config.ClientConfig) *config.ClientConfig {
	c = cloneConfig(c)
	if len(c.Tunnels) == 0 {
		c.Tunnels = nil
	}
	for i := range c.Tunnels {
		c.Tunnels[i].Type = normType(c.Tunnels[i].Type)
	}
	if len(c.PortForwards) == 0 {
		c.PortForwards = nil
	}
	for _, s := range []*[]string{&c.VPN.Routes, &c.VPN.Bypass, &c.VPN.DNS} {
		if len(*s) == 0 {
			*s = nil
		}
	}
	c.LogLevel = levelOf(c.LogLevel)
	return c
}

func formatFloat(v float64) string {
	if v == 0 {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func itoa(v int) string { return strconv.Itoa(v) }

func setOptional(on *widget.Bool, ed *gui.TextField, v, def string) {
	on.Value = v != ""
	if v == "" {
		v = def
	}
	ed.SetText(v)
}

// load resets every field from cfg.
func (f *settingsForm) load(cfg *config.ClientConfig) {
	f.base = normalize(cfg)
	c := f.base
	f.name.SetText(c.Name)
	f.listen.SetText(c.ListenAddr)
	f.token.SetText(c.AuthToken)
	setOptional(&f.socksOn, f.socksAddr, c.SOCKSAddr, defSOCKS)
	f.socksUser.SetText(c.SOCKSUser)
	f.socksPass.SetText(c.SOCKSPass)
	setOptional(&f.httpOn, f.httpAddr, c.HTTPProxyAddr, defHTTP)
	f.tunnels = nil
	for _, t := range c.Tunnels {
		f.addTunnelRow(t)
	}
	f.forwards = nil
	for _, pf := range c.PortForwards {
		f.addForwardRow(pf)
	}
	f.vpnOn.Value = c.VPN.Enabled
	f.vpnIface.SetText(c.VPN.InterfaceName)
	f.vpnRoutes.SetText(strings.Join(c.VPN.Routes, ", "))
	f.vpnMode.Value = "full"
	if len(c.VPN.Routes) > 0 {
		f.vpnMode.Value = "split"
	}
	f.vpnBypass.SetText(strings.Join(c.VPN.Bypass, ", "))
	f.vpnDNS.SetText(strings.Join(c.VPN.DNS, ", "))
	mtu := ""
	if c.VPN.MTU != 0 {
		mtu = itoa(c.VPN.MTU)
	}
	f.vpnMTU.SetText(mtu)
	ft := c.FileTransfer
	f.ftOn.Value = ft.Enabled
	f.ftAddr.SetText(ft.ReceiveAddr)
	f.ftDir.SetText(ft.DropDir)
	f.ftToken.SetText(ft.Token)
	f.ftMax.SetText(formatFloat(float64(ft.MaxFileSize) / mib))
	f.ftEndpoint.SetText(ft.SendEndpoint)
	f.ftControl.SetText(ft.ControlAddr)
	f.logLevel.Value = c.LogLevel
	f.certPath.SetText(c.CertPath)
	f.keyPath.SetText(c.KeyPath)
	f.showErrs = false
}

func (f *settingsForm) addTunnelRow(t config.TunnelConfig) {
	r := &tunnelRow{host: field(""), port: field(digits), pub: field(digits), hname: field("")}
	r.typ.Value = normType(t.Type)
	host := t.LocalHost
	if host == "" {
		host = "127.0.0.1"
	}
	r.host.SetText(host)
	if t.LocalPort != 0 {
		r.port.SetText(itoa(t.LocalPort))
	}
	r.pub.SetText(itoa(t.RequestedPort))
	r.hname.SetText(t.Hostname)
	f.tunnels = append(f.tunnels, r)
}

func (f *settingsForm) addForwardRow(pf config.PortForwardConfig) {
	r := &forwardRow{local: field(digits), host: field(""), remote: field(digits), bind: field("")}
	if pf.LocalPort != 0 {
		r.local.SetText(itoa(pf.LocalPort))
	}
	r.host.SetText(pf.RemoteHost)
	if pf.RemotePort != 0 {
		r.remote.SetText(itoa(pf.RemotePort))
	}
	r.bind.SetText(pf.Bind)
	f.forwards = append(f.forwards, r)
}

func text(f *gui.TextField) string { return strings.TrimSpace(f.Text()) }

// collect builds the config the form describes and validates it.
func (f *settingsForm) collect() (*config.ClientConfig, fieldErrors) {
	cfg := cloneConfig(f.base)
	perr := fieldErrors{}
	num := func(ed *gui.TextField, k string) int {
		s := text(ed)
		if s == "" {
			return 0
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			perr[k] = "정수를 입력하십시오"
		}
		return v
	}
	opt := func(on bool, ed *gui.TextField) string {
		if !on {
			return ""
		}
		return text(ed)
	}

	cfg.Name = text(f.name)
	cfg.ListenAddr = text(f.listen)
	cfg.AuthToken = text(f.token)
	cfg.SOCKSAddr = opt(f.socksOn.Value, f.socksAddr)
	cfg.SOCKSUser = text(f.socksUser)
	cfg.SOCKSPass = text(f.socksPass)
	cfg.HTTPProxyAddr = opt(f.httpOn.Value, f.httpAddr)
	cfg.Tunnels = nil
	for i, r := range f.tunnels {
		t := config.TunnelConfig{Type: normType(r.typ.Value), LocalHost: text(r.host), LocalPort: num(r.port, key(fTunnelPort, i))}
		if t.Type == "tcp" {
			t.RequestedPort = num(r.pub, key(fTunnelPub, i))
		} else {
			t.Hostname = text(r.hname)
		}
		cfg.Tunnels = append(cfg.Tunnels, t)
	}
	cfg.PortForwards = nil
	for i, r := range f.forwards {
		cfg.PortForwards = append(cfg.PortForwards, config.PortForwardConfig{
			LocalPort: num(r.local, key(fPFLocal, i)), RemoteHost: text(r.host),
			RemotePort: num(r.remote, key(fPFRemote, i)), Bind: text(r.bind),
		})
	}
	cfg.VPN.Enabled = f.vpnOn.Value
	cfg.VPN.InterfaceName = text(f.vpnIface)
	cfg.VPN.Routes = nil
	if f.vpnMode.Value == "split" {
		cfg.VPN.Routes = gui.SplitList(f.vpnRoutes.Text())
		if len(cfg.VPN.Routes) == 0 {
			perr[fVPNRoutes] = "VPN 으로 보낼 대역을 하나 이상 입력하십시오"
		}
	}
	cfg.VPN.Bypass = gui.SplitList(f.vpnBypass.Text())
	cfg.VPN.DNS = gui.SplitList(f.vpnDNS.Text())
	cfg.VPN.MTU = num(f.vpnMTU, fVPNMTU)
	cfg.FileTransfer.Enabled = f.ftOn.Value
	cfg.FileTransfer.ReceiveAddr = text(f.ftAddr)
	cfg.FileTransfer.DropDir = text(f.ftDir)
	cfg.FileTransfer.Token = text(f.ftToken)
	if s := text(f.ftMax); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			perr[fFTMax] = "숫자를 입력하십시오"
		} else {
			cfg.FileTransfer.MaxFileSize = int64(math.Round(v * mib))
		}
	} else {
		cfg.FileTransfer.MaxFileSize = 0
	}
	cfg.FileTransfer.SendEndpoint = text(f.ftEndpoint)
	cfg.FileTransfer.ControlAddr = text(f.ftControl)
	cfg.LogLevel = levelOf(f.logLevel.Value)
	cfg.CertPath = text(f.certPath)
	cfg.KeyPath = text(f.keyPath)

	errs := validate(cfg)
	for k, v := range perr {
		errs[k] = v
	}
	return cfg, errs
}

// errFor shows a message once the user tried to save or typed something.
func (f *settingsForm) errFor(k string, ed *gui.TextField) string {
	msg := f.errs[k]
	if msg == "" {
		return ""
	}
	if f.showErrs || ed == nil || text(ed) != "" {
		return msg
	}
	return ""
}

// visibleErrs counts, per tab, the errors errFor would show.
func (f *settingsForm) visibleErrs() map[string]int {
	counts := map[string]int{}
	for k := range f.errs {
		if f.errFor(k, f.fieldFor(k)) != "" {
			counts[tabOf(k)]++
		}
	}
	return counts
}

// fieldFor maps an error key to the field it is shown under (nil when the
// error does not belong to one text field).
func (f *settingsForm) fieldFor(k string) *gui.TextField {
	simple := map[string]*gui.TextField{
		fName: f.name, fListen: f.listen, fToken: f.token, fSOCKSAddr: f.socksAddr,
		fHTTPAddr: f.httpAddr, fVPNRoutes: f.vpnRoutes, fVPNBypass: f.vpnBypass, fVPNDNS: f.vpnDNS,
		fVPNMTU: f.vpnMTU, fVPNIface: f.vpnIface, fFTAddr: f.ftAddr, fFTDir: f.ftDir, fFTMax: f.ftMax,
		fFTEndpoint: f.ftEndpoint, fFTControl: f.ftControl, fCertPath: f.certPath, fKeyPath: f.keyPath,
	}
	if ed, ok := simple[k]; ok {
		return ed
	}
	for j, r := range f.tunnels {
		for format, ed := range map[string]*gui.TextField{fTunnelHost: r.host, fTunnelPort: r.port, fTunnelPub: r.pub, fTunnelName: r.hname} {
			if key(format, j) == k {
				return ed
			}
		}
	}
	for j, r := range f.forwards {
		for format, ed := range map[string]*gui.TextField{fPFLocal: r.local, fPFHost: r.host, fPFRemote: r.remote, fPFBind: r.bind} {
			if key(format, j) == k {
				return ed
			}
		}
	}
	return nil
}

func (f *settingsForm) update(gtx C, u *UI) {
	if f.genToken.Clicked(gtx) {
		f.token.SetText(newToken())
		u.toast.Show("새 토큰을 만들었습니다. 서버 설정에도 같은 값을 넣으십시오", gui.KindInfo)
	}
	if f.copyToken.Clicked(gtx) {
		gui.CopyText(gtx, text(f.token))
		u.toast.Show("토큰을 복사했습니다", gui.KindSuccess)
	}
	if f.genFT.Clicked(gtx) {
		f.ftToken.SetText(newToken())
	}
	if f.openDrop.Clicked(gtx) {
		if err := u.be.OpenDropFolder(); err != nil {
			u.toast.Show("수신 폴더를 열지 못했습니다: "+err.Error(), gui.KindDanger)
		}
	}
	if f.addTunnel.Clicked(gtx) {
		f.addTunnelRow(config.TunnelConfig{Type: "tcp"})
	}
	for i := 0; i < len(f.tunnels); i++ {
		if f.tunnels[i].del.Clicked(gtx) {
			f.tunnels = append(f.tunnels[:i], f.tunnels[i+1:]...)
			i--
		}
	}
	if f.addForward.Clicked(gtx) {
		f.addForwardRow(config.PortForwardConfig{})
	}
	for i := 0; i < len(f.forwards); i++ {
		if f.forwards[i].del.Clicked(gtx) {
			f.forwards = append(f.forwards[:i], f.forwards[i+1:]...)
			i--
		}
	}
	for _, o := range []struct {
		on  *widget.Bool
		ed  *gui.TextField
		def string
	}{{&f.socksOn, f.socksAddr, defSOCKS}, {&f.httpOn, f.httpAddr, defHTTP}} {
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
	if f.ctxMenu.Update(gtx) {
		if err := u.be.SetContextMenu(f.ctxMenu.Value); err != nil {
			f.ctxMenu.Value = !f.ctxMenu.Value
			u.toast.Show("우클릭 메뉴를 바꾸지 못했습니다: "+err.Error(), gui.KindDanger)
		} else if f.ctxMenu.Value {
			u.toast.Show("탐색기 우클릭 메뉴에 ‘Astron Link로 파일 전송’을 추가했습니다", gui.KindSuccess)
		} else {
			u.toast.Show("우클릭 메뉴를 제거했습니다", gui.KindNeutral)
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
			for _, tb := range settingsTabs {
				found := false
				for k := range errs {
					if tabOf(k) == tb.key {
						found = true
					}
				}
				if found {
					f.tab.Value = tb.key
					break
				}
			}
			u.toast.Show(fmt.Sprintf("입력값 %d개를 확인하십시오", len(errs)), gui.KindDanger)
			return
		}
		title, body, confirm := "설정을 저장할까요?", "config.yaml 에 저장합니다. 다음 연결부터 적용됩니다.", "저장"
		if u.active() {
			title = "저장하고 다시 연결할까요?"
			body = "config.yaml 에 저장한 뒤 다시 연결합니다. 서버 연결과 VPN 이 잠시 끊겼다가 서버가 다시 접속합니다."
			confirm = "저장 후 재연결"
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
			u.toast.Show("저장했습니다", gui.KindSuccess)
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
		n := 0
		switch tb.key {
		case tabTunnels:
			n = len(f.tunnels)
		case tabForwards:
			n = len(f.forwards)
		}
		if c := counts[tb.key]; c > 0 {
			items[i].Badge, items[i].BadgeKind = strconv.Itoa(c), gui.KindDanger
		} else if n > 0 {
			items[i].Badge, items[i].BadgeKind = strconv.Itoa(n), gui.KindNeutral
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 28, Right: 28}.Layout(gtx, func(gtx C) D { return t.Tabs(gtx, &f.tab, items) })
		}),
		gui.VSpace(18),
		layout.Flexed(1, func(gtx C) D { return u.scroll(gtx, &f.list, f.tabContent(u)...) }),
		layout.Rigid(func(gtx C) D { return f.saveBar(gtx, u) }))
}

func (f *settingsForm) tabContent(u *UI) []layout.Widget {
	switch f.tab.Value {
	case tabProxy:
		return []layout.Widget{f.proxyCards(u)}
	case tabTunnels:
		return []layout.Widget{f.tunnelsCard(u)}
	case tabForwards:
		return []layout.Widget{f.forwardsCard(u)}
	case tabVPN:
		return []layout.Widget{f.vpnCard(u)}
	case tabFiles:
		return []layout.Widget{f.filesCard(u)}
	case tabGeneral:
		return []layout.Widget{f.generalCard(u), f.certCard(u)}
	}
	return []layout.Widget{f.connCard(u)}
}

func (f *settingsForm) input(t *gui.Theme, ed *gui.TextField, k, hint string, mono bool, extra ...layout.FlexChild) layout.Widget {
	return func(gtx C) D { return ed.Layout(gtx, t, hint, f.errFor(k, ed) != "", mono, extra...) }
}

func (f *settingsForm) row(t *gui.Theme, title, desc, k string, ed *gui.TextField, hint string, mono bool, extra ...layout.FlexChild) layout.Widget {
	return func(gtx C) D {
		return t.FormRow(gtx, title, desc, f.errFor(k, ed), f.input(t, ed, k, hint, mono, extra...))
	}
}

func (f *settingsForm) toggledRow(t *gui.Theme, title, desc, k string, on *widget.Bool, ed *gui.TextField, hint string) layout.Widget {
	return func(gtx C) D {
		return t.FormRow(gtx, title, desc, f.errFor(k, ed), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return t.Switch(gtx, on) }),
				gui.HSpace(12),
				layout.Flexed(1, func(gtx C) D {
					if !on.Value {
						gtx = gtx.Disabled()
					}
					return f.input(t, ed, k, hint, true)(gtx)
				}))
		})
	}
}

func disabledUnless(on *widget.Bool, w layout.Widget) layout.Widget {
	return func(gtx C) D {
		if !on.Value {
			gtx = gtx.Disabled()
		}
		return w(gtx)
	}
}

func (f *settingsForm) connCard(u *UI) layout.Widget {
	t := u.th
	return gui.FormCard(t, "서버 연결", "서버가 이 PC 로 접속해 옵니다(역방향). 아래 세 값이 서버 관리 콘솔의 클라이언트 항목과 맞아야 합니다.",
		f.row(t, "이름", "서버 클라이언트 목록의 이름과 같아야 합니다.", fName, f.name, "my-pc", false),
		f.row(t, "대기 주소", "서버가 접속할 TLS 포트입니다. Windows 방화벽에서 이 포트를 허용하십시오.", fListen, f.listen, ":8443", true),
		f.row(t, "인증 토큰", "서버가 보내는 토큰과 같아야 합니다. 서버 콘솔의 [토큰 복사] 값을 붙여 넣으십시오.", fToken, f.token, "서버 토큰을 붙여 넣으십시오", true,
			gui.IconAction(t, &f.copyToken, gui.IconCopy, ""),
			gui.IconAction(t, &f.genToken, gui.IconGenerate, "생성")))
}

func (f *settingsForm) proxyCards(u *UI) layout.Widget {
	t := u.th
	socks := gui.FormCard(t, "SOCKS5 프록시", "앱이 이 주소를 프록시로 쓰면 서버를 거쳐 나갑니다. 127.0.0.1 이 아니면 인증을 설정하십시오.",
		f.toggledRow(t, "SOCKS5 리스너", "", fSOCKSAddr, &f.socksOn, f.socksAddr, defSOCKS),
		disabledUnless(&f.socksOn, f.row(t, "사용자", "비밀번호와 함께 쓰면 RFC 1929 인증을 요구합니다.", fSOCKSAuth, f.socksUser, "비우면 인증 없음", false)),
		disabledUnless(&f.socksOn, f.row(t, "비밀번호", "", fSOCKSAuth, f.socksPass, "비우면 인증 없음", false)))
	http := gui.FormCard(t, "HTTP CONNECT 프록시", "HTTPS 프록시로 지정합니다. 인증은 지원하지 않으므로 127.0.0.1 에서만 받으십시오.",
		f.toggledRow(t, "HTTP 프록시 리스너", "", fHTTPAddr, &f.httpOn, f.httpAddr, defHTTP))
	return func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Rigid(socks), gui.VSpace(16), layout.Rigid(http))
	}
}

func (f *settingsForm) tunnelsCard(u *UI) layout.Widget {
	t := u.th
	return func(gtx C) D {
		return t.Card(gtx, func(gtx C) D {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return t.SectionTitle(gtx, "터널", "TCP 는 서버의 공개 포트, HTTP · HTTPS 는 서버 프록시의 호스트 이름으로 들어온 연결을 이 PC 의 서비스로 보냅니다.")
						}),
						gui.HSpace(16),
						layout.Rigid(t.Btn(&f.addTunnel, gui.BtnSecondary, "추가", gui.IconAdd).Layout))
				}),
				gui.VSpace(14),
			}
			if len(f.tunnels) == 0 {
				children = append(children, layout.Rigid(func(gtx C) D {
					return t.EmptyState(gtx, gui.IconSwap, "터널이 없습니다", "[추가]를 눌러 공개할 서비스를 등록하십시오.")
				}))
			}
			for i, r := range f.tunnels {
				if i > 0 {
					children = append(children, gui.VSpace(12))
				}
				children = append(children, layout.Rigid(func(gtx C) D { return f.tunnelEditor(gtx, t, i, r) }))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
}

func (f *settingsForm) tunnelEditor(gtx C, t *gui.Theme, i int, r *tunnelRow) D {
	kh, kp, kpub, kn := key(fTunnelHost, i), key(fTunnelPort, i), key(fTunnelPub, i), key(fTunnelName, i)
	typ := normType(r.typ.Value)
	return gui.Box{Bg: t.P.SurfaceAlt, Border: t.P.Border, Radius: 10, Expand: true, Inset: layout.UniformInset(16)}.Layout(gtx, func(gtx C) D {
		public := gui.Labeled(t, "서버 공개 포트", "0 이면 서버가 빈 포트를 고릅니다", f.errFor(kpub, r.pub), f.input(t, r.pub, kpub, "0", true))
		if typ != "tcp" {
			public = gui.Labeled(t, "호스트 이름", "서버 "+strings.ToUpper(typ)+" 프록시로 이 이름이 들어오면 연결합니다", f.errFor(kn, r.hname), f.input(t, r.hname, kn, "app.example.com", true))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return t.Segmented(gtx, &r.typ, []gui.SegOption{{Key: "tcp", Label: "TCP"}, {Key: "http", Label: "HTTP"}, {Key: "https", Label: "HTTPS"}})
					}),
					layout.Flexed(1, layout.Spacer{}.Layout),
					layout.Rigid(t.Btn(&r.del, gui.BtnGhost, "삭제", gui.IconDelete).WithSmall().Layout))
			}),
			gui.VSpace(12),
			layout.Rigid(func(gtx C) D {
				return gui.Grid(gtx, 200, 14,
					gui.Labeled(t, "로컬 호스트", "이 PC 에서 서비스가 듣는 주소", f.errFor(kh, r.host), f.input(t, r.host, kh, "127.0.0.1", true)),
					gui.Labeled(t, "로컬 포트", "", f.errFor(kp, r.port), f.input(t, r.port, kp, "3000", true)),
					public)
			}))
	})
}

func (f *settingsForm) forwardsCard(u *UI) layout.Widget {
	t := u.th
	return func(gtx C) D {
		return t.Card(gtx, func(gtx C) D {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return t.SectionTitle(gtx, "포트 포워드", "이 PC 의 로컬 포트로 들어온 연결을 서버가 대상 호스트:포트로 이어 줍니다(서버 쪽 내부망 접근).")
						}),
						gui.HSpace(16),
						layout.Rigid(t.Btn(&f.addForward, gui.BtnSecondary, "추가", gui.IconAdd).Layout))
				}),
				gui.VSpace(14),
			}
			if len(f.forwards) == 0 {
				children = append(children, layout.Rigid(func(gtx C) D {
					return t.EmptyState(gtx, gui.IconRouter, "포트 포워드가 없습니다", "[추가]를 눌러 로컬 포트와 대상을 입력하십시오.")
				}))
			}
			for i, r := range f.forwards {
				if i > 0 {
					children = append(children, gui.VSpace(12))
				}
				children = append(children, layout.Rigid(func(gtx C) D { return f.forwardEditor(gtx, t, i, r) }))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
}

func (f *settingsForm) forwardEditor(gtx C, t *gui.Theme, i int, r *forwardRow) D {
	kl, kh, kr, kb := key(fPFLocal, i), key(fPFHost, i), key(fPFRemote, i), key(fPFBind, i)
	return gui.Box{Bg: t.P.SurfaceAlt, Border: t.P.Border, Radius: 10, Expand: true, Inset: layout.UniformInset(16)}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(gui.IconW(gui.IconRouter, 18, t.P.TextMuted)),
					gui.HSpace(8),
					layout.Flexed(1, t.Label(14, fmt.Sprintf("포트 포워드 %d", i+1), t.P.Text, font.SemiBold).Layout),
					layout.Rigid(t.Btn(&r.del, gui.BtnGhost, "삭제", gui.IconDelete).WithSmall().Layout))
			}),
			gui.VSpace(12),
			layout.Rigid(func(gtx C) D {
				return gui.Grid(gtx, 160, 14,
					gui.Labeled(t, "바인드 주소", "비우면 127.0.0.1", f.errFor(kb, r.bind), f.input(t, r.bind, kb, "127.0.0.1", true)),
					gui.Labeled(t, "로컬 포트", "", f.errFor(kl, r.local), f.input(t, r.local, kl, "15432", true)),
					gui.Labeled(t, "대상 호스트", "서버에서 본 주소", f.errFor(kh, r.host), f.input(t, r.host, kh, "10.0.0.5", true)),
					gui.Labeled(t, "대상 포트", "", f.errFor(kr, r.remote), f.input(t, r.remote, kr, "5432", true)))
			}))
	})
}

func (f *settingsForm) vpnCard(u *UI) layout.Widget {
	t := u.th
	off := func(w layout.Widget) layout.Widget { return disabledUnless(&f.vpnOn, w) }
	return gui.FormCard(t, "VPN 모드", "서버가 접속하면 가상 네트워크 어댑터(Wintun)를 만들어 트래픽을 서버로 보냅니다. 관리자 권한과 프로그램 폴더의 wintun.dll 이 필요합니다.",
		func(gtx C) D {
			return t.SwitchRow(gtx, "VPN 사용", "끄면 SOCKS5 · HTTP 프록시와 터널만 씁니다.", &f.vpnOn)
		},
		off(func(gtx C) D {
			return t.FormRow(gtx, "보낼 트래픽", "전체 터널은 모든 IPv4 트래픽을, 일부 대역은 지정한 대역만 서버로 보냅니다.", f.errFor(fVPNRoutes, f.vpnRoutes), func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return t.Segmented(gtx, &f.vpnMode, []gui.SegOption{{Key: "full", Label: "전체 터널"}, {Key: "split", Label: "일부 대역만"}})
					}),
					layout.Rigid(func(gtx C) D {
						if f.vpnMode.Value != "split" {
							return D{}
						}
						return layout.Inset{Top: 10}.Layout(gtx, f.input(t, f.vpnRoutes, fVPNRoutes, "10.0.0.0/8, 192.168.50.0/24", true))
					}))
			})
		}),
		off(f.row(t, "우회 대역", "항상 물리 네트워크로 보낼 대역입니다. 이 PC 에서 서비스를 공개 중이면 그 접속 대역을 넣으십시오. 서버 주소는 자동으로 우회합니다.", fVPNBypass, f.vpnBypass, "203.0.113.10, 192.168.0.0/16", true)),
		off(f.row(t, "DNS 서버", "비우면 서버가 알려 준 DNS(서버 내장 리졸버)를 씁니다.", fVPNDNS, f.vpnDNS, "비우면 서버 값", true)),
		off(f.row(t, "MTU", "비우면 서버 값(기본 1500).", fVPNMTU, f.vpnMTU, "서버 값", true)),
		off(f.row(t, "어댑터 이름", "비우면 AstronLink.", fVPNIface, f.vpnIface, "AstronLink", false)))
}

func (f *settingsForm) filesCard(u *UI) layout.Widget {
	t := u.th
	off := func(w layout.Widget) layout.Widget { return disabledUnless(&f.ftOn, w) }
	return func(gtx C) D {
		recv := gui.FormCard(t, "파일 수신", "서버 쪽 피어가 터널로 보낸 파일을 이 PC 의 폴더에 저장합니다.",
			func(gtx C) D { return t.SwitchRow(gtx, "파일 전송 사용", "", &f.ftOn) },
			off(f.row(t, "수신 주소", "터널로 노출할 로컬 HTTP 주소입니다.", fFTAddr, f.ftAddr, "127.0.0.1:8089", true)),
			off(f.row(t, "저장 폴더", "상대 경로는 프로그램 폴더 기준입니다.", fFTDir, f.ftDir, "received", true,
				gui.IconAction(t, &f.openDrop, gui.IconFolder, "열기"))),
			off(f.row(t, "업로드 토큰", "비우면 인증 없이 받습니다. 양쪽이 같은 값이어야 합니다.", "", f.ftToken, "비우면 인증 없음", true,
				gui.IconAction(t, &f.genFT, gui.IconGenerate, "생성"))),
			off(f.row(t, "최대 파일 크기 (MB)", "0 이면 제한하지 않습니다.", fFTMax, f.ftMax, "0", true)))
		send := gui.FormCard(t, "파일 보내기", "탐색기에서 파일을 우클릭해 서버 쪽으로 보냅니다.",
			off(f.row(t, "보낼 주소", "이 PC 에서 터널을 통해 닿는 상대의 수신 주소입니다 (예: 포트 포워드한 http://127.0.0.1:8090).", fFTEndpoint, f.ftEndpoint, "http://127.0.0.1:8090", true)),
			off(f.row(t, "제어 주소", "우클릭 메뉴가 이 프로그램에 전송을 요청하는 로컬 주소입니다.", fFTControl, f.ftControl, "127.0.0.1:8077", true)),
			func(gtx C) D {
				return t.SwitchRow(gtx, "탐색기 우클릭 메뉴", "‘Astron Link로 파일 전송’ 항목을 등록합니다. 바로 적용됩니다 (Windows 11 은 ‘추가 옵션 표시’ 안).", &f.ctxMenu)
			})
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Rigid(recv), gui.VSpace(16), layout.Rigid(send))
	}
}

func (f *settingsForm) generalCard(u *UI) layout.Widget {
	t := u.th
	return gui.FormCard(t, "일반", "",
		func(gtx C) D {
			return t.FormRow(gtx, "로그 수준", "debug 는 연결 과정의 세부 내용까지 기록합니다.", "", func(gtx C) D {
				return t.Segmented(gtx, &f.logLevel, []gui.SegOption{
					{Key: "debug", Label: "debug"}, {Key: "info", Label: "info"},
					{Key: "warn", Label: "warn"}, {Key: "error", Label: "error"},
				})
			})
		},
		func(gtx C) D {
			return t.SwitchRow(gtx, "로그온 시 자동 실행", "Windows 에 로그온하면 트레이에서 시작하고 바로 연결합니다. 바로 적용됩니다.", &f.autostart)
		})
}

func (f *settingsForm) certCard(u *UI) layout.Widget {
	t := u.th
	return gui.FormCard(t, "TLS 인증서", "서버가 접속할 때 이 PC 가 제시하는 인증서입니다. 파일이 없으면 자체 서명 인증서를 만듭니다.",
		f.row(t, "인증서 파일", "", fCertPath, f.certPath, "client.crt", true),
		f.row(t, "키 파일", "", fKeyPath, f.keyPath, "client.key", true))
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
				label := "저장"
				if u.active() {
					label = "저장 후 재연결"
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
					layout.Rigid(t.Btn(&f.save, gui.BtnPrimary, label, gui.IconSave).WithDisabled(!f.dirty).Layout))
			})
		})
}
