//go:build uipreview

// Renders every console page with demo data to PNG files:
//
//	CGO_ENABLED=1 EGL_PLATFORM=surfaceless UIPREVIEW_OUT=/tmp/out \
//	  go test -tags uipreview,novulkan,nowayland,nox11 -run TestPreview ./internal/gui/clientui/
package clientui

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/AstronWorks/AstronLink/internal/gui"
)

func TestPreview(t *testing.T) {
	out := os.Getenv("UIPREVIEW_OUT")
	if out == "" {
		t.Skip("UIPREVIEW_OUT not set")
	}
	const scale = 1.25
	w, h := int(1240*scale), int(800*scale)
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Fatal(err)
	}
	var router input.Router
	for _, dark := range []bool{false, true} {
		th := gui.NewTheme(dark)
		u := New(th, newDemo())
		shots := []struct {
			name string
			prep func()
		}{
			{"overview", func() { u.page = PageOverview }},
			{"tunnels", func() { u.page = PageTunnels }},
			{"logs", func() { u.page = PageLogs }},
			{"settings-conn", func() { u.page = PageSettings; u.set.tab.Value = tabConn }},
			{"settings-proxy", func() { u.set.tab.Value = tabProxy }},
			{"settings-tunnels", func() { u.set.tab.Value = tabTunnels }},
			{"settings-forwards", func() { u.set.tab.Value = tabForwards }},
			{"settings-vpn", func() { u.set.tab.Value = tabVPN }},
			{"settings-files", func() { u.set.tab.Value = tabFiles }},
			{"settings-general", func() { u.set.tab.Value = tabGeneral }},
			{"about", func() { u.page = PageAbout }},
		}
		for _, s := range shots {
			s.prep()
			var img *image.RGBA
			for i := 0; i < 3; i++ { // settle lists and cached state
				ops := new(op.Ops)
				gtx := layout.Context{
					Ops:         ops,
					Now:         time.Now(),
					Source:      router.Source(),
					Metric:      unit.Metric{PxPerDp: scale, PxPerSp: scale},
					Constraints: layout.Exact(image.Pt(w, h)),
				}
				u.Layout(gtx)
				router.Frame(ops)
				if err := win.Frame(ops); err != nil {
					t.Fatal(err)
				}
			}
			img = image.NewRGBA(image.Rect(0, 0, w, h))
			if err := win.Screenshot(img); err != nil {
				t.Fatal(err)
			}
			mode := "light"
			if dark {
				mode = "dark"
			}
			f, err := os.Create(filepath.Join(out, fmt.Sprintf("client-%s-%s.png", mode, s.name)))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}
