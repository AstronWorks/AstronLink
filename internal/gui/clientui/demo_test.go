package clientui

import (
	"time"

	"github.com/AstronWorks/AstronLink/internal/config"
)

// demoBackend is an in-memory Backend for UI tests and previews.
type demoBackend struct {
	cfg          *config.ClientConfig
	state        State
	saved        *config.ClientConfig
	disconnected bool
	autostart    bool
	ctxMenu      bool
}

func newDemo() *demoBackend {
	cfg := validConfig()
	cfg.FileTransfer.Enabled = true
	cfg.FileTransfer.SendEndpoint = "http://127.0.0.1:8090"
	now := time.Now()
	return &demoBackend{cfg: cfg, state: State{
		Phase: Connected, Since: now.Add(-3*time.Hour - 2*time.Minute), Config: cfg,
		Servers:   []Server{{Addr: "192.168.1.20:52114", ConnectedAt: now.Add(-48 * time.Minute)}},
		VPN:       VPNState{Active: true, Interface: "AstronLink", Address: "10.77.0.2/24", Gateway: "10.77.0.1"},
		SOCKSAddr: "127.0.0.1:1080", HTTPAddr: "127.0.0.1:8080", FileReceiver: "127.0.0.1:8089",
	}}
}

func (d *demoBackend) State() State                              { return d.state }
func (d *demoBackend) LoadConfig() (*config.ClientConfig, error) { return cloneConfig(d.cfg), nil }
func (d *demoBackend) SaveConfig(c *config.ClientConfig) error {
	d.saved, d.cfg = cloneConfig(c), cloneConfig(c)
	return nil
}
func (d *demoBackend) Connect()                     {}
func (d *demoBackend) Disconnect()                  { d.disconnected = true }
func (d *demoBackend) Reconnect()                   {}
func (d *demoBackend) Autostart() bool              { return d.autostart }
func (d *demoBackend) SetAutostart(on bool) error   { d.autostart = on; return nil }
func (d *demoBackend) ContextMenu() bool            { return d.ctxMenu }
func (d *demoBackend) SetContextMenu(on bool) error { d.ctxMenu = on; return nil }
func (d *demoBackend) OpenDropFolder() error        { return nil }
func (d *demoBackend) Info() Info {
	return Info{Version: "v1.7.0", Commit: "532df19", Built: "2026-10-26 15:40",
		ConfigPath: `C:\Tools\AstronLink\config.yaml`, LogPath: `C:\Tools\AstronLink\winclient.log`,
		LocalIPs: []string{"192.168.1.221"}, Admin: true, Wintun: true}
}
func (d *demoBackend) ReadLog(int) ([]byte, error) {
	return []byte(`{"msg":"winclient starting","version":"v1.7.0"}
{"time":"2026-10-26T16:02:20.10+09:00","level":"INFO","msg":"client SOCKS5 listener started","component":"winclient","addr":"127.0.0.1:1080"}
{"time":"2026-10-26T16:02:20.11+09:00","level":"INFO","msg":"listening for servers","component":"winclient","addr":":8443"}
{"time":"2026-10-26T16:02:23.31+09:00","level":"INFO","msg":"server connected","component":"winclient","remote":"192.168.1.20:52114","active_servers":1}
{"time":"2026-10-26T16:02:23.40+09:00","level":"INFO","msg":"vpn: adapter up","component":"winclient","interface":"AstronLink","address":"10.77.0.2/24"}
{"time":"2026-10-26T16:01:02.11+09:00","level":"WARN","msg":"registration rejected: invalid token from server","component":"winclient","remote":"192.168.1.20:52090"}
`), nil
}
