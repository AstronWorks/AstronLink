// Package vpntest provides a TUN device whose "operating system" side is a
// gVisor stack, so VPN mode can be tested end to end without root, a real
// adapter or OS routing changes. Only tests import it.
package vpntest

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"sync"

	"golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"

	"github.com/AstronWorks/AstronLink/internal/vpn/adapter"
)

const nicID = 1

// Device implements tun.Device. Packets the stack sends are returned by Read;
// packets passed to Write are delivered to the stack. Its NetConfig applies
// addresses and routes to the stack, the way the OS would for a real adapter.
type Device struct {
	stack  *stack.Stack
	ep     *channel.Endpoint
	ctx    context.Context
	cancel context.CancelFunc
	events chan tun.Event
	once   sync.Once

	mu     sync.Mutex
	calls  []string
	routes []tcpip.Route
}

// NewDevice returns a device whose stack has no address until SetAddress.
func NewDevice() *Device {
	st := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	ep := channel.New(1024, 1500, "")
	if err := st.CreateNIC(nicID, ep); err != nil {
		panic(fmt.Sprintf("vpntest: CreateNIC: %v", err))
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Device{stack: st, ep: ep, ctx: ctx, cancel: cancel, events: make(chan tun.Event)}
}

func (d *Device) File() *os.File           { return nil }
func (d *Device) MTU() (int, error)        { return int(d.ep.MTU()), nil }
func (d *Device) Name() (string, error)    { return "vpntest0", nil }
func (d *Device) Events() <-chan tun.Event { return d.events }
func (d *Device) BatchSize() int           { return 1 }

// Read returns the next packet the stack sends towards the adapter.
func (d *Device) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	pkt := d.ep.ReadContext(d.ctx)
	if pkt == nil {
		return 0, os.ErrClosed
	}
	v := pkt.ToView()
	pkt.DecRef()
	sizes[0] = copy(bufs[0][offset:], v.AsSlice())
	v.Release()
	return 1, nil
}

// Write delivers packets to the stack.
func (d *Device) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(b[offset:])})
		d.ep.InjectInbound(ipv4.ProtocolNumber, pkb)
		pkb.DecRef()
	}
	return len(bufs), nil
}

// Close tears the stack down.
func (d *Device) Close() error {
	d.once.Do(func() {
		d.cancel()
		d.ep.Close()
		d.stack.Close()
	})
	return nil
}

// DialTCP opens a TCP connection from the device's stack (an "application").
func (d *Device) DialTCP(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	return gonet.DialContextTCP(ctx, d.stack, fullAddr(dst), ipv4.ProtocolNumber)
}

// DialUDP opens a connected UDP socket from the device's stack.
func (d *Device) DialUDP(dst netip.AddrPort) (net.Conn, error) {
	fa := fullAddr(dst)
	return gonet.DialUDP(d.stack, nil, &fa, ipv4.ProtocolNumber)
}

// Calls returns the NetConfig operations applied so far.
func (d *Device) Calls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

// NetConfig returns the adapter.NetConfig that configures this device.
func (d *Device) NetConfig() adapter.NetConfig { return (*netConfig)(d) }

// NewDeviceFunc / NewNetConfigFunc plug the device into adapter.Options.
func (d *Device) NewDeviceFunc() func(string, int) (tun.Device, error) {
	return func(string, int) (tun.Device, error) { return d, nil }
}

func (d *Device) NewNetConfigFunc() func(tun.Device, *slog.Logger) (adapter.NetConfig, error) {
	return func(tun.Device, *slog.Logger) (adapter.NetConfig, error) { return d.NetConfig(), nil }
}

type netConfig Device

func (n *netConfig) record(s string) {
	n.mu.Lock()
	n.calls = append(n.calls, s)
	n.mu.Unlock()
}

func (n *netConfig) SetAddress(addr netip.Prefix, mtu int) error {
	n.record(fmt.Sprintf("addr %s mtu=%d", addr, mtu))
	pa := tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{Address: tcpip.AddrFrom4(addr.Addr().As4()), PrefixLen: addr.Bits()},
	}
	if err := n.stack.AddProtocolAddress(nicID, pa, stack.AddressProperties{}); err != nil {
		if _, dup := err.(*tcpip.ErrDuplicateAddress); !dup {
			return fmt.Errorf("vpntest: add address: %v", err)
		}
	}
	return nil
}

func (n *netConfig) AddRoute(dst netip.Prefix) error {
	n.record("route+ " + dst.String())
	n.mu.Lock()
	defer n.mu.Unlock()
	n.routes = append(n.routes, tcpip.Route{Destination: subnet(dst), NIC: nicID})
	n.stack.SetRouteTable(n.routes)
	return nil
}

func (n *netConfig) DeleteRoute(dst netip.Prefix) error {
	n.record("route- " + dst.String())
	n.mu.Lock()
	defer n.mu.Unlock()
	out := n.routes[:0]
	for _, r := range n.routes {
		if r.Destination != subnet(dst) {
			out = append(out, r)
		}
	}
	n.routes = out
	n.stack.SetRouteTable(n.routes)
	return nil
}

func (n *netConfig) AddBypass(dst netip.Prefix) (func() error, error) {
	n.record("bypass+ " + dst.String())
	return func() error { n.record("bypass- " + dst.String()); return nil }, nil
}

func (n *netConfig) SetDNS(servers []netip.Addr, full bool) error {
	n.record(fmt.Sprintf("dns %v full=%v", servers, full))
	return nil
}

func (n *netConfig) ClearDNS() error {
	n.record("dns-")
	return nil
}

func subnet(p netip.Prefix) tcpip.Subnet {
	p = p.Masked()
	return tcpip.AddressWithPrefix{Address: tcpip.AddrFrom4(p.Addr().As4()), PrefixLen: p.Bits()}.Subnet()
}

func fullAddr(ap netip.AddrPort) tcpip.FullAddress {
	return tcpip.FullAddress{NIC: nicID, Addr: tcpip.AddrFrom4(ap.Addr().As4()), Port: ap.Port()}
}
