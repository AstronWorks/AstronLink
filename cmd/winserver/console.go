//go:build windows

package main

import (
	"io"
	"os"
	"sync"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/gui"
	"github.com/EcoKG/reversproxy/internal/gui/serverui"
)

// console is the single management window; nil fields mean it is closed.
var console struct {
	mu  sync.Mutex
	win *app.Window
	ui  *serverui.UI
}

// openConsole shows the console on page p, creating the window if needed.
func openConsole(p serverui.Page) {
	console.mu.Lock()
	if console.win != nil {
		w, u := console.win, console.ui
		console.mu.Unlock()
		u.Show(p)
		w.Perform(system.ActionRaise)
		w.Invalidate()
		return
	}
	u := serverui.New(gui.NewTheme(prefDark()), backend{})
	u.OnTheme = setPrefDark
	u.Show(p)
	w := new(app.Window)
	console.win, console.ui = w, u
	console.mu.Unlock()
	go runConsole(w, u)
}

func runConsole(w *app.Window, u *serverui.UI) {
	defer func() {
		console.mu.Lock()
		if console.win == w {
			console.win, console.ui = nil, nil
		}
		console.mu.Unlock()
	}()
	defer recoverLog("console")
	w.Option(
		app.Title("Reversproxy Server"),
		app.Size(unit.Dp(1240), unit.Dp(800)),
		app.MinSize(unit.Dp(980), unit.Dp(640)),
	)
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			u.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// invalidateConsole asks an open console to redraw (state changed).
func invalidateConsole() {
	console.mu.Lock()
	w := console.win
	console.mu.Unlock()
	if w != nil {
		w.Invalidate()
	}
}

// closeConsole closes the window if it is open.
func closeConsole() {
	console.mu.Lock()
	w := console.win
	console.mu.Unlock()
	if w != nil {
		w.Perform(system.ActionClose)
	}
}

// backend adapts the tray process to serverui.Backend. Long operations run in
// goroutines so the window never blocks.
type backend struct{}

func (backend) State() serverui.State { return snapshot() }

func (backend) LoadConfig() (*config.ServerConfig, error) { return config.LoadServerConfig(configPath) }

func (backend) SaveConfig(cfg *config.ServerConfig) error {
	if err := saveConfig(configPath, cfg); err != nil {
		return err
	}
	srv.mu.Lock()
	running := srv.phase == serverui.Running || srv.phase == serverui.Starting
	srv.mu.Unlock()
	if running {
		restartServer()
	}
	return nil
}

func (backend) Start() {
	go func() {
		defer recoverLog("start")
		startServer()
	}()
}

func (backend) Stop()    { stopServer() }
func (backend) Restart() { restartServer() }

func (backend) Reconnect(id string) bool { return reconnectClient(id) }

func (backend) ReadLog(max int) ([]byte, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if off := fi.Size() - int64(max); off > 0 {
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(io.LimitReader(f, int64(max)))
}

func (backend) Autostart() bool { return isAutoStartEnabled() }

func (backend) SetAutostart(on bool) error {
	if err := setAutoStart(on); err != nil {
		return err
	}
	syncAutostartItem(on)
	return nil
}

func (backend) Info() serverui.Info {
	return serverui.Info{
		Version: Version, Commit: valOr(Commit, "unknown"), Built: valOr(BuildTime, "unknown"),
		ConfigPath: configPath, LogPath: logPath,
	}
}
