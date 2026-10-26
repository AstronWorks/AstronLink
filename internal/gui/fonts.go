package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
)

// SansTypeface is the UI typeface list: the Windows UI fonts first (Segoe UI
// for Latin, Malgun Gothic for Hangul), then the fonts the headless preview
// finds on Linux, then the embedded Go fonts. The shaper falls back rune by
// rune along the list.
const SansTypeface font.Typeface = "Segoe UI, Malgun Gothic, Inter, NanumGothic, Go"

// MonoTypeface is used for logs, addresses and tokens.
const MonoTypeface font.Typeface = "Cascadia Mono, Consolas, JetBrains Mono, Malgun Gothic, NanumGothic, Go Mono"

var (
	fontsOnce sync.Once
	fontColl  []font.FontFace
)

// Fonts returns the font collection: the system fonts of fontFiles that exist
// plus the embedded Go fonts, parsed once.
func Fonts() []font.FontFace {
	fontsOnce.Do(func() {
		for _, path := range fontFiles() {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			faces, err := opentype.ParseCollection(data)
			if err != nil {
				continue
			}
			fontColl = append(fontColl, faces...)
		}
		fontColl = append(fontColl, gofont.Collection()...)
	})
	return fontColl
}

func fontFiles() []string {
	if runtime.GOOS == "windows" {
		dir := os.Getenv("WINDIR")
		if dir == "" {
			dir = `C:\Windows`
		}
		dir = filepath.Join(dir, "Fonts")
		names := []string{
			"segoeui.ttf", "seguisb.ttf", "segoeuib.ttf",
			"malgun.ttf", "malgunbd.ttf",
			"CascadiaMono.ttf", "consola.ttf",
		}
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = filepath.Join(dir, n)
		}
		return out
	}
	return []string{
		"/usr/share/fonts/opentype/inter/Inter-Regular.otf",
		"/usr/share/fonts/opentype/inter/Inter-SemiBold.otf",
		"/usr/share/fonts/opentype/inter/Inter-Bold.otf",
		"/usr/share/fonts/truetype/nanum/NanumGothic.ttf",
		"/usr/share/fonts/truetype/nanum/NanumGothicBold.ttf",
		"/usr/share/fonts/truetype/jetbrains-mono/JetBrainsMono-Regular.ttf",
	}
}
