// Package serverui is the Windows server management console: a sidebar with
// 개요/클라이언트/터널/로그/설정/정보 pages over a Backend. It is platform
// independent so it can be rendered headlessly; cmd/winserver supplies the
// real backend and the window.
package serverui

import (
	"time"

	"github.com/AstronWorks/AstronLink/internal/config"
)

// Backend is what the console needs from the host process. Methods are
// called on the UI goroutine and must not block; long work (restarts) runs in
// the background and shows up in later State calls.
type Backend interface {
	// State returns a race-free snapshot of the server.
	State() State
	// LoadConfig reads the saved configuration (defaults when missing).
	LoadConfig() (*config.ServerConfig, error)
	// SaveConfig writes cfg and restarts the server with it.
	SaveConfig(cfg *config.ServerConfig) error
	Start()
	Stop()
	Restart()
	// Reconnect drops a client's control connection; the server dials it
	// again after the retry delay.
	Reconnect(clientID string) bool
	// ReadLog returns up to maxBytes from the end of the server log.
	ReadLog(maxBytes int) ([]byte, error)
	Autostart() bool
	SetAutostart(on bool) error
	Info() Info
}

// Phase is the server lifecycle state.
type Phase int

const (
	Stopped Phase = iota
	Starting
	Running
	Failed
)

// State is a snapshot of the running server.
type State struct {
	Phase     Phase
	Err       string // why the last start failed (Phase == Failed)
	StartedAt time.Time
	// Config is the configuration the server runs with (nil when stopped).
	Config      *config.ServerConfig
	Clients     []Client
	Tunnels     []Tunnel
	TotalConns  int64
	ActiveConns int64
}

// Client is one connected client.
type Client struct {
	ID, Name, Addr string
	ConnectedAt    time.Time
	Tunnels        int
}

// Tunnel is one active tunnel.
type Tunnel struct {
	ID, ClientID string
	Client       string // client name
	Type         string // tcp | http | https
	Public       string // ":port" for tcp, hostname for http/https
	Local        string // client-side target host:port
}

// Info describes the build and the files the server uses.
type Info struct {
	Version, Commit, Built string
	ConfigPath, LogPath    string
}
