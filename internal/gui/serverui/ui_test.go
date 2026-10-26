package serverui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/gui"
)

// harness lays out the console without a GPU; clicks are injected with
// Clickable.Click.
type harness struct {
	t  *testing.T
	be *demoBackend
	u  *UI
	r  input.Router
}

func newHarness(t *testing.T) *harness {
	be := newDemo()
	h := &harness{t: t, be: be, u: New(gui.NewTheme(false), be)}
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

func TestNavigationAndSave(t *testing.T) {
	h := newHarness(t)
	h.u.nav[PageSettings].Click()
	h.frame()
	if h.u.page != PageSettings {
		t.Fatalf("page = %v, want settings", h.u.page)
	}
	if h.u.set.dirty {
		t.Fatal("a freshly loaded form must not be dirty")
	}

	h.u.set.addClientBtn.Click()
	h.frame()
	if n := len(h.u.set.clients); n != 3 {
		t.Fatalf("clients = %d, want 3", n)
	}
	row := h.u.set.clients[2]
	row.name.SetText("lab")
	row.addr.SetText("10.0.0.5:8443")
	h.u.set.vpnDNS.SetText("1.1.1.1, 8.8.8.8")
	h.frame()
	if !h.u.set.dirty || len(h.u.set.errs) != 0 {
		t.Fatalf("dirty=%v errs=%v", h.u.set.dirty, h.u.set.errs)
	}

	h.u.set.save.Click()
	h.frame()
	if !h.u.modal.Visible() {
		t.Fatal("save should ask for confirmation")
	}
	h.u.modal.Confirm()
	h.frame()
	saved := h.be.saved
	if saved == nil {
		t.Fatal("SaveConfig was not called")
	}
	if len(saved.Clients) != 3 || saved.Clients[2] != (config.ClientTarget{Name: "lab", Address: "10.0.0.5:8443"}) {
		t.Fatalf("clients = %+v", saved.Clients)
	}
	if got := saved.VPN.DNS; len(got) != 2 || got[0] != "1.1.1.1" || got[1] != "8.8.8.8" {
		t.Fatalf("vpn.dns = %v", got)
	}
	if saved.AuthToken != newDemo().cfg.AuthToken || saved.HTTPAddr != ":8787" {
		t.Fatalf("untouched fields changed: token=%q http=%q", saved.AuthToken, saved.HTTPAddr)
	}
	h.frame()
	if h.u.set.dirty {
		t.Fatal("form should be clean after saving")
	}
}

func TestSaveBlockedByValidation(t *testing.T) {
	h := newHarness(t)
	h.u.page = PageSettings
	h.u.set.tab.Value = tabVPN
	h.u.set.addClientBtn.Click()
	h.frame()
	h.u.set.clients[2].addr.SetText("no-port")
	h.frame()
	h.u.set.save.Click()
	h.frame()
	if h.u.modal.Visible() || h.be.saved != nil {
		t.Fatal("an invalid form must not be saved")
	}
	if !h.u.set.showErrs || h.u.set.tab.Value != tabConn {
		t.Fatalf("errors should be shown on the 연결 tab: show=%v tab=%q", h.u.set.showErrs, h.u.set.tab.Value)
	}
	if h.u.set.errs[clientKey(fClientName, 2)] == "" || h.u.set.errs[clientKey(fClientAddr, 2)] == "" {
		t.Fatalf("missing row errors: %v", h.u.set.errs)
	}
}

func TestOptionalListenerAndRevert(t *testing.T) {
	h := newHarness(t)
	h.u.page = PageSettings
	h.u.set.tab.Value = tabListen
	h.frame()
	h.u.set.httpOn.Value = false
	h.frame()
	cfg, errs := h.u.set.collect()
	if cfg.HTTPAddr != "" || len(errs) != 0 {
		t.Fatalf("http_addr = %q, errs %v", cfg.HTTPAddr, errs)
	}
	h.u.set.revert.Click()
	h.frame()
	if !h.u.set.httpOn.Value || h.u.set.dirty {
		t.Fatal("revert should restore the saved form")
	}
}

func TestGenerateTokenAndStop(t *testing.T) {
	h := newHarness(t)
	h.u.page = PageSettings
	h.frame()
	old := h.u.set.authToken.Text()
	h.u.set.genAuth.Click()
	h.frame()
	if tok := h.u.set.authToken.Text(); len(tok) != 64 || tok == old {
		t.Fatalf("generated token %q", tok)
	}

	h.u.stop.Click()
	h.frame()
	if !h.u.modal.Visible() || h.be.stopped {
		t.Fatal("stop should ask first")
	}
	h.u.modal.Confirm()
	h.frame()
	if !h.be.stopped {
		t.Fatal("Stop was not called after confirming")
	}
}
