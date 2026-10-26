//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/EcoKG/reversproxy/internal/app"
	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/gui/serverui"
	"github.com/EcoKG/reversproxy/internal/vpn"
)

// srv is the server lifecycle. gen increments on every start and stop so a
// slow goroutine of an older instance cannot overwrite the current state.
var srv struct {
	mu        sync.Mutex
	gen       int
	phase     serverui.Phase
	err       string
	startedAt time.Time
	app       *app.ServerApp
	cancel    context.CancelFunc
	done      chan struct{} // closed when the current app's Start returns
}

// startServer loads server.yaml and runs the server. It waits briefly for an
// immediate listener-bind failure and reports whether the server came up.
func startServer() bool {
	srv.mu.Lock()
	if srv.phase == serverui.Running || srv.phase == serverui.Starting {
		srv.mu.Unlock()
		return true
	}
	srv.gen++
	gen := srv.gen
	srv.phase, srv.err = serverui.Starting, ""
	srv.mu.Unlock()
	setTray(serverui.Starting, "시작하는 중")
	invalidateConsole()

	cfg, err := config.LoadServerConfig(configPath)
	if err != nil {
		fail(gen, "server.yaml 을 읽지 못했습니다: "+err.Error())
		return false
	}
	a, err := app.NewServerApp(cfg)
	if err != nil {
		fail(gen, "설정이 올바르지 않습니다: "+err.Error())
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	srv.mu.Lock()
	if srv.gen != gen { // stopped meanwhile
		srv.mu.Unlock()
		cancel()
		return false
	}
	srv.app, srv.cancel, srv.done = a, cancel, done
	srv.mu.Unlock()

	exited := make(chan error, 1)
	go func() {
		defer close(done)
		defer recoverLog("server.Start")
		// Start blocks until ctx is cancelled; it returns early only on a
		// start-up failure such as a port already in use.
		err := a.Start(ctx)
		if ctx.Err() == nil {
			msg := "서버가 예기치 않게 멈췄습니다"
			if err != nil {
				msg = "서버를 시작하지 못했습니다: " + err.Error() + " (다른 프로그램이 같은 포트를 쓰는지 확인하십시오)"
			}
			fail(gen, msg)
		}
		exited <- err
	}()

	select {
	case <-exited:
		return false
	case <-time.After(400 * time.Millisecond):
	}
	srv.mu.Lock()
	ok := srv.gen == gen && srv.phase == serverui.Starting
	if ok {
		srv.phase, srv.startedAt = serverui.Running, time.Now()
	}
	srv.mu.Unlock()
	if ok {
		setTray(serverui.Running, "실행 중")
		invalidateConsole()
	}
	return ok
}

// fail records a start failure of generation gen.
func fail(gen int, msg string) {
	logError(msg, nil)
	srv.mu.Lock()
	if srv.gen != gen {
		srv.mu.Unlock()
		return
	}
	if srv.cancel != nil {
		srv.cancel()
	}
	srv.phase, srv.err = serverui.Failed, msg
	srv.app, srv.cancel = nil, nil
	srv.mu.Unlock()
	setTray(serverui.Failed, "시작 실패")
	invalidateConsole()
}

// stopServer cancels the running server and returns a channel closed once it
// has shut down (nil when nothing was running).
func stopServer() chan struct{} {
	srv.mu.Lock()
	srv.gen++
	cancel, done := srv.cancel, srv.done
	srv.app, srv.cancel, srv.done = nil, nil, nil
	srv.phase, srv.err = serverui.Stopped, ""
	srv.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	setTray(serverui.Stopped, "중지됨")
	invalidateConsole()
	return done
}

// restartServer stops the server, waits for its listeners to close and
// starts it again with the saved configuration.
func restartServer() {
	done := stopServer()
	go func() {
		defer recoverLog("restart")
		if done != nil {
			select {
			case <-done:
			case <-time.After(6 * time.Second):
			}
		}
		time.Sleep(300 * time.Millisecond) // let the OS release the ports
		startServer()
	}()
}

// snapshot is the console's view of the server.
func snapshot() serverui.State {
	srv.mu.Lock()
	st := serverui.State{Phase: srv.phase, Err: srv.err, StartedAt: srv.startedAt}
	a := srv.app
	srv.mu.Unlock()
	if a == nil || st.Phase == serverui.Failed {
		return st
	}
	st.Config = a.Config()
	names := map[string]string{}
	tunnels := map[string]int{}
	for _, t := range a.Manager().ListTunnels() {
		tunnels[t.ClientID]++
		st.Tunnels = append(st.Tunnels, serverui.Tunnel{
			ID: t.ID, ClientID: t.ClientID, Type: "tcp",
			Public: fmt.Sprintf(":%d", t.PublicPort),
			Local:  fmt.Sprintf("%s:%d", t.LocalHost, t.LocalPort),
		})
	}
	for _, h := range a.Manager().ListHTTPTunnels() {
		tunnels[h.ClientID]++
		typ := "http"
		if h.IsTLS {
			typ = "https"
		}
		st.Tunnels = append(st.Tunnels, serverui.Tunnel{
			ID: h.ID, ClientID: h.ClientID, Type: typ, Public: h.Hostname,
			Local: fmt.Sprintf("%s:%d", h.LocalHost, h.LocalPort),
		})
	}
	for _, c := range a.Registry().List() {
		names[c.ID] = c.Name
		st.Clients = append(st.Clients, serverui.Client{
			ID: c.ID, Name: c.Name, Addr: c.Addr, ConnectedAt: c.RegisteredAt, Tunnels: tunnels[c.ID],
		})
	}
	for i := range st.Tunnels {
		st.Tunnels[i].Client = names[st.Tunnels[i].ClientID]
	}
	gs := a.GlobalStats()
	st.TotalConns, st.ActiveConns = gs.TotalConnections.Load(), gs.ActiveConnections.Load()
	return st
}

// reconnectClient drops a client's control connection; the dial loop
// reconnects after its retry delay.
func reconnectClient(id string) bool {
	srv.mu.Lock()
	a := srv.app
	srv.mu.Unlock()
	return a != nil && a.Registry().Disconnect(id)
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// writeDefaultConfig creates a first-run server.yaml: a random token and
// insecure mode, which the self-signed client certificates need unless a CA
// is configured.
func writeDefaultConfig(path string) error {
	cfg := config.DefaultServerConfig()
	cfg.AuthToken = randomToken()
	cfg.Insecure = true
	cfg.AllowPrivateTargets = true
	return saveConfig(path, cfg)
}

const configHeader = "# Reversproxy 서버 설정 — 관리 콘솔의 [설정] 탭에서 편집합니다.\n"

// saveConfig validates cfg and writes it atomically, keeping the previous
// file as server.yaml.bak.
func saveConfig(path string, cfg *config.ServerConfig) error {
	if err := cfg.ValidateSecurity(); err != nil {
		return err
	}
	if _, err := vpn.ServerSettingsFromConfig(cfg.VPN); err != nil {
		return err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(configHeader), out...), 0o600); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		_ = os.Remove(path + ".bak")
		_ = os.Rename(path, path+".bak")
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("server.yaml 저장 실패: %w", err)
	}
	return nil
}

// logError writes a winserver error to server.log in the server's JSON format
// so the console's log page shows it.
func logError(msg string, err error) {
	l := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "winserver")
	if err != nil {
		l.Error(msg, "err", err.Error())
		return
	}
	l.Error(msg)
}
