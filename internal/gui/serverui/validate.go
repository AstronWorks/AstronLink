package serverui

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/gui"
	"github.com/AstronWorks/AstronLink/internal/vpn"
)

// fieldErrors maps a settings field key to its validation message.
type fieldErrors map[string]string

// Field keys shared by the form and validate.
const (
	fAuthToken   = "auth_token"
	fDataAddr    = "data_addr"
	fHTTPAddr    = "http_addr"
	fHTTPSAddr   = "https_addr"
	fAdminAddr   = "admin_addr"
	fAdminToken  = "admin_token"
	fClientCA    = "client_ca_cert"
	fVPNSubnet   = "vpn.subnet"
	fVPNMTU      = "vpn.mtu"
	fVPNDNS      = "vpn.dns"
	fRate        = "max_conn_rate"
	fBurst       = "max_conn_burst"
	fConcurrent  = "max_concurrent"
	fFTAddr      = "ft.receive_addr"
	fFTDir       = "ft.drop_dir"
	fFTMax       = "ft.max_file_size"
	fClientName  = "client.%d.name"
	fClientAddr  = "client.%d.address"
	fClientToken = "client.%d.token"
)

func clientKey(format string, i int) string { return fmt.Sprintf(format, i) }

// validate checks cfg the way the server will at start-up and reports the
// problems per field. It also enforces what the GUI promises on top of that
// (a non-empty token, unique client names, no listener port clashes).
func validate(cfg *config.ServerConfig) fieldErrors {
	errs := fieldErrors{}
	set := func(k, msg string) {
		if msg != "" && errs[k] == "" {
			errs[k] = msg
		}
	}

	token := strings.TrimSpace(cfg.AuthToken)
	switch {
	case token == "":
		set(fAuthToken, "토큰을 입력하거나 [생성]을 누르십시오")
	case token == "changeme" && !cfg.Insecure:
		set(fAuthToken, "기본값 changeme 는 인증서 검증을 켠 상태에서 쓸 수 없습니다")
	}

	if strings.TrimSpace(cfg.DataAddr) == "" {
		set(fDataAddr, "데이터 리스너 주소는 필수입니다 (예: :8444)")
	} else {
		set(fDataAddr, gui.ListenAddrError(cfg.DataAddr))
	}
	listeners := []struct{ key, addr string }{
		{fDataAddr, cfg.DataAddr}, {fHTTPAddr, cfg.HTTPAddr}, {fHTTPSAddr, cfg.HTTPSAddr}, {fAdminAddr, cfg.AdminAddr},
	}
	for _, l := range listeners[1:] {
		if strings.TrimSpace(l.addr) != "" {
			set(l.key, gui.ListenAddrError(l.addr))
		}
	}
	if cfg.FileTransfer.Enabled {
		listeners = append(listeners, struct{ key, addr string }{fFTAddr, cfg.FileTransfer.ReceiveAddr})
	}
	for i, a := range listeners {
		for _, b := range listeners[:i] {
			set(a.key, gui.PortClash(a.addr, b.addr))
		}
	}

	names := map[string]int{}
	for i, cl := range cfg.Clients {
		name := strings.TrimSpace(cl.Name)
		switch {
		case name == "":
			set(clientKey(fClientName, i), "이름을 입력하십시오 (클라이언트 설정의 name 과 같아야 합니다)")
		case names[name] > 0:
			set(clientKey(fClientName, i), "같은 이름의 클라이언트가 이미 있습니다")
		}
		names[name]++
		set(clientKey(fClientAddr, i), gui.DialAddrError(cl.Address))
		if strings.TrimSpace(cl.AuthToken) == "changeme" && !cfg.Insecure {
			set(clientKey(fClientToken, i), "기본값 changeme 는 쓸 수 없습니다")
		}
	}

	if cfg.VPN.Enabled {
		p, err := netip.ParsePrefix(strings.TrimSpace(cfg.VPN.Subnet))
		switch {
		case err != nil:
			set(fVPNSubnet, "CIDR 형식이어야 합니다 (예: 10.77.0.0/24)")
		case !p.Addr().Is4():
			set(fVPNSubnet, "IPv4 대역만 지원합니다")
		case p.Bits() > 30:
			set(fVPNSubnet, "/30 이하여야 합니다 (게이트웨이와 클라이언트 주소 필요)")
		}
		if m := cfg.VPN.MTU; m != 0 && (m < vpn.MinMTU || m > vpn.MaxMTU) {
			set(fVPNMTU, fmt.Sprintf("%d–%d 사이여야 합니다", vpn.MinMTU, vpn.MaxMTU))
		}
		for _, d := range cfg.VPN.DNS {
			if _, err := netip.ParseAddr(strings.TrimSpace(d)); err != nil {
				set(fVPNDNS, fmt.Sprintf("%q 는 IP 주소가 아닙니다", d))
			}
		}
		if len(errs) == 0 {
			if _, err := vpn.ServerSettingsFromConfig(cfg.VPN); err != nil {
				set(fVPNSubnet, err.Error())
			}
		}
	}

	if cfg.MaxConnRate < 0 {
		set(fRate, "0 이상이어야 합니다")
	}
	if cfg.MaxConnBurst < 0 {
		set(fBurst, "0 이상이어야 합니다")
	}
	if cfg.MaxConcurrent < 0 {
		set(fConcurrent, "0 이상이어야 합니다")
	}

	if cfg.FileTransfer.Enabled {
		if strings.TrimSpace(cfg.FileTransfer.ReceiveAddr) == "" {
			set(fFTAddr, "수신 주소를 입력하십시오 (예: 127.0.0.1:8089)")
		} else {
			set(fFTAddr, gui.ListenAddrError(cfg.FileTransfer.ReceiveAddr))
		}
		if strings.TrimSpace(cfg.FileTransfer.DropDir) == "" {
			set(fFTDir, "저장 폴더를 입력하십시오")
		}
		if cfg.FileTransfer.MaxFileSize < 0 {
			set(fFTMax, "0 이상이어야 합니다")
		}
	}
	return errs
}
