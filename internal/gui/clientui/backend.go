// Package clientui is the Windows client management console: 개요/터널/로그/
// 설정/정보 pages over a Backend. Like serverui it is platform independent;
// cmd/winclient supplies the real backend and the window.
package clientui

import (
	"time"

	"github.com/AstronWorks/AstronLink/internal/config"
)

// Backend is what the console needs from the host process. Methods are
// called on the UI goroutine and must not block.
type Backend interface {
	State() State
	// LoadConfig reads the saved configuration (defaults when missing).
	LoadConfig() (*config.ClientConfig, error)
	// SaveConfig writes cfg and, when connected, reconnects with it.
	SaveConfig(cfg *config.ClientConfig) error
	Connect()
	Disconnect()
	Reconnect()
	// ReadLog returns up to maxBytes from the end of the client log.
	ReadLog(maxBytes int) ([]byte, error)
	Autostart() bool
	SetAutostart(on bool) error
	// ContextMenu reports whether the Explorer "파일 전송" verb is registered.
	ContextMenu() bool
	SetContextMenu(on bool) error
	// OpenDropFolder shows the received-files folder in Explorer.
	OpenDropFolder() error
	Info() Info
}

// Phase is the client lifecycle state.
type Phase int

const (
	Disconnected Phase = iota
	Waiting            // listening, no server connected
	Connected
	Failed
)

// State is a snapshot of the client.
type State struct {
	Phase Phase
	Err   string
	Since time.Time // when the listener started
	// Config is the configuration the client runs with (nil when stopped).
	Config  *config.ClientConfig
	Servers []Server
	VPN     VPNState
	// Bound local proxy addresses ("" when off).
	SOCKSAddr, HTTPAddr string
	// FileReceiver is the file-transfer receive address ("" when off).
	FileReceiver string
}

// Server is one connected proxy server.
type Server struct {
	Addr        string
	ConnectedAt time.Time
}

// VPNState describes the virtual adapter.
type VPNState struct {
	Active    bool
	Interface string
	Address   string // client address/prefix on the adapter
	Gateway   string
}

// Info describes the build, the files and the host.
type Info struct {
	Version, Commit, Built string
	ConfigPath, LogPath    string
	// LocalIPs are this PC's IPv4 addresses, for the server's client list.
	LocalIPs []string
	// Admin reports whether the process runs elevated (VPN needs it).
	Admin bool
	// Wintun reports whether wintun.dll is next to the executable.
	Wintun bool
}
