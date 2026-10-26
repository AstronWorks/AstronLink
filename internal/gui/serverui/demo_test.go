package serverui

import (
	"time"

	"github.com/AstronWorks/AstronLink/internal/config"
)

// demoBackend is an in-memory Backend for UI tests and previews. It records
// the calls the console makes.
type demoBackend struct {
	cfg       *config.ServerConfig
	state     State
	saved     *config.ServerConfig
	stopped   bool
	restarted bool
	autostart bool
}

func newDemo() *demoBackend {
	cfg := config.DefaultServerConfig()
	cfg.AuthToken = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"
	cfg.Insecure = true
	cfg.AllowPrivateTargets = true
	cfg.HTTPAddr = ":8787"
	cfg.Clients = []config.ClientTarget{
		{Name: "my-pc", Address: "192.168.1.221:8443"},
		{Name: "office-nas", Address: "10.20.0.15:8443"},
	}
	now := time.Now()
	return &demoBackend{cfg: cfg, state: State{
		Phase: Running, StartedAt: now.Add(-2*time.Hour - 13*time.Minute), Config: cfg,
		Clients: []Client{{ID: "c1", Name: "my-pc", Addr: "192.168.1.221:8443", ConnectedAt: now.Add(-47 * time.Minute), Tunnels: 3}},
		Tunnels: []Tunnel{
			{ID: "t1", ClientID: "c1", Client: "my-pc", Type: "tcp", Public: ":40022", Local: "127.0.0.1:22"},
			{ID: "t2", ClientID: "c1", Client: "my-pc", Type: "http", Public: "app.example.com", Local: "127.0.0.1:3000"},
			{ID: "t3", ClientID: "c1", Client: "my-pc", Type: "https", Public: "ag.example.com", Local: "127.0.0.1:8443"},
		},
		TotalConns: 4, ActiveConns: 1,
	}}
}

func (d *demoBackend) State() State                              { return d.state }
func (d *demoBackend) LoadConfig() (*config.ServerConfig, error) { return cloneConfig(d.cfg), nil }
func (d *demoBackend) SaveConfig(c *config.ServerConfig) error {
	d.saved, d.cfg = cloneConfig(c), cloneConfig(c)
	return nil
}
func (d *demoBackend) Start()                     {}
func (d *demoBackend) Stop()                      { d.stopped = true }
func (d *demoBackend) Restart()                   { d.restarted = true }
func (d *demoBackend) Reconnect(string) bool      { return true }
func (d *demoBackend) Autostart() bool            { return d.autostart }
func (d *demoBackend) SetAutostart(on bool) error { d.autostart = on; return nil }
func (d *demoBackend) Info() Info {
	return Info{Version: "v1.7.0", Commit: "532df19", Built: "2026-10-26 15:40",
		ConfigPath: `C:\Tools\AstronLink\server.yaml`, LogPath: `C:\Tools\AstronLink\server.log`}
}
func (d *demoBackend) ReadLog(int) ([]byte, error) {
	return []byte(`{"msg":"winserver starting","version":"v1.7.0","commit":"532df19"}
{"time":"2026-10-26T16:02:23.04+09:00","level":"INFO","msg":"starting server","component":"server","data_addr":":8444","http_addr":":8787","https_addr":":8445","admin_addr":":9090","client_targets":2,"vpn_enabled":true}
{"time":"2026-10-26T16:02:23.05+09:00","level":"INFO","msg":"data listener started","component":"server","addr":"[::]:8444"}
{"time":"2026-10-26T16:02:23.05+09:00","level":"INFO","msg":"HTTP proxy listener started","component":"server","addr":"[::]:8787"}
{"time":"2026-10-26T16:02:23.06+09:00","level":"INFO","msg":"dialing client","component":"server","name":"my-pc","addr":"192.168.1.221:8443"}
{"time":"2026-10-26T16:02:23.06+09:00","level":"INFO","msg":"dialing client","component":"server","name":"office-nas","addr":"10.20.0.15:8443"}
{"time":"2026-10-26T16:02:23.31+09:00","level":"INFO","msg":"connected to client","component":"server","name":"my-pc","addr":"192.168.1.221:8443"}
{"time":"2026-10-26T16:02:23.40+09:00","level":"INFO","msg":"VPN session opened","component":"server","name":"my-pc","client_addr":"10.77.0.2/24","mtu":1500}
{"time":"2026-10-26T16:02:38.07+09:00","level":"WARN","msg":"failed to dial client","component":"server","name":"office-nas","addr":"10.20.0.15:8443","err":"dial tcp 10.20.0.15:8443: i/o timeout"}
{"time":"2026-10-26T16:03:02.11+09:00","level":"WARN","msg":"client rejected registration: invalid token","component":"server","name":"office-nas"}
{"time":"2026-10-26T16:04:10.52+09:00","level":"INFO","msg":"tunnel registered","component":"server","client":"my-pc","type":"https","hostname":"ag.example.com"}
`), nil
}
