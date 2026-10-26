package clientui

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/gui"
	"github.com/EcoKG/reversproxy/internal/vpn"
)

type fieldErrors map[string]string

// Field keys shared by the form and validate.
const (
	fName        = "name"
	fListen      = "listen_addr"
	fToken       = "auth_token"
	fSOCKSAddr   = "socks_addr"
	fSOCKSAuth   = "socks_auth"
	fHTTPAddr    = "http_proxy_addr"
	fTunnelHost  = "tunnel.%d.local_host"
	fTunnelPort  = "tunnel.%d.local_port"
	fTunnelPub   = "tunnel.%d.requested_port"
	fTunnelName  = "tunnel.%d.hostname"
	fPFLocal     = "pf.%d.local_port"
	fPFHost      = "pf.%d.remote_host"
	fPFRemote    = "pf.%d.remote_port"
	fPFBind      = "pf.%d.bind"
	fVPNRoutes   = "vpn.routes"
	fVPNBypass   = "vpn.bypass"
	fVPNDNS      = "vpn.dns"
	fVPNMTU      = "vpn.mtu"
	fVPNIface    = "vpn.interface_name"
	fFTAddr      = "ft.receive_addr"
	fFTDir       = "ft.drop_dir"
	fFTMax       = "ft.max_file_size"
	fFTEndpoint  = "ft.send_endpoint"
	fFTControl   = "ft.control_addr"
	fCertPath    = "cert_path"
	fKeyPath     = "key_path"
	tunnelPrefix = "tunnel."
	pfPrefix     = "pf."
)

func key(format string, i int) string { return fmt.Sprintf(format, i) }

func normType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "http":
		return "http"
	case "https":
		return "https"
	}
	return "tcp"
}

// validate checks cfg the way the client will at start-up, plus what the GUI
// promises on top (a name and token, no local port clashes).
func validate(cfg *config.ClientConfig) fieldErrors {
	errs := fieldErrors{}
	set := func(k, msg string) {
		if msg != "" && errs[k] == "" {
			errs[k] = msg
		}
	}

	if strings.TrimSpace(cfg.Name) == "" {
		set(fName, "이름을 입력하십시오 (서버 클라이언트 목록의 이름과 같아야 합니다)")
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		set(fToken, "서버 설정의 토큰과 같은 값을 입력하십시오")
	}
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		set(fListen, "대기 주소는 필수입니다 (예: :8443)")
	} else {
		set(fListen, gui.ListenAddrError(cfg.ListenAddr))
	}
	if cfg.SOCKSAddr != "" {
		set(fSOCKSAddr, gui.ListenAddrError(cfg.SOCKSAddr))
	}
	if (cfg.SOCKSUser == "") != (cfg.SOCKSPass == "") {
		set(fSOCKSAuth, "사용자와 비밀번호를 함께 입력하거나 둘 다 비우십시오")
	}
	if cfg.HTTPProxyAddr != "" {
		set(fHTTPAddr, gui.ListenAddrError(cfg.HTTPProxyAddr))
	}

	for i, t := range cfg.Tunnels {
		if strings.TrimSpace(t.LocalHost) == "" {
			set(key(fTunnelHost, i), "로컬 호스트를 입력하십시오 (예: 127.0.0.1)")
		}
		set(key(fTunnelPort, i), gui.PortError(t.LocalPort, false))
		switch normType(t.Type) {
		case "tcp":
			set(key(fTunnelPub, i), gui.PortError(t.RequestedPort, true))
		default:
			if strings.TrimSpace(t.Hostname) == "" {
				set(key(fTunnelName, i), "호스트 이름을 입력하십시오 (예: app.example.com)")
			} else if strings.ContainsAny(t.Hostname, " /:") {
				set(key(fTunnelName, i), "도메인 이름만 입력하십시오")
			}
		}
	}

	listeners := []struct{ key, addr string }{
		{fListen, cfg.ListenAddr}, {fSOCKSAddr, cfg.SOCKSAddr}, {fHTTPAddr, cfg.HTTPProxyAddr},
	}
	for i, pf := range cfg.PortForwards {
		set(key(fPFLocal, i), gui.PortError(pf.LocalPort, false))
		if strings.TrimSpace(pf.RemoteHost) == "" {
			set(key(fPFHost, i), "대상 호스트를 입력하십시오")
		}
		set(key(fPFRemote, i), gui.PortError(pf.RemotePort, false))
		if b := strings.TrimSpace(pf.Bind); b != "" && net.ParseIP(b) == nil {
			set(key(fPFBind, i), "IP 주소를 입력하십시오 (비우면 127.0.0.1)")
		}
		bind := pf.Bind
		if bind == "" {
			bind = "127.0.0.1"
		}
		listeners = append(listeners, struct{ key, addr string }{key(fPFLocal, i), net.JoinHostPort(bind, strconv.Itoa(pf.LocalPort))})
	}

	if cfg.VPN.Enabled {
		if _, err := vpn.ParsePrefixes(cfg.VPN.Routes); err != nil {
			set(fVPNRoutes, "CIDR 또는 IP 주소 목록이어야 합니다 (예: 10.0.0.0/8)")
		}
		if _, err := vpn.ParsePrefixes(cfg.VPN.Bypass); err != nil {
			set(fVPNBypass, "CIDR 또는 IP 주소 목록이어야 합니다 (예: 203.0.113.10)")
		}
		for _, d := range cfg.VPN.DNS {
			if _, err := netip.ParseAddr(strings.TrimSpace(d)); err != nil {
				set(fVPNDNS, fmt.Sprintf("%q 는 IP 주소가 아닙니다", d))
			}
		}
		if m := cfg.VPN.MTU; m != 0 && (m < vpn.MinMTU || m > vpn.MaxMTU) {
			set(fVPNMTU, fmt.Sprintf("%d–%d 사이여야 합니다 (0 은 서버 값)", vpn.MinMTU, vpn.MaxMTU))
		}
		if strings.ContainsAny(cfg.VPN.InterfaceName, `\/:*?"<>|`) {
			set(fVPNIface, "어댑터 이름에 쓸 수 없는 문자가 있습니다")
		}
	}

	ft := cfg.FileTransfer
	if ft.Enabled {
		if strings.TrimSpace(ft.ReceiveAddr) == "" {
			set(fFTAddr, "수신 주소를 입력하십시오 (예: 127.0.0.1:8089)")
		} else {
			set(fFTAddr, gui.ListenAddrError(ft.ReceiveAddr))
		}
		if strings.TrimSpace(ft.DropDir) == "" {
			set(fFTDir, "저장 폴더를 입력하십시오")
		}
		if ft.MaxFileSize < 0 {
			set(fFTMax, "0 이상이어야 합니다")
		}
		if ft.SendEndpoint != "" {
			if u, err := url.Parse(ft.SendEndpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				set(fFTEndpoint, "http:// 로 시작하는 주소여야 합니다 (예: http://127.0.0.1:8090)")
			}
		}
		if ft.ControlAddr != "" {
			set(fFTControl, gui.ListenAddrError(ft.ControlAddr))
		}
		listeners = append(listeners,
			struct{ key, addr string }{fFTAddr, ft.ReceiveAddr},
			struct{ key, addr string }{fFTControl, ft.ControlAddr})
	}
	for i, a := range listeners {
		if strings.TrimSpace(a.addr) == "" {
			continue
		}
		for _, b := range listeners[:i] {
			if strings.TrimSpace(b.addr) != "" {
				set(a.key, gui.PortClash(a.addr, b.addr))
			}
		}
	}

	if strings.TrimSpace(cfg.CertPath) == "" {
		set(fCertPath, "인증서 파일 경로를 입력하십시오 (없으면 자동 생성)")
	}
	if strings.TrimSpace(cfg.KeyPath) == "" {
		set(fKeyPath, "키 파일 경로를 입력하십시오 (없으면 자동 생성)")
	}
	return errs
}
