package clientui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/gui"
)

type harness struct {
	be *demoBackend
	u  *UI
	r  input.Router
}

func newHarness() *harness {
	be := newDemo()
	h := &harness{be: be, u: New(gui.NewTheme(false), be)}
	h.frame()
	return h
}

func (h *harness) frame() {
	ops := new(op.Ops)
	gtx := layout.Context{
		Ops: ops, Now: time.Now(), Source: h.r.Source(),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1240, 800)),
	}
	h.u.Layout(gtx)
	h.r.Frame(ops)
}

func TestEditTunnelsAndSave(t *testing.T) {
	h := newHarness()
	h.u.nav[PageSettings].Click()
	h.frame()
	if h.u.page != PageSettings || h.u.set.dirty {
		t.Fatalf("page=%v dirty=%v", h.u.page, h.u.set.dirty)
	}
	h.u.set.tab.Value = tabTunnels
	h.u.set.addTunnel.Click()
	h.frame()
	r := h.u.set.tunnels[2]
	r.typ.Value = "http"
	r.port.SetText("3000")
	r.hname.SetText("web.example.com")
	h.u.set.tunnels[0].del.Click()
	h.frame()
	h.u.set.tab.Value = tabForwards
	h.u.set.addForward.Click()
	h.frame()
	pf := h.u.set.forwards[1]
	pf.local.SetText("13389")
	pf.host.SetText("10.0.0.9")
	pf.remote.SetText("3389")
	h.frame()
	if len(h.u.set.errs) != 0 || !h.u.set.dirty {
		t.Fatalf("errs=%v dirty=%v", h.u.set.errs, h.u.set.dirty)
	}
	h.u.set.save.Click()
	h.frame()
	h.u.modal.Confirm()
	h.frame()
	got := h.be.saved
	if got == nil {
		t.Fatal("SaveConfig not called")
	}
	want := []config.TunnelConfig{
		{Type: "https", LocalHost: "127.0.0.1", LocalPort: 8443, Hostname: "app.example.com"},
		{Type: "http", LocalHost: "127.0.0.1", LocalPort: 3000, Hostname: "web.example.com"},
	}
	if len(got.Tunnels) != 2 || got.Tunnels[0] != want[0] || got.Tunnels[1] != want[1] {
		t.Fatalf("tunnels = %+v", got.Tunnels)
	}
	if len(got.PortForwards) != 2 || got.PortForwards[1] != (config.PortForwardConfig{LocalPort: 13389, RemoteHost: "10.0.0.9", RemotePort: 3389}) {
		t.Fatalf("port forwards = %+v", got.PortForwards)
	}
	if got.AuthToken != validConfig().AuthToken || got.VPN.Routes[0] != "10.0.0.0/8" {
		t.Fatalf("untouched fields changed: %+v", got)
	}
}

func TestVPNModeAndValidation(t *testing.T) {
	h := newHarness()
	h.u.page = PageSettings
	h.u.set.tab.Value = tabVPN
	h.frame()
	h.u.set.vpnMode.Value = "full"
	h.frame()
	cfg, errs := h.u.set.collect()
	if len(cfg.VPN.Routes) != 0 || len(errs) != 0 {
		t.Fatalf("full tunnel should clear routes: %v %v", cfg.VPN.Routes, errs)
	}
	h.u.set.vpnMode.Value = "split"
	h.u.set.vpnRoutes.SetText("")
	h.u.set.token.SetText("")
	h.frame()
	h.u.set.save.Click()
	h.frame()
	if h.be.saved != nil || h.u.modal.Visible() {
		t.Fatal("invalid form saved")
	}
	if h.u.set.tab.Value != tabConn || h.u.set.errs[fToken] == "" || h.u.set.errs[fVPNRoutes] == "" {
		t.Fatalf("tab=%q errs=%v", h.u.set.tab.Value, h.u.set.errs)
	}
}

func TestDisconnectAsks(t *testing.T) {
	h := newHarness()
	h.u.disconnect.Click()
	h.frame()
	if !h.u.modal.Visible() || h.be.disconnected {
		t.Fatal("disconnect should ask first")
	}
	h.u.modal.Confirm()
	h.frame()
	if !h.be.disconnected {
		t.Fatal("Disconnect not called")
	}
}
