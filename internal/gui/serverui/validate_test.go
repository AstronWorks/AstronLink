package serverui

import (
	"testing"

	"github.com/EcoKG/reversproxy/internal/config"
)

func validConfig() *config.ServerConfig {
	cfg := config.DefaultServerConfig()
	cfg.AuthToken = "0123456789abcdef0123456789abcdef"
	cfg.Insecure = true
	cfg.Clients = []config.ClientTarget{{Name: "my-pc", Address: "192.168.1.221:8443"}}
	return cfg
}

func TestValidateAcceptsDefaults(t *testing.T) {
	if errs := validate(validConfig()); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateReportsFields(t *testing.T) {
	cfg := validConfig()
	cfg.AuthToken = ""
	cfg.DataAddr = "8444"
	cfg.HTTPAddr = ":8445" // clashes with https_addr
	cfg.Clients = append(cfg.Clients,
		config.ClientTarget{Name: "my-pc", Address: ":8443"},
		config.ClientTarget{Name: "", Address: "10.0.0.1:99999"})
	cfg.VPN.Subnet = "10.77.0.0/31"
	cfg.VPN.DNS = []string{"dns.example"}
	cfg.MaxConnBurst = -1

	errs := validate(cfg)
	for _, k := range []string{
		fAuthToken, fDataAddr, fHTTPSAddr,
		clientKey(fClientName, 1), clientKey(fClientAddr, 1),
		clientKey(fClientName, 2), clientKey(fClientAddr, 2),
		fVPNSubnet, fVPNDNS, fBurst,
	} {
		if errs[k] == "" {
			t.Errorf("expected an error for %s, got %v", k, errs)
		}
	}
	if errs[fHTTPAddr] != "" {
		t.Errorf("the first of two clashing listeners should not be flagged: %q", errs[fHTTPAddr])
	}
}

func TestValidateWeakTokenNeedsInsecure(t *testing.T) {
	cfg := validConfig()
	cfg.AuthToken = "changeme"
	if errs := validate(cfg); errs[fAuthToken] != "" {
		t.Fatalf("changeme should be tolerated in insecure mode: %v", errs)
	}
	cfg.Insecure = false
	if errs := validate(cfg); errs[fAuthToken] == "" {
		t.Fatal("changeme must be rejected with verification on")
	}
}
