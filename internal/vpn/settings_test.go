package vpn

import (
	"strings"
	"testing"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/protocol"
)

func TestServerSettingsFromConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config.VPNServerConfig
		wantErr string
	}{
		{"defaults", config.DefaultVPNServerConfig(), ""},
		{"disabled skips validation", config.VPNServerConfig{Enabled: false, Subnet: "garbage"}, ""},
		{"bad subnet", config.VPNServerConfig{Enabled: true, Subnet: "10.77.0.0"}, "vpn.subnet"},
		{"ipv6 subnet", config.VPNServerConfig{Enabled: true, Subnet: "fd00::/64"}, "only IPv4"},
		{"too small", config.VPNServerConfig{Enabled: true, Subnet: "10.0.0.0/31"}, "/30"},
		{"bad mtu", config.VPNServerConfig{Enabled: true, Subnet: "10.0.0.0/24", MTU: 100}, "vpn.mtu"},
		{"bad dns", config.VPNServerConfig{Enabled: true, Subnet: "10.0.0.0/24", DNS: []string{"x"}}, "vpn.dns"},
		{"custom dns", config.VPNServerConfig{Enabled: true, Subnet: "10.0.0.0/24", DNS: []string{"1.1.1.1"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ServerSettingsFromConfig(tc.cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v; want containing %q (settings %+v)", err, tc.wantErr, s)
			}
		})
	}

	s, _ := ServerSettingsFromConfig(config.VPNServerConfig{Enabled: true, Subnet: "10.9.8.77/24", DNS: []string{"1.1.1.1"}})
	if s.Gateway.String() != "10.9.8.1" || s.ClientAddr.String() != "10.9.8.2/24" || s.BuiltinDNS || s.MTU != DefaultMTU {
		t.Fatalf("derived settings = %+v", s)
	}
}

func TestInstallRoutes(t *testing.T) {
	got := InstallRoutes(nil)
	if len(got) != 2 || got[0].String() != "0.0.0.0/1" || got[1].String() != "128.0.0.0/1" {
		t.Fatalf("full tunnel routes = %v", got)
	}
	routes, err := ParsePrefixes([]string{"10.1.2.3/16", "192.0.2.9", "10.1.0.0/16"})
	if err != nil {
		t.Fatalf("ParsePrefixes: %v", err)
	}
	got = InstallRoutes(routes)
	if len(got) != 2 || got[0].String() != "10.1.0.0/16" || got[1].String() != "192.0.2.9/32" {
		t.Fatalf("split routes = %v", got)
	}
	if IsFullTunnel(routes) || !IsFullTunnel(nil) {
		t.Fatal("isFullTunnel misclassified")
	}
}

func TestParamsFromConfig(t *testing.T) {
	ok := protocol.VPNConfig{Status: "ok", ClientAddr: "10.77.0.2/24", Gateway: "10.77.0.1", MTU: 1500, DNS: []string{"10.77.0.1"}, DataPort: 8444}
	p, err := ParamsFromConfig(ok)
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if p.Address.String() != "10.77.0.2/24" || p.Gateway.String() != "10.77.0.1" || p.DataPort != 8444 {
		t.Fatalf("params = %+v", p)
	}
	if !p.EqualAddressing(p) {
		t.Fatal("EqualAddressing(self) = false")
	}

	bad := []protocol.VPNConfig{
		{Status: "error", Error: "vpn disabled"},
		{Status: "ok", ClientAddr: "nope", Gateway: "10.77.0.1", MTU: 1500},
		{Status: "ok", ClientAddr: "10.77.0.2/24", Gateway: "10.88.0.1", MTU: 1500},
		{Status: "ok", ClientAddr: "10.77.0.2/24", Gateway: "10.77.0.2", MTU: 1500},
		{Status: "ok", ClientAddr: "10.77.0.2/24", Gateway: "10.77.0.1", MTU: 10},
		{Status: "ok", ClientAddr: "10.77.0.2/24", Gateway: "10.77.0.1", MTU: 1500, DNS: []string{"bad"}},
	}
	for i, vc := range bad {
		if _, err := ParamsFromConfig(vc); err == nil {
			t.Errorf("case %d (%+v): accepted; want error", i, vc)
		}
	}
}
