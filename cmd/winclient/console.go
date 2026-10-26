//go:build windows

package main

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/unit"
	"golang.org/x/sys/windows"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/gui"
	"github.com/EcoKG/reversproxy/internal/gui/clientui"
)

// console is the single management window; nil fields mean it is closed.
var console struct {
	mu  sync.Mutex
	win *app.Window
	ui  *clientui.UI
}

// openConsole shows the console on page p, creating the window if needed.
func openConsole(p clientui.Page) {
	console.mu.Lock()
	if console.win != nil {
		w, u := console.win, console.ui
		console.mu.Unlock()
		u.Show(p)
		w.Perform(system.ActionRaise)
		w.Invalidate()
		return
	}
	u := clientui.New(gui.NewTheme(prefDark()), backend{})
	u.OnTheme = setPrefDark
	u.Show(p)
	w := new(app.Window)
	console.win, console.ui = w, u
	console.mu.Unlock()
	go runConsole(w, u)
}

func runConsole(w *app.Window, u *clientui.UI) {
	defer func() {
		console.mu.Lock()
		if console.win == w {
			console.win, console.ui = nil, nil
		}
		console.mu.Unlock()
	}()
	defer recoverLog("console")
	w.Option(
		app.Title("Astron Link Client"),
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

func invalidateConsole() {
	console.mu.Lock()
	w := console.win
	console.mu.Unlock()
	if w != nil {
		w.Invalidate()
	}
}

func closeConsole() {
	console.mu.Lock()
	w := console.win
	console.mu.Unlock()
	if w != nil {
		w.Perform(system.ActionClose)
	}
}

// backend adapts the tray process to clientui.Backend.
type backend struct{}

func (backend) State() clientui.State { return snapshot() }

func (backend) LoadConfig() (*config.ClientConfig, error) { return config.LoadClientConfig(configPath) }

func (backend) SaveConfig(cfg *config.ClientConfig) error {
	if err := saveConfig(configPath, cfg); err != nil {
		return err
	}
	go func() {
		defer recoverLog("save")
		restartFileTransfer(cfg)
		if isActive() {
			restartConnection()
		}
	}()
	return nil
}

func (backend) Connect() {
	go func() {
		defer recoverLog("connect")
		connect()
	}()
}

func (backend) Disconnect() { disconnect() }
func (backend) Reconnect()  { restartConnection() }

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

func (backend) ContextMenu() bool            { return contextMenuRegistered() }
func (backend) SetContextMenu(on bool) error { return setContextMenu(on) }
func (backend) OpenDropFolder() error        { return openDropFolder() }

func (backend) Info() clientui.Info {
	_, err := os.Stat(filepath.Join(exeDir, "wintun.dll"))
	return clientui.Info{
		Version: Version, Commit: valOr(Commit, "unknown"), Built: valOr(BuildTime, "unknown"),
		ConfigPath: configPath, LogPath: logPath,
		LocalIPs: localIPv4s(), Admin: windows.GetCurrentProcessToken().IsElevated(), Wintun: err == nil,
	}
}

// localIPv4s lists this PC's IPv4 addresses on up, non-loopback interfaces,
// skipping link-local addresses and the VPN adapter.
func localIPv4s() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if strings.EqualFold(ifc.Name, "AstronLink") || strings.Contains(strings.ToLower(ifc.Name), "wintun") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || ip.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}
