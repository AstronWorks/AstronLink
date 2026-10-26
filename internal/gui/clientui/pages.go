package clientui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/gui"
	"github.com/AstronWorks/AstronLink/internal/gui/logview"
)

// listenPort returns the port of a listen address ("" when invalid).
func listenPort(addr string) string {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return p
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ---------------------------------------------------------------- overview

type overview struct {
	list                          widget.List
	toSettings, toLogs, toTunnels widget.Clickable
	copyAddr                      []widget.Clickable
	copySOCKS, copyHTTP           widget.Clickable
}

func (o *overview) layout(gtx C, u *UI) D {
	t := u.th
	st := u.state
	cfg := u.cfg()
	if o.toSettings.Clicked(gtx) {
		u.page = PageSettings
	}
	if o.toLogs.Clicked(gtx) {
		u.page = PageLogs
	}
	if o.toTunnels.Clicked(gtx) {
		u.page = PageTunnels
	}

	var items []layout.Widget
	if st.Phase == Failed {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindDanger, "연결을 시작하지 못했습니다", st.Err,
				layout.Rigid(t.Btn(&o.toSettings, gui.BtnSecondary, "설정 열기", nil).Layout))
		})
	}
	if u.loadErr != "" {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindDanger, "config.yaml 을 읽지 못해 기본값을 표시합니다", u.loadErr)
		})
	}
	if strings.TrimSpace(cfg.AuthToken) == "" || cfg.AuthToken == "changeme" {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindWarning, "인증 토큰을 확인하십시오",
				"서버 관리 콘솔의 [클라이언트] 탭에서 [토큰 복사]로 값을 가져와 설정 > 연결에 붙여 넣으십시오. 다르면 서버 로그에 invalid token 이 남습니다.",
				layout.Rigid(t.Btn(&o.toSettings, gui.BtnSecondary, "설정 열기", nil).Layout))
		})
	}
	if cfg.VPN.Enabled && !u.info.Admin {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindWarning, "VPN 을 쓰려면 관리자 권한이 필요합니다",
				"가상 네트워크 어댑터를 만들 수 있도록 프로그램을 [관리자 권한으로 실행]하십시오.")
		})
	}
	if cfg.VPN.Enabled && !u.info.Wintun {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindDanger, "wintun.dll 이 없습니다",
				"VPN 어댑터 드라이버(wintun.dll, amd64)를 프로그램 폴더에 두어야 합니다.")
		})
	}
	if st.Phase == Waiting {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindInfo, "서버의 접속을 기다리는 중입니다",
				fmt.Sprintf("서버 관리 콘솔의 클라이언트 목록에 이름 %q 와 아래 주소 중 하나(포트 %s)를 등록하십시오.", cfg.Name, listenPort(cfg.ListenAddr)))
		})
	}

	items = append(items, func(gtx C) D {
		k, label := phaseView(st.Phase)
		sub := "—"
		switch st.Phase {
		case Connected, Waiting:
			sub = "대기 시작 후 " + gui.HumanDuration(gtx.Now.Sub(st.Since))
		}
		vpnVal, vpnSub, vpnKind := "꺼짐", "설정 > VPN 에서 켭니다", gui.KindNeutral
		if cfg.VPN.Enabled {
			vpnVal, vpnSub, vpnKind = "대기", "서버가 접속하면 어댑터를 만듭니다", gui.KindWarning
			if st.VPN.Active {
				vpnVal, vpnSub, vpnKind = "활성", st.VPN.Address, gui.KindSuccess
			}
		}
		return gui.Grid(gtx, 210, 16,
			func(gtx C) D { return t.StatTile(gtx, gui.IconPower, k, "연결", label, sub) },
			func(gtx C) D {
				return t.StatTile(gtx, gui.IconRouter, gui.KindPrimary, "연결된 서버", strconv.Itoa(len(st.Servers)), "이 PC 에 접속한 서버")
			},
			func(gtx C) D { return t.StatTile(gtx, gui.IconVPN, vpnKind, "VPN", vpnVal, vpnSub) },
			func(gtx C) D {
				return t.StatTile(gtx, gui.IconSwap, gui.KindInfo, "터널", strconv.Itoa(len(cfg.Tunnels)),
					fmt.Sprintf("포트 포워드 %d개", len(cfg.PortForwards)))
			})
	})
	items = append(items, func(gtx C) D {
		return gui.Grid(gtx, 400, 16,
			func(gtx C) D { return o.servers(gtx, u, cfg) },
			func(gtx C) D { return o.proxies(gtx, u, cfg) })
	})
	items = append(items, func(gtx C) D {
		return gui.Grid(gtx, 400, 16,
			func(gtx C) D { return o.vpn(gtx, u, cfg) },
			func(gtx C) D { return o.recent(gtx, u) })
	})
	return u.scroll(gtx, &o.list, items...)
}

func (o *overview) servers(gtx C, u *UI, cfg *config.ClientConfig) D {
	t := u.th
	port := listenPort(cfg.ListenAddr)
	addrs := make([]string, 0, len(u.info.LocalIPs))
	for _, ip := range u.info.LocalIPs {
		addrs = append(addrs, net.JoinHostPort(ip, port))
	}
	for len(o.copyAddr) < len(addrs) {
		o.copyAddr = append(o.copyAddr, widget.Clickable{})
	}
	for i, a := range addrs {
		if o.copyAddr[i].Clicked(gtx) {
			gui.CopyText(gtx, a)
			u.toast.Show(a+" 를 복사했습니다", gui.KindSuccess)
		}
	}
	return t.Card(gtx, func(gtx C) D {
		children := []layout.FlexChild{layout.Rigid(gui.CardHeader(t, "서버 연결", nil)), gui.VSpace(6)}
		if len(u.state.Servers) == 0 {
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, t.Muted("연결된 서버가 없습니다.").Layout)
			}))
		}
		for i, s := range u.state.Servers {
			if i > 0 {
				children = append(children, layout.Rigid(t.Divider))
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return t.Dot(gtx, t.P.Success, 8) }),
						gui.HSpace(12),
						layout.Flexed(1, t.Code(s.Addr).Layout),
						layout.Rigid(t.Label(12.5, "연결됨 · "+gui.HumanDuration(gtx.Now.Sub(s.ConnectedAt)), t.P.Success, font.Medium).Layout))
				})
			}))
		}
		children = append(children, gui.VSpace(10), layout.Rigid(t.Divider), gui.VSpace(12),
			layout.Rigid(t.Label(13, "서버에 등록할 이 PC 의 주소", t.P.TextMuted, font.Medium).Layout), gui.VSpace(4))
		if len(addrs) == 0 {
			children = append(children, layout.Rigid(t.Caption("IPv4 주소를 찾지 못했습니다.").Layout))
		}
		for i, a := range addrs {
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, t.Code(a).Layout),
					layout.Rigid(t.Btn(&o.copyAddr[i], gui.BtnGhost, "", gui.IconCopy).WithSmall().Layout))
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (o *overview) proxies(gtx C, u *UI, cfg *config.ClientConfig) D {
	t := u.th
	socks, http := u.state.SOCKSAddr, u.state.HTTPAddr
	if socks == "" && !u.active() {
		socks = cfg.SOCKSAddr
	}
	if http == "" && !u.active() {
		http = cfg.HTTPProxyAddr
	}
	if o.copySOCKS.Clicked(gtx) {
		gui.CopyText(gtx, "socks5h://"+socks)
		u.toast.Show("SOCKS5 주소를 복사했습니다", gui.KindSuccess)
	}
	if o.copyHTTP.Clicked(gtx) {
		gui.CopyText(gtx, "http://"+http)
		u.toast.Show("HTTP 프록시 주소를 복사했습니다", gui.KindSuccess)
	}
	row := func(name, desc, addr string, clk *widget.Clickable, badge layout.Widget) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						// The inset keeps wrapped text clear of the address column.
						return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(t.Label(13.5, name, t.P.Text, font.Medium).Layout),
										gui.HSpace(8),
										layout.Rigid(badge))
								}),
								layout.Rigid(t.Caption(desc).Layout))
						})
					}),
					layout.Rigid(func(gtx C) D {
						if addr == "" {
							return t.Badge(gtx, gui.KindNeutral, "사용 안 함")
						}
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(t.Code(addr).Layout),
							gui.HSpace(4),
							layout.Rigid(t.Btn(clk, gui.BtnGhost, "", gui.IconCopy).WithSmall().Layout))
					}))
			})
		}
	}
	socksAuth := gui.OnOffBadge(t, cfg.SOCKSUser != "", "인증", "인증 없음", gui.KindSuccess, gui.KindNeutral)
	return t.Card(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(gui.CardHeader(t, "로컬 프록시", nil)),
			gui.VSpace(6),
			layout.Rigid(row("SOCKS5", "앱의 프록시로 지정 · 이름은 서버가 풉니다", socks, &o.copySOCKS, socksAuth)),
			layout.Rigid(t.Divider),
			layout.Rigid(row("HTTP CONNECT", "HTTPS 프록시로 지정", http, &o.copyHTTP, gui.Nothing)),
			layout.Rigid(t.Divider),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
					return t.KeyValue(gtx, "파일 수신", 100, func(gtx C) D {
						if u.state.FileReceiver == "" {
							return t.Muted(map[bool]string{true: "켜짐 (연결 후 시작)", false: "꺼짐"}[cfg.FileTransfer.Enabled]).Layout(gtx)
						}
						return t.Code(u.state.FileReceiver).Layout(gtx)
					})
				})
			}))
	})
}

func (o *overview) vpn(gtx C, u *UI, cfg *config.ClientConfig) D {
	t := u.th
	v := u.state.VPN
	routes := "전체 트래픽 (0.0.0.0/1, 128.0.0.0/1)"
	if len(cfg.VPN.Routes) > 0 {
		routes = strings.Join(cfg.VPN.Routes, ", ")
	}
	bypass := "서버 주소만 (자동)"
	if len(cfg.VPN.Bypass) > 0 {
		bypass = strings.Join(cfg.VPN.Bypass, ", ") + " + 서버 주소"
	}
	dns := "서버가 알려 준 값"
	if len(cfg.VPN.DNS) > 0 {
		dns = strings.Join(cfg.VPN.DNS, ", ")
	}
	status := gui.OnOffBadge(t, v.Active, "활성", map[bool]string{true: "대기", false: "꺼짐"}[cfg.VPN.Enabled], gui.KindSuccess, gui.KindNeutral)
	text := func(s string) layout.Widget { return t.Body(s).Layout }
	return t.Card(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(gui.CardHeader(t, "VPN", nil)),
			gui.VSpace(8),
			layout.Rigid(gui.KV(t, "상태", 100, status)),
			layout.Rigid(gui.KV(t, "어댑터", 100, text(orDash(v.Interface)))),
			layout.Rigid(gui.KV(t, "가상 주소", 100, text(orDash(v.Address)))),
			layout.Rigid(gui.KV(t, "게이트웨이", 100, text(orDash(v.Gateway)))),
			layout.Rigid(gui.KV(t, "보낼 대역", 100, text(routes))),
			layout.Rigid(gui.KV(t, "우회 대역", 100, text(bypass))),
			layout.Rigid(gui.KV(t, "DNS", 100, text(dns))))
	})
}

func (o *overview) recent(gtx C, u *UI) D {
	t := u.th
	lines := u.logs.Recent(6, 2)
	return t.Card(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(gui.CardHeader(t, "최근 경고 · 오류", t.Btn(&o.toLogs, gui.BtnGhost, "로그 보기", gui.IconChevron).WithSmall().Layout)),
			gui.VSpace(6),
		}
		if len(lines) == 0 {
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 10}.Layout(gtx, t.Muted("최근 경고나 오류가 없습니다.").Layout)
			}))
		}
		for i, ln := range lines {
			if i > 0 {
				children = append(children, layout.Rigid(t.Divider))
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 9, Bottom: 9}.Layout(gtx, logview.LineWidget(t, ln))
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// ---------------------------------------------------------------- tunnels

type tunnelsPage struct {
	list                 widget.List
	tunnels, forwards    gui.Table
	editTunnels, editPFs widget.Clickable
}

func (p *tunnelsPage) layout(gtx C, u *UI) D {
	t := u.th
	cfg := u.cfg()
	if p.editTunnels.Clicked(gtx) {
		u.page = PageSettings
		u.set.tab.Value = tabTunnels
	}
	if p.editPFs.Clicked(gtx) {
		u.page = PageSettings
		u.set.tab.Value = tabForwards
	}
	tcols := []gui.Column{{Title: "유형", Width: 84}, {Title: "서버 쪽 공개", Weight: 1.3}, {Title: "이 PC 의 대상", Weight: 1.2}}
	fcols := []gui.Column{{Title: "이 PC 에서 받는 주소", Weight: 1.2}, {Title: "", Width: 36}, {Title: "서버가 접속할 대상", Weight: 1.3}}
	section := func(title, desc string, clk *widget.Clickable) layout.Widget {
		return func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return t.SectionTitle(gtx, title, desc) }),
				layout.Rigid(t.Btn(clk, gui.BtnSecondary, "편집", gui.IconSettings).Layout))
		}
	}
	return u.scroll(gtx, &p.list,
		section("터널", "서버의 공개 포트·호스트 이름으로 들어온 연결을 이 PC 의 서비스로 보냅니다.", &p.editTunnels),
		func(gtx C) D {
			return p.tunnels.Layout(gtx, t, tcols, len(cfg.Tunnels), func(gtx C, row, col int) D {
				tn := cfg.Tunnels[row]
				typ := normType(tn.Type)
				switch col {
				case 0:
					k := map[string]gui.Kind{"tcp": gui.KindInfo, "http": gui.KindPrimary, "https": gui.KindSuccess}[typ]
					return t.Badge(gtx, k, strings.ToUpper(typ))
				case 1:
					if typ == "tcp" {
						if tn.RequestedPort == 0 {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(t.Body("자동 할당").Layout),
								layout.Rigid(t.Caption("서버 관리 콘솔의 터널 탭에서 포트 확인").Layout))
						}
						return t.Code(":" + strconv.Itoa(tn.RequestedPort)).Layout(gtx)
					}
					return t.Code(tn.Hostname).Layout(gtx)
				case 2:
					return t.Code(net.JoinHostPort(tn.LocalHost, strconv.Itoa(tn.LocalPort))).Layout(gtx)
				}
				return D{}
			}, func(gtx C) D {
				return t.EmptyState(gtx, gui.IconSwap, "등록할 터널이 없습니다", "[편집]에서 TCP · HTTP · HTTPS 터널을 추가하십시오.")
			})
		},
		section("포트 포워드", "이 PC 의 포트로 들어온 연결을 서버가 대상 주소로 이어 줍니다.", &p.editPFs),
		func(gtx C) D {
			return p.forwards.Layout(gtx, t, fcols, len(cfg.PortForwards), func(gtx C, row, col int) D {
				pf := cfg.PortForwards[row]
				switch col {
				case 0:
					bind := pf.Bind
					if bind == "" {
						bind = "127.0.0.1"
					}
					return t.Code(net.JoinHostPort(bind, strconv.Itoa(pf.LocalPort))).Layout(gtx)
				case 1:
					return gui.IconW(gui.IconChevron, 18, t.P.TextSubtle)(gtx)
				case 2:
					return t.Code(net.JoinHostPort(pf.RemoteHost, strconv.Itoa(pf.RemotePort))).Layout(gtx)
				}
				return D{}
			}, func(gtx C) D {
				return t.EmptyState(gtx, gui.IconRouter, "포트 포워드가 없습니다", "[편집]에서 로컬 포트와 서버 쪽 대상을 추가하십시오.")
			})
		})
}

// ---------------------------------------------------------------- about

type aboutPage struct {
	list             widget.List
	copyCfg, copyLog widget.Clickable
}

func (p *aboutPage) layout(gtx C, u *UI) D {
	t := u.th
	info := u.info
	if p.copyCfg.Clicked(gtx) {
		gui.CopyText(gtx, info.ConfigPath)
		u.toast.Show("경로를 복사했습니다", gui.KindSuccess)
	}
	if p.copyLog.Clicked(gtx) {
		gui.CopyText(gtx, info.LogPath)
		u.toast.Show("경로를 복사했습니다", gui.KindSuccess)
	}
	path := func(s string, clk *widget.Clickable) layout.Widget {
		return func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, t.Code(s).Layout),
				layout.Rigid(t.Btn(clk, gui.BtnGhost, "", gui.IconCopy).WithSmall().Layout))
		}
	}
	yesNo := func(ok bool, yes, no string) layout.Widget {
		return gui.OnOffBadge(t, ok, yes, no, gui.KindSuccess, gui.KindWarning)
	}
	return u.scroll(gtx, &p.list,
		func(gtx C) D {
			return t.Card(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return t.Logo(gtx, 52, brandFrom, brandTo, gui.IconComputer) }),
							gui.HSpace(16),
							layout.Rigid(func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(t.Label(19, "Astron Link Client", t.P.Text, font.SemiBold).Layout),
									layout.Rigid(t.Muted("역방향 TLS 터널 · SOCKS5 / HTTP 프록시 · VPN 어댑터").Layout))
							}))
					}),
					gui.VSpace(18),
					layout.Rigid(t.Divider),
					gui.VSpace(10),
					layout.Rigid(gui.KV(t, "버전", 120, t.Body(info.Version).Layout)),
					layout.Rigid(gui.KV(t, "커밋", 120, t.Code(info.Commit).Layout)),
					layout.Rigid(gui.KV(t, "빌드 시각", 120, t.Body(info.Built).Layout)),
					layout.Rigid(gui.KV(t, "설정 파일", 120, path(info.ConfigPath, &p.copyCfg))),
					layout.Rigid(gui.KV(t, "로그 파일", 120, path(info.LogPath, &p.copyLog))),
					layout.Rigid(gui.KV(t, "관리자 권한", 120, yesNo(info.Admin, "예", "아니요 (VPN 불가)"))),
					layout.Rigid(gui.KV(t, "wintun.dll", 120, yesNo(info.Wintun, "있음", "없음 (VPN 불가)"))),
					layout.Rigid(gui.KV(t, "라이선스", 120, t.Body("MIT License").Layout)))
			})
		})
}
