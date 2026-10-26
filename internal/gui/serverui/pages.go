package serverui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/gui"
	"github.com/EcoKG/reversproxy/internal/gui/logview"
)

// clientView merges a configured client target with its live connection.
type clientView struct {
	Name, Addr string
	Configured bool
	Token      string // effective token sent to this client
	Conn       *Client
}

func (u *UI) clientViews() []clientView {
	cfg := u.cfg()
	used := map[string]bool{}
	var out []clientView
	for _, ct := range cfg.Clients {
		cv := clientView{Name: ct.Name, Addr: ct.Address, Configured: true, Token: ct.AuthToken}
		if cv.Token == "" {
			cv.Token = cfg.AuthToken
		}
		for i := range u.state.Clients {
			c := &u.state.Clients[i]
			if c.Name == ct.Name && !used[c.ID] {
				cv.Conn, used[c.ID] = c, true
				break
			}
		}
		out = append(out, cv)
	}
	for i := range u.state.Clients {
		c := &u.state.Clients[i]
		if !used[c.ID] {
			out = append(out, clientView{Name: c.Name, Addr: c.Addr, Conn: c})
		}
	}
	return out
}

func (u *UI) clientStatus(cv clientView) (gui.Kind, string) {
	switch {
	case cv.Conn != nil:
		return gui.KindSuccess, "연결됨"
	case u.state.Phase == Running:
		return gui.KindWarning, "접속 시도 중"
	}
	return gui.KindNeutral, "대기"
}

// displayListen shows where a listener really binds (":9090" admin binds to
// loopback only).
func displayListen(addr string, loopbackDefault bool) string {
	if addr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" && loopbackDefault {
		return "127.0.0.1:" + port
	}
	if host == "" {
		return "모든 주소 :" + port
	}
	return addr
}

func weakToken(s string) bool { s = strings.TrimSpace(s); return s == "" || s == "changeme" }

// ---------------------------------------------------------------- overview

type overview struct {
	list                                    widget.List
	toSettings, addClient, genToken, toLogs widget.Clickable
	toClients                               widget.Clickable
	copyAddr                                [4]widget.Clickable
}

func (o *overview) layout(gtx C, u *UI) D {
	t := u.th
	st := u.state
	cfg := u.cfg()
	if o.toSettings.Clicked(gtx) {
		u.page = PageSettings
	}
	if o.addClient.Clicked(gtx) {
		u.page = PageSettings
		u.set.tab.Value = tabConn
		u.set.addClient(config.ClientTarget{})
	}
	if o.genToken.Clicked(gtx) {
		u.page = PageSettings
		u.set.tab.Value = tabConn
		u.set.authToken.SetText(newToken())
	}
	if o.toLogs.Clicked(gtx) {
		u.page = PageLogs
	}
	if o.toClients.Clicked(gtx) {
		u.page = PageClients
	}

	var items []layout.Widget
	if st.Phase == Failed {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindDanger, "서버를 시작하지 못했습니다", st.Err,
				layout.Rigid(t.Btn(&o.toSettings, gui.BtnSecondary, "설정 열기", nil).Layout))
		})
	}
	if u.loadErr != "" {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindDanger, "server.yaml 을 읽지 못해 기본값을 표시합니다", u.loadErr)
		})
	}
	if len(cfg.Clients) == 0 {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindWarning, "접속할 클라이언트가 없습니다",
				"서버는 등록된 클라이언트 주소로 직접 접속합니다(역방향). 클라이언트 이름과 주소를 추가하십시오.",
				layout.Rigid(t.Btn(&o.addClient, gui.BtnPrimary, "클라이언트 추가", gui.IconAdd).Layout))
		})
	}
	if weakToken(cfg.AuthToken) {
		items = append(items, func(gtx C) D {
			return t.Banner(gtx, gui.KindWarning, "인증 토큰이 비어 있거나 기본값입니다",
				"무작위 토큰을 만들어 저장한 뒤, 클라이언트 설정에도 같은 토큰을 넣으십시오.",
				layout.Rigid(t.Btn(&o.genToken, gui.BtnSecondary, "토큰 생성", gui.IconGenerate).Layout))
		})
	}

	items = append(items, func(gtx C) D {
		k, label := phaseView(st.Phase)
		sub := "—"
		if st.Phase == Running {
			sub = "가동 " + gui.HumanDuration(gtx.Now.Sub(st.StartedAt))
		}
		var tcp, http, https int
		for _, tn := range st.Tunnels {
			switch tn.Type {
			case "http":
				http++
			case "https":
				https++
			default:
				tcp++
			}
		}
		return gui.Grid(gtx, 210, 16,
			func(gtx C) D { return t.StatTile(gtx, gui.IconPower, k, "서버", label, sub) },
			func(gtx C) D {
				return t.StatTile(gtx, gui.IconPeople, gui.KindPrimary, "클라이언트",
					fmt.Sprintf("%d / %d", len(st.Clients), len(cfg.Clients)), "연결됨 / 등록됨")
			},
			func(gtx C) D {
				return t.StatTile(gtx, gui.IconSwap, gui.KindInfo, "활성 터널", strconv.Itoa(len(st.Tunnels)),
					fmt.Sprintf("TCP %d · HTTP %d · HTTPS %d", tcp, http, https))
			},
			func(gtx C) D {
				return t.StatTile(gtx, gui.IconTimeline, gui.KindWarning, "제어 연결", strconv.FormatInt(st.ActiveConns, 10),
					fmt.Sprintf("프로그램 실행 후 누적 %d회", st.TotalConns))
			})
	})
	items = append(items, func(gtx C) D {
		return gui.Grid(gtx, 400, 16,
			func(gtx C) D { return o.listeners(gtx, u, cfg) },
			func(gtx C) D { return o.clients(gtx, u) })
	})
	items = append(items, func(gtx C) D {
		return gui.Grid(gtx, 400, 16,
			func(gtx C) D { return o.security(gtx, u, cfg) },
			func(gtx C) D { return o.recent(gtx, u) })
	})
	return u.scroll(gtx, &o.list, items...)
}

func (o *overview) listeners(gtx C, u *UI, cfg *config.ServerConfig) D {
	t := u.th
	rows := []struct{ name, desc, addr string }{
		{"데이터 연결", "클라이언트가 터널 데이터 연결을 엽니다", displayListen(cfg.DataAddr, false)},
		{"HTTP 프록시", "Host 헤더로 HTTP 터널에 분배", displayListen(cfg.HTTPAddr, false)},
		{"HTTPS 프록시", "SNI 로 HTTPS 터널에 분배", displayListen(cfg.HTTPSAddr, false)},
		{"관리 API", "JSON API · 웹 대시보드", displayListen(cfg.AdminAddr, true)},
	}
	return t.Card(gtx, func(gtx C) D {
		children := []layout.FlexChild{layout.Rigid(gui.CardHeader(t, "리스너", nil)), gui.VSpace(6)}
		for i, r := range rows {
			children = append(children, layout.Rigid(func(gtx C) D {
				if o.copyAddr[i].Clicked(gtx) {
					_, port, _ := net.SplitHostPort(strings.TrimPrefix(r.addr, "모든 주소 "))
					gui.CopyText(gtx, port)
					u.toast.Show("포트 번호를 복사했습니다", gui.KindSuccess)
				}
				return layout.Inset{Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(t.Label(13.5, r.name, t.P.Text, font.Medium).Layout),
								layout.Rigid(t.Caption(r.desc).Layout))
						}),
						layout.Rigid(func(gtx C) D {
							if r.addr == "" {
								return t.Badge(gtx, gui.KindNeutral, "사용 안 함")
							}
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(t.Code(r.addr).Layout),
								gui.HSpace(4),
								layout.Rigid(t.Btn(&o.copyAddr[i], gui.BtnGhost, "", gui.IconCopy).WithSmall().Layout))
						}))
				})
			}))
			if i < len(rows)-1 {
				children = append(children, layout.Rigid(t.Divider))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (o *overview) clients(gtx C, u *UI) D {
	t := u.th
	views := u.clientViews()
	return t.Card(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(gui.CardHeader(t, "클라이언트 연결", t.Btn(&o.toClients, gui.BtnGhost, "모두 보기", gui.IconChevron).WithSmall().Layout)),
			gui.VSpace(6),
		}
		if len(views) == 0 {
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 10}.Layout(gtx, t.Muted("등록된 클라이언트가 없습니다.").Layout)
			}))
		}
		for i, cv := range views {
			if i == 6 {
				break
			}
			if i > 0 {
				children = append(children, layout.Rigid(t.Divider))
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				k, label := u.clientStatus(cv)
				fg, _ := t.KindColors(k)
				return layout.Inset{Top: 11, Bottom: 11}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return t.Dot(gtx, fg, 8) }),
						gui.HSpace(12),
						layout.Flexed(1, func(gtx C) D {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(t.Label(13.5, cv.Name, t.P.Text, font.Medium).Layout),
								layout.Rigid(func(gtx C) D {
									l := t.Code(cv.Addr)
									l.TextSize, l.Color = 12, t.P.TextMuted
									return l.Layout(gtx)
								}))
						}),
						layout.Rigid(func(gtx C) D {
							s := label
							if cv.Conn != nil {
								s += " · " + gui.HumanDuration(gtx.Now.Sub(cv.Conn.ConnectedAt))
							}
							return t.Label(12.5, s, fg, font.Medium).Layout(gtx)
						}))
				})
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (o *overview) security(gtx C, u *UI, cfg *config.ServerConfig) D {
	t := u.th
	vpnText := "사용 안 함"
	if cfg.VPN.Enabled {
		mtu := cfg.VPN.MTU
		if mtu == 0 {
			mtu = 1500
		}
		vpnText = fmt.Sprintf("%s · MTU %d", cfg.VPN.Subnet, mtu)
	}
	dns := "서버 내장 리졸버"
	if len(cfg.VPN.DNS) > 0 {
		dns = strings.Join(cfg.VPN.DNS, ", ")
	}
	limit := "제한 없음"
	if cfg.MaxConnRate > 0 || cfg.MaxConcurrent > 0 {
		limit = fmt.Sprintf("초당 %g · 동시 %d", cfg.MaxConnRate, cfg.MaxConcurrent)
	}
	text := func(s string) layout.Widget { return t.Body(s).Layout }
	rows := []struct {
		key string
		w   layout.Widget
	}{
		{"인증서 검증", gui.OnOffBadge(t, !cfg.Insecure, "켜짐", "생략 (insecure)", gui.KindSuccess, gui.KindWarning)},
		{"사설 주소 접근", gui.OnOffBadge(t, cfg.AllowPrivateTargets, "허용", "차단", gui.KindWarning, gui.KindSuccess)},
		{"관리 API 인증", gui.OnOffBadge(t, cfg.AdminToken != "", "토큰 필요", "없음", gui.KindSuccess, gui.KindNeutral)},
		{"VPN", text(vpnText)},
		{"VPN DNS", text(dns)},
		{"연결 제한", text(limit)},
		{"로그 수준", text(levelOf(cfg.LogLevel))},
	}
	return t.Card(gtx, func(gtx C) D {
		children := []layout.FlexChild{layout.Rigid(gui.CardHeader(t, "보안 · VPN", nil)), gui.VSpace(8)}
		for _, r := range rows {
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 7, Bottom: 7}.Layout(gtx, func(gtx C) D {
					return t.KeyValue(gtx, r.key, 120, r.w)
				})
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
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

// ---------------------------------------------------------------- clients

type clientsPage struct {
	list      widget.List
	table     gui.Table
	add       widget.Clickable
	reconnect []widget.Clickable
	copyTok   []widget.Clickable
}

func (p *clientsPage) layout(gtx C, u *UI) D {
	t := u.th
	views := u.clientViews()
	for len(p.reconnect) < len(views) {
		p.reconnect = append(p.reconnect, widget.Clickable{})
		p.copyTok = append(p.copyTok, widget.Clickable{})
	}
	if p.add.Clicked(gtx) {
		u.page = PageSettings
		u.set.tab.Value = tabConn
		u.set.addClient(config.ClientTarget{})
	}
	for i, cv := range views {
		if p.copyTok[i].Clicked(gtx) {
			gui.CopyText(gtx, cv.Token)
			u.toast.Show(cv.Name+" 의 토큰을 복사했습니다", gui.KindSuccess)
		}
		if p.reconnect[i].Clicked(gtx) && cv.Conn != nil {
			if u.be.Reconnect(cv.Conn.ID) {
				u.toast.Show(cv.Name+" 연결을 끊었습니다. 잠시 후 다시 접속합니다", gui.KindInfo)
			}
		}
	}
	cols := []gui.Column{
		{Title: "이름", Weight: 1.1}, {Title: "주소", Weight: 1.3}, {Title: "상태", Weight: 1},
		{Title: "연결 시각", Weight: 1.1}, {Title: "터널", Width: 56}, {Title: "", Width: 236},
	}
	connected := len(u.state.Clients)
	return u.scroll(gtx, &p.list,
		func(gtx C) D {
			return t.Banner(gtx, gui.KindInfo, "서버가 클라이언트에 접속합니다 (역방향)",
				"클라이언트 PC 의 reversproxy-client 설정에서 name, listen_addr 포트, auth_token 이 아래 값과 같아야 합니다. [토큰 복사]로 값을 옮기십시오.")
		},
		func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return t.SectionTitle(gtx, "클라이언트 목록", fmt.Sprintf("등록 %d개 · 연결 %d개", len(u.cfg().Clients), connected))
				}),
				layout.Rigid(t.Btn(&p.add, gui.BtnPrimary, "클라이언트 추가", gui.IconAdd).Layout))
		},
		func(gtx C) D {
			return p.table.Layout(gtx, t, cols, len(views), func(gtx C, row, col int) D {
				cv := views[row]
				switch col {
				case 0:
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(t.Label(14, cv.Name, t.P.Text, font.SemiBold).Layout),
						layout.Rigid(func(gtx C) D {
							if cv.Configured {
								return D{}
							}
							return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D { return t.Badge(gtx, gui.KindNeutral, "미등록") })
						}))
				case 1:
					return t.Code(cv.Addr).Layout(gtx)
				case 2:
					k, label := u.clientStatus(cv)
					return t.StatusPill(gtx, k, label)
				case 3:
					if cv.Conn == nil {
						return t.Muted("—").Layout(gtx)
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(t.Body(cv.Conn.ConnectedAt.Format("01-02 15:04:05")).Layout),
						layout.Rigid(t.Caption(gui.HumanDuration(gtx.Now.Sub(cv.Conn.ConnectedAt))+" 전").Layout))
				case 4:
					if cv.Conn == nil {
						return t.Muted("—").Layout(gtx)
					}
					return t.Body(strconv.Itoa(cv.Conn.Tunnels)).Layout(gtx)
				case 5:
					return layout.Flex{}.Layout(gtx,
						layout.Rigid(t.Btn(&p.copyTok[row], gui.BtnSecondary, "토큰 복사", gui.IconCopy).WithSmall().WithDisabled(cv.Token == "").Layout),
						gui.HSpace(6),
						layout.Rigid(t.Btn(&p.reconnect[row], gui.BtnGhost, "재연결", gui.IconRefresh).WithSmall().WithDisabled(cv.Conn == nil).Layout))
				}
				return D{}
			}, func(gtx C) D {
				return t.EmptyState(gtx, gui.IconPeople, "등록된 클라이언트가 없습니다", "[클라이언트 추가]로 이름과 주소(예: 192.168.1.10:8443)를 등록하십시오.")
			})
		})
}

// ---------------------------------------------------------------- tunnels

type tunnelsPage struct {
	list  widget.List
	table gui.Table
	copy  []widget.Clickable
}

func (p *tunnelsPage) layout(gtx C, u *UI) D {
	t := u.th
	tunnels := u.state.Tunnels
	for len(p.copy) < len(tunnels) {
		p.copy = append(p.copy, widget.Clickable{})
	}
	cfg := u.cfg()
	for i, tn := range tunnels {
		if p.copy[i].Clicked(gtx) {
			gui.CopyText(gtx, strings.TrimPrefix(tn.Public, ":"))
			u.toast.Show("복사했습니다", gui.KindSuccess)
		}
	}
	cols := []gui.Column{
		{Title: "유형", Width: 84}, {Title: "공개 주소", Weight: 1.4}, {Title: "클라이언트", Weight: 1},
		{Title: "로컬 대상", Weight: 1.2}, {Title: "", Width: 48},
	}
	return u.scroll(gtx, &p.list, func(gtx C) D {
		return p.table.Layout(gtx, t, cols, len(tunnels), func(gtx C, row, col int) D {
			tn := tunnels[row]
			switch col {
			case 0:
				k := gui.KindInfo
				switch tn.Type {
				case "http":
					k = gui.KindPrimary
				case "https":
					k = gui.KindSuccess
				}
				return t.Badge(gtx, k, strings.ToUpper(tn.Type))
			case 1:
				via := "TCP 공개 포트"
				switch tn.Type {
				case "http":
					via = "HTTP 프록시 " + displayListen(cfg.HTTPAddr, false)
				case "https":
					via = "HTTPS 프록시 " + displayListen(cfg.HTTPSAddr, false)
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(t.Code(tn.Public).Layout),
					layout.Rigid(t.Caption(via).Layout))
			case 2:
				return t.Body(tn.Client).Layout(gtx)
			case 3:
				return t.Code(tn.Local).Layout(gtx)
			case 4:
				return t.Btn(&p.copy[row], gui.BtnGhost, "", gui.IconCopy).WithSmall().Layout(gtx)
			}
			return D{}
		}, func(gtx C) D {
			return t.EmptyState(gtx, gui.IconSwap, "활성 터널이 없습니다", "클라이언트 설정의 터널이 등록되면 여기에 표시됩니다.")
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
	info := u.be.Info()
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
	return u.scroll(gtx, &p.list,
		func(gtx C) D {
			return t.Card(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return t.Logo(gtx, 52, gui.RGB(0x6366F1), gui.RGB(0x9333EA), gui.IconDNS)
							}),
							gui.HSpace(16),
							layout.Rigid(func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(t.Label(19, "Reversproxy Server", t.P.Text, font.SemiBold).Layout),
									layout.Rigid(t.Muted("역방향 TLS 터널 · SOCKS5 출구 · VPN 서버").Layout))
							}))
					}),
					gui.VSpace(18),
					layout.Rigid(t.Divider),
					gui.VSpace(10),
					layout.Rigid(gui.KV(t, "버전", 110, t.Body(info.Version).Layout)),
					layout.Rigid(gui.KV(t, "커밋", 110, t.Code(info.Commit).Layout)),
					layout.Rigid(gui.KV(t, "빌드 시각", 110, t.Body(info.Built).Layout)),
					layout.Rigid(gui.KV(t, "설정 파일", 110, path(info.ConfigPath, &p.copyCfg))),
					layout.Rigid(gui.KV(t, "로그 파일", 110, path(info.LogPath, &p.copyLog))),
					layout.Rigid(gui.KV(t, "라이선스", 110, t.Body("MIT License").Layout)))
			})
		},
		func(gtx C) D {
			return t.Card(gtx, func(gtx C) D {
				steps := []string{
					"1. 클라이언트 PC 에서 reversproxy-client 가 listen_addr(기본 :8443)로 대기합니다.",
					"2. 이 서버가 설정의 클라이언트 주소로 TLS 1.3 연결을 열고 인증 토큰을 보냅니다.",
					"3. 토큰이 맞으면 터널·SOCKS5·VPN 트래픽이 이 연결로 오갑니다.",
					"4. 설정 탭에서 저장하면 server.yaml 에 기록되고 서버가 다시 시작됩니다.",
				}
				children := []layout.FlexChild{layout.Rigid(t.Heading("동작 방식").Layout), gui.VSpace(10)}
				for _, s := range steps {
					children = append(children, layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, t.Label(13.5, s, t.P.TextMuted, font.Normal).Layout)
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			})
		})
}

func levelOf(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "warn", "error":
		return strings.ToLower(strings.TrimSpace(s))
	}
	return "info"
}
