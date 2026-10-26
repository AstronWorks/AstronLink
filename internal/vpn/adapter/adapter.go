// Package adapter is the client side of VPN mode: it owns the TUN adapter,
// applies the addressing negotiated with a server, installs routes/DNS and
// moves IP packets between the adapter and the server sessions.
//
// Lifecycle (see package vpn for the protocol):
//
//  1. A server dials in and completes the registration handshake over the
//     physical network. The session reports the server address (TrackPeer) so
//     it gets a bypass route, then sends MsgVPNOpen.
//  2. On MsgVPNConfig the session calls Attach: the first attached session
//     creates the adapter (if needed), assigns the address and installs the
//     bypass routes, the adapter routes and DNS. From then on traffic flows
//     through the adapter.
//  3. With several servers, one session is active; if it ends, the next
//     attached session takes over. When the last one ends the routes and DNS
//     are removed again (the adapter itself is kept until Close), so the next
//     handshake can always reach the client over the physical network.
package adapter

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.zx2c4.com/wireguard/tun"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/vpn"
)

// tunOffset is the headroom kept in front of every packet buffer handed to
// the TUN device (the Linux driver needs room for its virtio-net header).
const tunOffset = 16

// NetConfig applies OS network settings for the adapter. The platform default
// is used unless Options.NewNetConfig is set (tests inject fakes).
type NetConfig interface {
	// SetAddress assigns addr to the adapter (replacing any previous one),
	// applies mtu and brings the adapter up.
	SetAddress(addr netip.Prefix, mtu int) error
	// AddRoute routes dst through the adapter.
	AddRoute(dst netip.Prefix) error
	// DeleteRoute removes a route added by AddRoute.
	DeleteRoute(dst netip.Prefix) error
	// AddBypass routes dst over the best physical (non-adapter) path and
	// returns a function that removes that route again.
	AddBypass(dst netip.Prefix) (undo func() error, err error)
	// SetDNS points the adapter's resolver at servers; fullTunnel asks the OS
	// to prefer them for every domain.
	SetDNS(servers []netip.Addr, fullTunnel bool) error
	// ClearDNS reverts SetDNS.
	ClearDNS() error
}

// PacketWriter sends one IP packet to a server over its control connection.
// pkt is only valid during the call.
type PacketWriter interface {
	WritePacket(pkt []byte) error
}

// Options configures a Manager.
type Options struct {
	// InterfaceName is the adapter name (empty = platform default).
	InterfaceName string
	// Routes go through the adapter (empty = full tunnel).
	Routes []netip.Prefix
	// Bypass always use the physical network.
	Bypass []netip.Prefix
	// DNS overrides the servers pushed by the server (empty = server's).
	DNS []netip.Addr
	// MTU is requested from the server (0 = server default).
	MTU int
	Log *slog.Logger

	// NewDevice creates the TUN device (nil = platform default).
	NewDevice func(name string, mtu int) (tun.Device, error)
	// NewNetConfig returns the configurator for dev (nil = platform default).
	NewNetConfig func(dev tun.Device, log *slog.Logger) (NetConfig, error)
}

// OptionsFromConfig validates the client's vpn config block.
func OptionsFromConfig(c config.VPNClientConfig, log *slog.Logger) (Options, error) {
	routes, err := vpn.ParsePrefixes(c.Routes)
	if err != nil {
		return Options{}, fmt.Errorf("vpn.routes: %w", err)
	}
	bypass, err := vpn.ParsePrefixes(c.Bypass)
	if err != nil {
		return Options{}, fmt.Errorf("vpn.bypass: %w", err)
	}
	dns, err := vpn.ParseAddrs(c.DNS)
	if err != nil {
		return Options{}, fmt.Errorf("vpn.dns: %w", err)
	}
	if c.MTU != 0 && (c.MTU < vpn.MinMTU || c.MTU > vpn.MaxMTU) {
		return Options{}, fmt.Errorf("vpn.mtu %d out of range [%d, %d]", c.MTU, vpn.MinMTU, vpn.MaxMTU)
	}
	return Options{
		InterfaceName: c.InterfaceName,
		Routes:        routes,
		Bypass:        bypass,
		DNS:           dns,
		MTU:           c.MTU,
		Log:           log,
	}, nil
}

type attachment struct {
	id     string
	params vpn.Params
	w      PacketWriter
}

type deviceBox struct{ dev tun.Device }

// Manager owns the TUN adapter and the VPN state shared by all server sessions.
// It is safe for concurrent use.
type Manager struct {
	opts Options
	log  *slog.Logger

	mu       sync.Mutex
	dev      tun.Device
	netcfg   NetConfig
	sessions []*attachment // attached sessions, in attach order
	applied  *vpn.Params   // addressing currently applied (nil = inactive)
	routes   []netip.Prefix
	dnsSet   bool
	bypass   map[netip.Prefix]func() error
	peers    map[netip.Addr]int
	closed   bool

	active  atomic.Pointer[attachment]
	device  atomic.Pointer[deviceBox]
	writeMu sync.Mutex
	dropped atomic.Uint64
}

// New returns a Manager. The adapter is created lazily on the first Attach.
func New(opts Options) *Manager {
	if opts.InterfaceName == "" {
		opts.InterfaceName = defaultInterfaceName
	}
	if opts.NewDevice == nil {
		opts.NewDevice = newPlatformDevice
	}
	if opts.NewNetConfig == nil {
		opts.NewNetConfig = newPlatformNetConfig
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		opts:   opts,
		log:    log.With("subsystem", "vpn"),
		bypass: make(map[netip.Prefix]func() error),
		peers:  make(map[netip.Addr]int),
	}
}

// RequestedMTU is the MTU to put in MsgVPNOpen (0 = server default).
func (m *Manager) RequestedMTU() int { return m.opts.MTU }

// TrackPeer registers the address of a connected server so the control
// connection keeps using the physical network while routes are installed.
// Call it when a session starts; call the returned func when it ends. Bypass
// routes stay in place until the VPN goes inactive, so a server can reconnect
// from the same address while another session keeps the VPN up.
func (m *Manager) TrackPeer(peer netip.Addr) (release func()) {
	peer = peer.Unmap()
	if !peer.IsValid() || peer.IsLoopback() {
		return func() {} // loopback never goes through an adapter
	}
	m.mu.Lock()
	m.peers[peer]++
	if m.applied != nil {
		if err := m.bypassLocked(hostPrefix(peer)); err != nil {
			m.log.Warn("vpn: bypass route for server failed", "server", peer, "err", err)
		}
	}
	m.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			if m.peers[peer]--; m.peers[peer] <= 0 {
				delete(m.peers, peer)
			}
			m.mu.Unlock()
		})
	}
}

// Attach registers a session whose server accepted the VPN request. The first
// attached session activates the adapter. id must be unique per session.
func (m *Manager) Attach(id string, p vpn.Params, w PacketWriter) error {
	if len(m.opts.DNS) > 0 {
		p.DNS = m.opts.DNS
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("vpn: adapter manager closed")
	}
	m.removeLocked(id)
	a := &attachment{id: id, params: p, w: w}
	m.sessions = append(m.sessions, a)
	if m.active.Load() != nil {
		m.log.Info("vpn: standby session attached", "session", id, "address", p.Address)
		return nil
	}
	if err := m.activateLocked(a); err != nil {
		m.removeLocked(id)
		return err
	}
	return nil
}

// Detach unregisters a session. If it was active, the next attached session
// takes over; if none is left the routes and DNS are removed.
func (m *Manager) Detach(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.removeLocked(id) {
		return
	}
	cur := m.active.Load()
	if cur == nil || cur.id != id {
		return
	}
	m.active.Store(nil)
	for len(m.sessions) > 0 {
		next := m.sessions[0]
		err := m.activateLocked(next)
		if err == nil {
			m.log.Info("vpn: failed over to another server session", "session", next.id)
			return
		}
		m.log.Warn("vpn: failover to session failed", "session", next.id, "err", err)
		m.removeLocked(next.id)
	}
	m.teardownLocked()
	m.log.Info("vpn: no server session left — adapter routes and DNS removed until the next handshake")
}

// Inbound writes a packet received from a server to the adapter.
func (m *Manager) Inbound(pkt []byte) {
	box := m.device.Load()
	if box == nil || len(pkt) == 0 {
		return
	}
	buf := make([]byte, tunOffset+len(pkt))
	copy(buf[tunOffset:], pkt)
	m.writeMu.Lock()
	_, err := box.dev.Write([][]byte{buf}, tunOffset)
	m.writeMu.Unlock()
	if err != nil {
		m.log.Debug("vpn: write to adapter failed", "err", err)
	}
}

// GatewayDataAddr returns the server's tunnel data address on the virtual
// network when session id is the active VPN session, so data dial-backs go
// through the adapter instead of the physical path.
func (m *Manager) GatewayDataAddr(id string) (string, bool) {
	a := m.active.Load()
	if a == nil || a.id != id || a.params.DataPort == 0 {
		return "", false
	}
	return net.JoinHostPort(a.params.Gateway.String(), strconv.Itoa(a.params.DataPort)), true
}

// Status describes the VPN state for display.
type Status struct {
	Active        bool
	InterfaceName string
	Address       netip.Prefix
	Gateway       netip.Addr
	Session       string
}

// Status returns a snapshot of the current state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	st := Status{InterfaceName: m.opts.InterfaceName}
	m.mu.Unlock()
	if a := m.active.Load(); a != nil {
		st.Active = true
		st.Address = a.params.Address
		st.Gateway = a.params.Gateway
		st.Session = a.id
	}
	return st
}

// Close removes every setting and destroys the adapter. Idempotent.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.teardownLocked()
	m.sessions = nil
	dev, name := m.dev, m.opts.InterfaceName
	m.dev = nil
	m.device.Store(nil)
	m.mu.Unlock()

	if dev != nil {
		if err := dev.Close(); err != nil {
			m.log.Warn("vpn: closing adapter failed", "err", err)
		}
		m.log.Info("vpn: adapter closed", "interface", name)
	}
}

// activateLocked makes a the active session, (re)configuring the adapter when
// its addressing differs from what is applied.
func (m *Manager) activateLocked(a *attachment) error {
	p := a.params
	if m.applied != nil && m.applied.EqualAddressing(p) {
		m.active.Store(a)
		return nil
	}
	if m.applied != nil {
		m.teardownLocked()
	}
	if err := m.ensureDeviceLocked(p.MTU); err != nil {
		return err
	}
	if err := m.applyLocked(p); err != nil {
		m.teardownLocked()
		return err
	}
	applied := p
	m.applied = &applied
	m.active.Store(a)
	m.log.Info("vpn: adapter active — traffic now flows through the virtual network",
		"interface", m.opts.InterfaceName,
		"address", p.Address,
		"gateway", p.Gateway,
		"mtu", p.MTU,
		"dns", p.DNS,
		"routes", vpn.InstallRoutes(m.opts.Routes),
		"session", a.id,
	)
	return nil
}

func (m *Manager) applyLocked(p vpn.Params) error {
	if err := m.netcfg.SetAddress(p.Address, p.MTU); err != nil {
		return fmt.Errorf("vpn: set adapter address: %w", err)
	}
	// Bypass routes first: once the adapter routes exist, a lookup of the
	// physical path for a server address would resolve to the adapter itself.
	for peer := range m.peers {
		if err := m.bypassLocked(hostPrefix(peer)); err != nil {
			return fmt.Errorf("vpn: bypass route for server %s: %w", peer, err)
		}
	}
	for _, b := range m.opts.Bypass {
		if err := m.bypassLocked(b); err != nil {
			return fmt.Errorf("vpn: bypass route %s: %w", b, err)
		}
	}
	for _, r := range vpn.InstallRoutes(m.opts.Routes) {
		if err := m.netcfg.AddRoute(r); err != nil {
			return fmt.Errorf("vpn: add route %s: %w", r, err)
		}
		m.routes = append(m.routes, r)
	}
	if len(p.DNS) > 0 {
		if err := m.netcfg.SetDNS(p.DNS, vpn.IsFullTunnel(m.opts.Routes)); err != nil {
			// Traffic still flows; only name resolution keeps using the
			// physical resolvers.
			m.log.Warn("vpn: setting adapter DNS failed", "dns", p.DNS, "err", err)
		} else {
			m.dnsSet = true
		}
	}
	return nil
}

// teardownLocked removes DNS, adapter routes and bypass routes. The adapter
// device itself is kept.
func (m *Manager) teardownLocked() {
	m.active.Store(nil)
	if m.netcfg == nil {
		return
	}
	if m.dnsSet {
		if err := m.netcfg.ClearDNS(); err != nil {
			m.log.Warn("vpn: clearing adapter DNS failed", "err", err)
		}
		m.dnsSet = false
	}
	for i := len(m.routes) - 1; i >= 0; i-- {
		if err := m.netcfg.DeleteRoute(m.routes[i]); err != nil {
			m.log.Warn("vpn: removing route failed", "route", m.routes[i], "err", err)
		}
	}
	m.routes = nil
	for dst, undo := range m.bypass {
		if err := undo(); err != nil {
			m.log.Warn("vpn: removing bypass route failed", "route", dst, "err", err)
		}
		delete(m.bypass, dst)
	}
	m.applied = nil
}

func (m *Manager) ensureDeviceLocked(mtu int) error {
	if m.dev != nil {
		return nil
	}
	dev, err := m.opts.NewDevice(m.opts.InterfaceName, mtu)
	if err != nil {
		if privilegeHint != "" {
			return fmt.Errorf("vpn: create adapter %q: %w (%s)", m.opts.InterfaceName, err, privilegeHint)
		}
		return fmt.Errorf("vpn: create adapter %q: %w", m.opts.InterfaceName, err)
	}
	nc, err := m.opts.NewNetConfig(dev, m.log)
	if err != nil {
		_ = dev.Close()
		return err
	}
	if name, err := dev.Name(); err == nil && name != "" {
		m.opts.InterfaceName = name
	}
	m.dev = dev
	m.netcfg = nc
	m.device.Store(&deviceBox{dev: dev})
	go m.readLoop(dev)
	m.log.Info("vpn: adapter created", "interface", m.opts.InterfaceName)
	return nil
}

func (m *Manager) bypassLocked(dst netip.Prefix) error {
	if _, ok := m.bypass[dst]; ok {
		return nil
	}
	undo, err := m.netcfg.AddBypass(dst)
	if err != nil {
		return err
	}
	m.bypass[dst] = undo
	return nil
}

func (m *Manager) removeLocked(id string) bool {
	for i, a := range m.sessions {
		if a.id == id {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return true
		}
	}
	return false
}

// readLoop forwards packets the OS writes to the adapter to the active
// session. Without an active session they are dropped (no leak to the
// physical network: those destinations are routed to the adapter only while
// a session is active).
func (m *Manager) readLoop(dev tun.Device) {
	batch := dev.BatchSize()
	if batch < 1 {
		batch = 1
	}
	bufs := make([][]byte, batch)
	for i := range bufs {
		bufs[i] = make([]byte, tunOffset+vpn.MaxMTU)
	}
	sizes := make([]int, batch)
	failures := 0
	for {
		n, err := dev.Read(bufs, sizes, tunOffset)
		if a := m.active.Load(); a != nil {
			for i := 0; i < n; i++ {
				if werr := a.w.WritePacket(bufs[i][tunOffset : tunOffset+sizes[i]]); werr != nil {
					m.dropped.Add(1)
				}
			}
		} else {
			m.dropped.Add(uint64(n))
		}
		if err == nil {
			failures = 0
			continue
		}
		if errors.Is(err, tun.ErrTooManySegments) {
			continue
		}
		if errors.Is(err, os.ErrClosed) || m.device.Load() == nil {
			return
		}
		if failures++; failures > 50 {
			m.log.Error("vpn: adapter read keeps failing, stopping reader", "err", err)
			return
		}
		m.log.Warn("vpn: adapter read failed", "err", err)
		time.Sleep(100 * time.Millisecond)
	}
}

func hostPrefix(a netip.Addr) netip.Prefix {
	return netip.PrefixFrom(a, a.BitLen())
}
