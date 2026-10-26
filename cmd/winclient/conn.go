//go:build windows

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/AstronWorks/AstronLink/internal/client"
	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/control"
	"github.com/AstronWorks/AstronLink/internal/gui/clientui"
	"github.com/AstronWorks/AstronLink/internal/logger"
	"github.com/AstronWorks/AstronLink/internal/reconnect"
	"github.com/AstronWorks/AstronLink/internal/socks"
	"github.com/AstronWorks/AstronLink/internal/tunnel"
	"github.com/AstronWorks/AstronLink/internal/vpn/adapter"
)

// cl is the connection lifecycle. gen increments on every connect and
// disconnect so goroutines of an older run cannot overwrite the state.
var cl struct {
	mu        sync.Mutex
	gen       int
	phase     clientui.Phase
	err       string
	since     time.Time
	cfg       *config.ClientConfig
	cancel    context.CancelFunc
	done      chan struct{}
	servers   map[*client.ServerSession]time.Time
	vpn       *adapter.Manager
	socksAddr string
	httpAddr  string
}

func isActive() bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.phase == clientui.Waiting || cl.phase == clientui.Connected
}

// connect loads config.yaml, opens the TLS listener and waits for servers.
// It reports whether the listener came up.
func connect() bool {
	cl.mu.Lock()
	if cl.phase == clientui.Waiting || cl.phase == clientui.Connected {
		cl.mu.Unlock()
		return true
	}
	cl.gen++
	gen := cl.gen
	cl.mu.Unlock()

	cfg, err := config.LoadClientConfig(configPath)
	if err != nil {
		failConn(gen, "config.yaml 을 읽지 못했습니다: "+err.Error())
		return false
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		failConn(gen, "인증 토큰이 비어 있습니다. 설정 > 연결에 서버의 토큰을 넣으십시오.")
		return false
	}
	log := logger.NewWithLevel("winclient", cfg.LogLevel)

	cert, err := control.LoadOrGenerateCert(cfg.CertPath, cfg.KeyPath)
	if err != nil {
		failConn(gen, "TLS 인증서를 준비하지 못했습니다: "+err.Error())
		return false
	}
	tlsCfg := control.NewServerTLSConfig(cert)
	if cfg.Insecure {
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	}
	ln, err := tls.Listen("tcp", cfg.ListenAddr, tlsCfg)
	if err != nil {
		failConn(gen, fmt.Sprintf("%s 에서 대기하지 못했습니다: %v (다른 프로그램이 포트를 쓰는지 확인하십시오)", cfg.ListenAddr, err))
		return false
	}

	var vpnMgr *adapter.Manager
	if cfg.VPN.Enabled {
		opts, err := adapter.OptionsFromConfig(cfg.VPN, log)
		if err != nil {
			ln.Close()
			failConn(gen, "VPN 설정 오류: "+err.Error())
			return false
		}
		vpnMgr = adapter.New(opts)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	cl.mu.Lock()
	if cl.gen != gen { // disconnected meanwhile
		cl.mu.Unlock()
		cancel()
		ln.Close()
		if vpnMgr != nil {
			vpnMgr.Close()
		}
		return false
	}
	cl.phase, cl.err, cl.since, cl.cfg = clientui.Waiting, "", time.Now(), cfg
	cl.cancel, cl.done, cl.vpn = cancel, done, vpnMgr
	cl.servers = map[*client.ServerSession]time.Time{}
	cl.socksAddr, cl.httpAddr = "", ""
	cl.mu.Unlock()
	setTray(clientui.Waiting, "서버 대기 중")
	invalidateConsole()
	log.Info("listening for servers", "addr", ln.Addr().String(), "name", cfg.Name)

	pool := client.NewServerPool()
	if cfg.SOCKSAddr != "" {
		if err := socks.StartClientSOCKSProxy(ctx, cfg.SOCKSAddr, pool, log, cfg.SOCKSUser, cfg.SOCKSPass); err != nil {
			log.Error("failed to start SOCKS5 proxy", "addr", cfg.SOCKSAddr, "err", err)
		} else {
			setProxyAddrs(gen, socks.LastClientSOCKSAddr, "")
		}
	}
	if cfg.HTTPProxyAddr != "" {
		if err := socks.StartHTTPConnectProxy(ctx, cfg.HTTPProxyAddr, pool, log); err != nil {
			log.Error("failed to start HTTP CONNECT proxy", "addr", cfg.HTTPProxyAddr, "err", err)
		} else {
			setProxyAddrs(gen, "", socks.LastClientHTTPProxyAddr)
		}
	}
	for _, pf := range cfg.PortForwards {
		if err := socks.StartPortForward(ctx, pf.LocalPort, pf.RemoteHost, pf.RemotePort, pf.Bind, pool, log); err != nil {
			log.Error("failed to start port forward", "localPort", pf.LocalPort, "err", err)
		}
	}

	go acceptLoop(ctx, gen, ln, cfg, pool, vpnMgr, log, done)
	return true
}

func setProxyAddrs(gen int, socksAddr, httpAddr string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.gen != gen {
		return
	}
	if socksAddr != "" {
		cl.socksAddr = socksAddr
	}
	if httpAddr != "" {
		cl.httpAddr = httpAddr
	}
}

// acceptLoop hands every inbound server connection to its own session until
// ctx ends, then releases the run's resources.
func acceptLoop(ctx context.Context, gen int, ln net.Listener, cfg *config.ClientConfig,
	pool *client.ServerPool, vpnMgr *adapter.Manager, log *slog.Logger, done chan struct{}) {
	defer close(done)
	defer recoverLog("acceptLoop")
	defer func() {
		if vpnMgr != nil {
			vpnMgr.Close()
		}
	}()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	rcCfg := buildClientConfig(cfg)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() == nil {
				log.Error("accept error", "err", err)
				failConn(gen, "대기 중 오류: "+err.Error())
			}
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer recoverLog("serverSession")
			handleServerSession(ctx, gen, conn, cfg, rcCfg, pool, vpnMgr, log)
		}()
	}
}

// handleServerSession serves one inbound server connection; the tray and
// console follow the number of live sessions.
func handleServerSession(ctx context.Context, gen int, conn net.Conn, cfg *config.ClientConfig,
	rcCfg *reconnect.ClientConfig, pool *client.ServerPool, vpnMgr *adapter.Manager, log *slog.Logger) {
	session := &client.ServerSession{
		ID:          conn.RemoteAddr().String(),
		Conn:        conn,
		Writer:      client.NewConnWriter(conn),
		Mux:         tunnel.NewSOCKSMux(),
		Addr:        conn.RemoteAddr().String(),
		ConnectedAt: time.Now(),
		VPN:         vpnMgr,
	}
	pool.Add(session)
	sessionChanged(gen, session, true)
	log.Info("server connected", "remote", session.Addr, "active_servers", pool.Len())

	client.HandleServerConn(ctx, session, cfg.AuthToken, cfg.Name, rcCfg, log)

	pool.Remove(session)
	sessionChanged(gen, session, false)
	log.Warn("server connection lost", "remote", session.Addr, "active_servers", pool.Len())
}

func sessionChanged(gen int, s *client.ServerSession, up bool) {
	cl.mu.Lock()
	if cl.gen != gen {
		cl.mu.Unlock()
		return
	}
	if up {
		cl.servers[s] = s.ConnectedAt
	} else {
		delete(cl.servers, s)
	}
	phase, text := clientui.Waiting, "서버 대기 중"
	if len(cl.servers) > 0 {
		phase, text = clientui.Connected, fmt.Sprintf("연결됨 (서버 %d)", len(cl.servers))
	}
	cl.phase = phase
	cl.mu.Unlock()
	setTray(phase, text)
	invalidateConsole()
}

// failConn records a failure of run gen.
func failConn(gen int, msg string) {
	logError(msg, nil)
	cl.mu.Lock()
	if cl.gen != gen {
		cl.mu.Unlock()
		return
	}
	if cl.cancel != nil {
		cl.cancel()
	}
	cl.phase, cl.err, cl.cfg, cl.cancel = clientui.Failed, msg, nil, nil
	cl.servers = nil
	cl.mu.Unlock()
	setTray(clientui.Failed, "시작 실패")
	invalidateConsole()
}

// disconnect stops the current run and returns a channel closed when it has
// released its listener and adapter (nil when nothing was running).
func disconnect() chan struct{} {
	cl.mu.Lock()
	cl.gen++
	cancel, done := cl.cancel, cl.done
	cl.phase, cl.err, cl.cfg, cl.cancel, cl.done = clientui.Disconnected, "", nil, nil, nil
	cl.servers, cl.vpn = nil, nil
	cl.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	setTray(clientui.Disconnected, "연결 안 됨")
	invalidateConsole()
	return done
}

// restartConnection restarts the run with the saved configuration.
func restartConnection() {
	done := disconnect()
	go func() {
		defer recoverLog("reconnect")
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
		}
		time.Sleep(300 * time.Millisecond) // let the OS release the port
		connect()
	}()
}

// snapshot is the console's view of the client.
func snapshot() clientui.State {
	cl.mu.Lock()
	st := clientui.State{
		Phase: cl.phase, Err: cl.err, Since: cl.since, Config: cl.cfg,
		SOCKSAddr: cl.socksAddr, HTTPAddr: cl.httpAddr,
	}
	for s, at := range cl.servers {
		st.Servers = append(st.Servers, clientui.Server{Addr: s.Addr, ConnectedAt: at})
	}
	vpnMgr := cl.vpn
	cl.mu.Unlock()
	if vpnMgr != nil {
		v := vpnMgr.Status()
		st.VPN = clientui.VPNState{Active: v.Active, Interface: v.InterfaceName}
		if v.Active {
			st.VPN.Address, st.VPN.Gateway = v.Address.String(), v.Gateway.String()
		}
	}
	st.FileReceiver = fileReceiverAddr()
	return st
}

// buildClientConfig converts config tunnels to the registration list.
func buildClientConfig(cfg *config.ClientConfig) *reconnect.ClientConfig {
	rcCfg := &reconnect.ClientConfig{}
	for _, t := range cfg.Tunnels {
		switch t.Type {
		case "tcp", "":
			rcCfg.AddTunnel(t.LocalHost, t.LocalPort, t.RequestedPort)
		case "http":
			rcCfg.AddHTTPTunnel(t.Hostname, t.LocalHost, t.LocalPort)
		case "https":
			rcCfg.AddHTTPSTunnel(t.Hostname, t.LocalHost, t.LocalPort)
		}
	}
	return rcCfg
}

// writeDefaultConfig creates a first-run config.yaml named after this PC. The
// token stays empty: it has to be the server's.
func writeDefaultConfig(path string) error {
	cfg := config.DefaultClientConfig()
	if h, err := os.Hostname(); err == nil && h != "" {
		cfg.Name = strings.ToLower(h)
	}
	return saveConfig(path, cfg)
}

const configHeader = "# Astron Link 클라이언트 설정 — 관리 콘솔의 [설정] 탭에서 편집합니다.\n"

// saveConfig writes cfg atomically, keeping the previous file as .bak.
func saveConfig(path string, cfg *config.ClientConfig) error {
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
		return fmt.Errorf("config.yaml 저장 실패: %w", err)
	}
	return nil
}

// logError writes a winclient error to the log in the JSON format the
// console's log page parses.
func logError(msg string, err error) {
	l := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "winclient")
	if err != nil {
		l.Error(msg, "err", err.Error())
		return
	}
	l.Error(msg)
}
