// Package vpn implements VPN (virtual network adapter) mode.
//
// The registration handshake between server and client always runs over the
// physical network. Afterwards a VPN-enabled client asks for a virtual-network
// session (MsgVPNOpen) and the two sides exchange raw IP packets
// (MsgVPNPacket) over the same control connection:
//
//	app ──► TUN adapter (Client) ──MsgVPNPacket──► Session (server) ──► internet
//	        10.77.0.2                              userspace TCP/IP stack,
//	                                               gateway 10.77.0.1
//
// Client side (package adapter): creates the TUN adapter after the first
// successful handshake, assigns the address pushed by the server, installs
// routes/DNS and keeps the control connection itself on the physical path
// (bypass route).
//
// Server side (package netstack): one gVisor userspace stack per control
// connection terminates the client's TCP/UDP flows and dials the real
// destinations with ordinary sockets (exit node), so the server needs no TUN
// device and no OS privileges. The gateway address answers DNS and the tunnel
// data port, so data dial-backs also travel through the adapter.
//
// This package holds what both sides share: settings and negotiated parameters.
package vpn

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/protocol"
)

const (
	DefaultMTU = 1500
	MinMTU     = 576
	MaxMTU     = 65535
)

// ServerSettings is the validated form of config.VPNServerConfig.
type ServerSettings struct {
	Enabled bool
	// Subnet is the virtual network; Gateway and ClientAddr are its first two hosts.
	Subnet     netip.Prefix
	Gateway    netip.Addr
	ClientAddr netip.Prefix
	MTU        int
	// DNS is what clients are told to use. BuiltinDNS reports whether the
	// gateway resolver answers queries (the gateway is one of the DNS servers).
	DNS        []netip.Addr
	BuiltinDNS bool
}

// ServerSettingsFromConfig validates c. A disabled config is returned as-is
// (Enabled=false) without further validation.
func ServerSettingsFromConfig(c config.VPNServerConfig) (ServerSettings, error) {
	if !c.Enabled {
		return ServerSettings{}, nil
	}
	subnet, err := netip.ParsePrefix(strings.TrimSpace(c.Subnet))
	if err != nil {
		return ServerSettings{}, fmt.Errorf("vpn.subnet: %w", err)
	}
	subnet = subnet.Masked()
	if !subnet.Addr().Is4() {
		return ServerSettings{}, fmt.Errorf("vpn.subnet %s: only IPv4 subnets are supported", subnet)
	}
	if subnet.Bits() > 30 {
		return ServerSettings{}, fmt.Errorf("vpn.subnet %s: prefix must be /30 or shorter (gateway + client)", subnet)
	}
	mtu := c.MTU
	if mtu == 0 {
		mtu = DefaultMTU
	}
	if mtu < MinMTU || mtu > MaxMTU {
		return ServerSettings{}, fmt.Errorf("vpn.mtu %d out of range [%d, %d]", mtu, MinMTU, MaxMTU)
	}
	gateway := subnet.Addr().Next()
	client := gateway.Next()
	s := ServerSettings{
		Enabled:    true,
		Subnet:     subnet,
		Gateway:    gateway,
		ClientAddr: netip.PrefixFrom(client, subnet.Bits()),
		MTU:        mtu,
	}
	if len(c.DNS) == 0 {
		s.DNS = []netip.Addr{gateway}
		s.BuiltinDNS = true
	} else {
		dns, err := ParseAddrs(c.DNS)
		if err != nil {
			return ServerSettings{}, fmt.Errorf("vpn.dns: %w", err)
		}
		s.DNS = dns
		for _, a := range dns {
			if a == gateway {
				s.BuiltinDNS = true
			}
		}
	}
	return s, nil
}

// NegotiateMTU returns the MTU for a session given the client's request.
func (s ServerSettings) NegotiateMTU(requested int) int {
	if requested >= MinMTU && requested < s.MTU {
		return requested
	}
	return s.MTU
}

// Params is the virtual-network addressing negotiated for one session, as the
// client applies it to its adapter.
type Params struct {
	Address  netip.Prefix // client address, e.g. 10.77.0.2/24
	Gateway  netip.Addr   // server address, e.g. 10.77.0.1
	MTU      int
	DNS      []netip.Addr
	DataPort int // tunnel data port reachable at Gateway (0 = not offered)
}

// ParamsFromConfig validates a server's MsgVPNConfig reply.
func ParamsFromConfig(vc protocol.VPNConfig) (Params, error) {
	if vc.Status != "ok" {
		return Params{}, fmt.Errorf("server refused VPN session: %s", vc.Error)
	}
	addr, err := netip.ParsePrefix(vc.ClientAddr)
	if err != nil {
		return Params{}, fmt.Errorf("invalid client address %q: %w", vc.ClientAddr, err)
	}
	gw, err := netip.ParseAddr(vc.Gateway)
	if err != nil {
		return Params{}, fmt.Errorf("invalid gateway %q: %w", vc.Gateway, err)
	}
	if !addr.Masked().Contains(gw) || addr.Addr() == gw {
		return Params{}, fmt.Errorf("gateway %s is not a peer address in %s", gw, addr)
	}
	if vc.MTU < MinMTU || vc.MTU > MaxMTU {
		return Params{}, fmt.Errorf("invalid MTU %d", vc.MTU)
	}
	if vc.DataPort < 0 || vc.DataPort > 65535 {
		return Params{}, fmt.Errorf("invalid data port %d", vc.DataPort)
	}
	dns, err := ParseAddrs(vc.DNS)
	if err != nil {
		return Params{}, fmt.Errorf("invalid DNS list: %w", err)
	}
	return Params{Address: addr, Gateway: gw, MTU: vc.MTU, DNS: dns, DataPort: vc.DataPort}, nil
}

// EqualAddressing reports whether two parameter sets configure the adapter
// identically (so a failover between sessions needs no reconfiguration).
func (p Params) EqualAddressing(o Params) bool {
	if p.Address != o.Address || p.Gateway != o.Gateway || p.MTU != o.MTU || len(p.DNS) != len(o.DNS) {
		return false
	}
	for i := range p.DNS {
		if p.DNS[i] != o.DNS[i] {
			return false
		}
	}
	return true
}

// ParseAddrs parses a list of IP addresses.
func ParseAddrs(list []string) ([]netip.Addr, error) {
	out := make([]netip.Addr, 0, len(list))
	for _, s := range list {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil {
			return nil, err
		}
		out = append(out, a.Unmap())
	}
	return out, nil
}

// ParsePrefixes parses CIDRs; a bare address is treated as a host route.
func ParsePrefixes(list []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if !strings.Contains(s, "/") {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, err
			}
			a = a.Unmap()
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// InstallRoutes expands the configured routes into the prefixes actually
// installed on the adapter. Empty means full tunnel. A default route is split
// into two half-default routes so the original default route stays in place
// and keeps carrying the control connection and bypass traffic.
func InstallRoutes(routes []netip.Prefix) []netip.Prefix {
	if len(routes) == 0 {
		routes = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
	}
	seen := make(map[netip.Prefix]bool)
	var out []netip.Prefix
	add := func(p netip.Prefix) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, r := range routes {
		r = r.Masked()
		if r.Bits() != 0 {
			add(r)
			continue
		}
		if r.Addr().Is4() {
			add(netip.MustParsePrefix("0.0.0.0/1"))
			add(netip.MustParsePrefix("128.0.0.0/1"))
		} else {
			add(netip.MustParsePrefix("::/1"))
			add(netip.MustParsePrefix("8000::/1"))
		}
	}
	return out
}

// IsFullTunnel reports whether the configured routes capture all IPv4 traffic.
func IsFullTunnel(routes []netip.Prefix) bool {
	if len(routes) == 0 {
		return true
	}
	for _, r := range routes {
		if r.Bits() == 0 && r.Addr().Is4() {
			return true
		}
	}
	return false
}
