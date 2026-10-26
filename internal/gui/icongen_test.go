//go:build uipreview

// Generates the Windows icons from the same mark the consoles draw:
//
//	CGO_ENABLED=1 EGL_PLATFORM=surfaceless UIICON_OUT=/tmp/icons \
//	  go test -tags uipreview,novulkan,nowayland,nox11 -run TestIcons ./internal/gui/
//
// Output per app (server, client): <app>.ico (16–256 px) plus tray-running,
// tray-stopped and tray-error .ico files (16–48 px), and 256 px PNGs.
package gui

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
)

type iconSpec struct {
	from, to color.NRGBA
	glyph    *widget.Icon
}

var iconApps = map[string]iconSpec{
	"server": {RGB(0x6366F1), RGB(0x9333EA), IconDNS},
	"client": {RGB(0x0EA5E9), RGB(0x14B8A6), IconComputer},
}

type iconState int

const (
	stateNormal iconState = iota
	stateStopped
	stateError
)

func renderIcon(t *testing.T, th *Theme, spec iconSpec, state iconState, px int) []byte {
	win, err := headless.NewWindow(px, px)
	if err != nil {
		t.Fatal(err)
	}
	defer win.Release()
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(px, px))}
	from, to := spec.from, spec.to
	if state == stateStopped {
		from, to = RGB(0xA3ACB9), RGB(0x6B7280)
	}
	th.Logo(gtx, unit.Dp(px), from, to, spec.glyph)
	if state == stateError {
		d := px * 46 / 100
		ring := max(1, px/16)
		FillCircle(gtx, image.Rect(px-d, px-d, px, px), RGB(0xFFFFFF))
		FillCircle(gtx, image.Rect(px-d+ring, px-d+ring, px-ring, px-ring), RGB(0xE5372B))
	}
	if err := win.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeICO packs PNG images (one per size) into an .ico file.
func writeICO(path string, sizes []int, pngs [][]byte) error {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, [3]uint16{0, 1, uint16(len(pngs))})
	offset := 6 + 16*len(pngs)
	for i, p := range pngs {
		dim := uint8(sizes[i])
		if sizes[i] >= 256 {
			dim = 0
		}
		binary.Write(&buf, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(p)), uint32(offset)})
		offset += len(p)
	}
	for _, p := range pngs {
		buf.Write(p)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func TestIcons(t *testing.T) {
	out := os.Getenv("UIICON_OUT")
	if out == "" {
		t.Skip("UIICON_OUT not set")
	}
	th := NewTheme(false)
	appSizes := []int{16, 20, 24, 32, 40, 48, 64, 128, 256}
	traySizes := []int{16, 20, 24, 32, 40, 48}
	for name, spec := range iconApps {
		var pngs [][]byte
		for _, s := range appSizes {
			pngs = append(pngs, renderIcon(t, th, spec, stateNormal, s))
		}
		if err := writeICO(filepath.Join(out, name+".ico"), appSizes, pngs); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, name+"-256.png"), pngs[len(pngs)-1], 0o644); err != nil {
			t.Fatal(err)
		}
		for st, suffix := range map[iconState]string{stateNormal: "running", stateStopped: "stopped", stateError: "error"} {
			var tray [][]byte
			for _, s := range traySizes {
				tray = append(tray, renderIcon(t, th, spec, st, s))
			}
			if err := writeICO(filepath.Join(out, name+"-tray-"+suffix+".ico"), traySizes, tray); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, name+"-tray-"+suffix+"-48.png"), tray[len(tray)-1], 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
