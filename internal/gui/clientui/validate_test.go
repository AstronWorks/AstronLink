package clientui

import (
	"testing"

	"github.com/AstronWorks/AstronLink/internal/config"
)

func validConfig() *config.ClientConfig {
	cfg := config.DefaultClientConfig()
	cfg.Name = "my-pc"
	cfg.AuthToken = "0123456789abcdef0123456789abcdef"
	cfg.Tunnels = []config.TunnelConfig{
		{Type: "tcp", LocalHost: "127.0.0.1", LocalPort: 22, RequestedPort: 40022},
		{Type: "https", LocalHost: "127.0.0.1", LocalPort: 8443, Hostname: "app.example.com"},
	}
	cfg.PortForwards = []config.PortForwardConfig{{LocalPort: 15432, RemoteHost: "db.internal", RemotePort: 5432}}
	cfg.VPN = config.VPNClientConfig{Enabled: true, Routes: []string{"10.0.0.0/8"}, Bypass: []string{"203.0.113.10"}}
	return cfg
}

func TestValidateAccepts(t *testing.T) {
	if errs := validate(validConfig()); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateReports(t *testing.T) {
	cfg := validConfig()
	cfg.Name = ""
	cfg.AuthToken = ""
	cfg.SOCKSUser = "u"
	cfg.HTTPProxyAddr = "127.0.0.1:1080" // clashes with socks_addr
	cfg.Tunnels[0].LocalPort = 0
	cfg.Tunnels[1].Hostname = ""
	cfg.PortForwards[0].Bind = "lan"
	cfg.VPN.Routes = []string{"10.0.0.0/33"}
	cfg.VPN.MTU = 100
	cfg.FileTransfer.Enabled = true
	cfg.FileTransfer.SendEndpoint = "ftp://x"

	errs := validate(cfg)
	for _, k := range []string{
		fName, fToken, fSOCKSAuth, fHTTPAddr,
		key(fTunnelPort, 0), key(fTunnelName, 1), key(fPFBind, 0),
		fVPNRoutes, fVPNMTU, fFTEndpoint,
	} {
		if errs[k] == "" {
			t.Errorf("expected an error for %s; got %v", k, errs)
		}
	}
}
