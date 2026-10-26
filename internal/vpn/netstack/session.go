// Package netstack is the server side of VPN mode: a per-client userspace
// TCP/IP stack that terminates the client's IP packets and dials the real
// destinations (exit node). See package vpn for the overall design.
package netstack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"

	"github.com/AstronWorks/AstronLink/internal/protocol"
	"github.com/AstronWorks/AstronLink/internal/vpn"
)

const (
	nicID = 1

	// outboundQueueLen bounds packets queued from the stack towards the client;
	// when the control connection is slower than the stack, excess packets are
	// dropped and TCP congestion control backs off.
	outboundQueueLen = 1024
	// tcpMaxInFlight bounds concurrent pending TCP handshakes (dials in progress).
	tcpMaxInFlight = 512

	udpIdleTimeout = 2 * time.Minute
	dnsIdleTimeout = 30 * time.Second
	relayBufSize   = 32 * 1024
)

// DialFunc opens an outbound connection on behalf of the virtual network. The
// server passes its SSRF-guarded exit-node dialer.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Resolver resolves names for the gateway DNS responder.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// SessionOptions configures a server-side Session.
type SessionOptions struct {
	Settings vpn.ServerSettings
	// RequestedMTU is the MTU from the client's VPNOpen (0 = server default).
	RequestedMTU int
	// Dial reaches real destinations (required).
	Dial DialFunc
	// Resolver answers gateway DNS queries (nil = net.DefaultResolver).
	Resolver Resolver
	// GatewayTCP maps TCP ports on the gateway address to local handlers that
	// take ownership of the accepted conn (e.g. the tunnel data port).
	// Connections to any other gateway port are refused.
	GatewayTCP map[uint16]func(net.Conn)
	// WritePacket sends one IP packet to the client (required). It is called
	// from a single goroutine; pkt is only valid during the call.
	WritePacket func(pkt []byte) error
	Log         *slog.Logger
}

// Session is the server end of one client's virtual network: a userspace
// TCP/IP stack whose only link is the client's control connection.
type Session struct {
	opts    SessionOptions
	log     *slog.Logger
	mtu     int
	gateway netip.Addr
	subnet  netip.Prefix

	stack *stack.Stack
	ep    *channel.Endpoint

	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	outDone   chan struct{}
}

// NewSession builds the userspace stack and starts forwarding. The session
// lives until Close is called or parent is cancelled.
func NewSession(parent context.Context, opts SessionOptions) (*Session, error) {
	if !opts.Settings.Enabled {
		return nil, errors.New("vpn: disabled on this server")
	}
	if opts.Dial == nil || opts.WritePacket == nil {
		return nil, errors.New("vpn: Dial and WritePacket are required")
	}
	if opts.Resolver == nil {
		opts.Resolver = net.DefaultResolver
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	ctx, cancel := context.WithCancel(parent)
	s := &Session{
		opts:    opts,
		log:     log.With("subsystem", "vpn"),
		mtu:     opts.Settings.NegotiateMTU(opts.RequestedMTU),
		gateway: opts.Settings.Gateway,
		subnet:  opts.Settings.Subnet,
		ctx:     ctx,
		cancel:  cancel,
		outDone: make(chan struct{}),
	}

	st := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6},
	})
	sack := tcpip.TCPSACKEnabled(true)
	rcv := tcpip.TCPReceiveBufferSizeRangeOption{Min: 4 << 10, Default: 256 << 10, Max: 4 << 20}
	snd := tcpip.TCPSendBufferSizeRangeOption{Min: 4 << 10, Default: 256 << 10, Max: 4 << 20}
	moderate := tcpip.TCPModerateReceiveBufferOption(true)
	for _, opt := range []tcpip.SettableTransportProtocolOption{&sack, &rcv, &snd, &moderate} {
		if err := st.SetTransportProtocolOption(tcp.ProtocolNumber, opt); err != nil {
			cancel()
			st.Destroy()
			return nil, fmt.Errorf("vpn: tcp option %T: %v", opt, err)
		}
	}

	ep := channel.New(outboundQueueLen, uint32(s.mtu), "")
	if err := st.CreateNIC(nicID, ep); err != nil {
		cancel()
		st.Destroy()
		return nil, fmt.Errorf("vpn: create NIC: %v", err)
	}
	gw := tcpip.AddressWithPrefix{Address: tcpip.AddrFrom4(s.gateway.As4()), PrefixLen: s.subnet.Bits()}
	if err := st.AddProtocolAddress(nicID, tcpip.ProtocolAddress{Protocol: ipv4.ProtocolNumber, AddressWithPrefix: gw}, stack.AddressProperties{}); err != nil {
		cancel()
		st.Destroy()
		return nil, fmt.Errorf("vpn: add gateway address: %v", err)
	}
	// Promiscuous + spoofing: accept packets for any destination and reply
	// from that destination's address — this is what makes the stack act as
	// the client's router to the whole internet.
	_ = st.SetPromiscuousMode(nicID, true)
	_ = st.SetSpoofing(nicID, true)
	st.SetRouteTable([]tcpip.Route{
		{Destination: header.IPv4EmptySubnet, NIC: nicID},
		{Destination: header.IPv6EmptySubnet, NIC: nicID},
	})

	tcpFwd := tcp.NewForwarder(st, 0, tcpMaxInFlight, s.handleTCP)
	st.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpFwd.HandlePacket)
	udpFwd := udp.NewForwarder(st, s.handleUDP)
	st.SetTransportProtocolHandler(udp.ProtocolNumber, udpFwd.HandlePacket)

	s.stack = st
	s.ep = ep

	go s.outboundLoop()
	context.AfterFunc(ctx, s.Close)
	return s, nil
}

// Config returns the MsgVPNConfig reply describing this session.
func (s *Session) Config(dataPort int) protocol.VPNConfig {
	dns := make([]string, len(s.opts.Settings.DNS))
	for i, a := range s.opts.Settings.DNS {
		dns[i] = a.String()
	}
	return protocol.VPNConfig{
		Status:     "ok",
		ClientAddr: s.opts.Settings.ClientAddr.String(),
		Gateway:    s.gateway.String(),
		MTU:        s.mtu,
		DNS:        dns,
		DataPort:   dataPort,
	}
}

// Inject hands one IP packet received from the client to the stack. The
// packet is copied; pkt may be reused by the caller.
func (s *Session) Inject(pkt []byte) {
	if s.ctx.Err() != nil || len(pkt) == 0 {
		return
	}
	var proto tcpip.NetworkProtocolNumber
	switch pkt[0] >> 4 {
	case 4:
		if len(pkt) < header.IPv4MinimumSize {
			return
		}
		ip := header.IPv4(pkt)
		// The stack would answer echo requests for any address (promiscuous
		// mode), faking reachability. Only the gateway answers pings.
		if ip.Protocol() == uint8(header.ICMPv4ProtocolNumber) && ip.DestinationAddress() != tcpip.AddrFrom4(s.gateway.As4()) {
			return
		}
		proto = ipv4.ProtocolNumber
	case 6:
		if len(pkt) < header.IPv6MinimumSize {
			return
		}
		if header.IPv6(pkt).TransportProtocol() == header.ICMPv6ProtocolNumber {
			return // no IPv6 gateway address; see the ICMPv4 note above
		}
		proto = ipv6.ProtocolNumber
	default:
		return
	}
	pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(pkt)})
	s.ep.InjectInbound(proto, pkb)
	pkb.DecRef()
}

// Close tears down the stack and every relayed connection. Idempotent.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.ep.Close()
		<-s.outDone
		s.stack.Close()
		// Wait for endpoint workers off the caller's goroutine: Close is called
		// from the control-connection cleanup path and must not stall it.
		go s.stack.Wait()
	})
}

// outboundLoop forwards packets produced by the stack to the client.
func (s *Session) outboundLoop() {
	defer close(s.outDone)
	for {
		pkt := s.ep.ReadContext(s.ctx)
		if pkt == nil {
			return
		}
		view := pkt.ToView()
		pkt.DecRef()
		err := s.opts.WritePacket(view.AsSlice())
		view.Release()
		if err != nil && s.ctx.Err() == nil {
			s.log.Debug("vpn: write packet to client failed", "err", err)
		}
	}
}

// allowedDestination reports whether the stack may dial dst on the client's
// behalf. Addresses inside the virtual subnet (other than the gateway, which
// is handled separately) and non-unicast addresses never leave the server.
// The SSRF policy for private/loopback ranges is enforced by Dial.
func (s *Session) allowedDestination(dst netip.Addr) bool {
	if s.subnet.Contains(dst) {
		return false
	}
	if dst.IsUnspecified() || dst.IsMulticast() || dst == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return false
	}
	return true
}

func (s *Session) handleTCP(r *tcp.ForwarderRequest) {
	id := r.ID()
	dst := netip.AddrPortFrom(addrFromTCPIP(id.LocalAddress), id.LocalPort)

	if dst.Addr() == s.gateway {
		h, ok := s.opts.GatewayTCP[dst.Port()]
		if !ok {
			r.Complete(true)
			return
		}
		if c := s.acceptTCP(r); c != nil {
			h(c)
		}
		return
	}
	if !s.allowedDestination(dst.Addr()) {
		r.Complete(true)
		return
	}

	// Dial before completing the handshake so an unreachable/refused target
	// surfaces to the client as a RST instead of an accepted-then-closed conn.
	remote, err := s.opts.Dial(s.ctx, "tcp", dst.String())
	if err != nil {
		s.log.Debug("vpn: tcp dial failed", "dst", dst.String(), "err", err)
		r.Complete(true)
		return
	}
	local := s.acceptTCP(r)
	if local == nil {
		remote.Close()
		return
	}
	s.log.Debug("vpn: tcp flow", "dst", dst.String())
	s.relayTCP(local, remote)
}

// acceptTCP completes the client's handshake and returns the stack-side conn.
func (s *Session) acceptTCP(r *tcp.ForwarderRequest) net.Conn {
	var wq waiter.Queue
	ep, terr := r.CreateEndpoint(&wq)
	if terr != nil {
		r.Complete(true)
		return nil
	}
	r.Complete(false)
	ep.SocketOptions().SetKeepAlive(true)
	return gonet.NewTCPConn(&wq, ep)
}

// relayTCP copies both directions, propagating half-closes, until both
// directions finish or the session ends.
func (s *Session) relayTCP(local, remote net.Conn) {
	stop := context.AfterFunc(s.ctx, func() {
		local.Close()
		remote.Close()
	})
	defer stop()

	var wg sync.WaitGroup
	wg.Add(2)
	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		buf := make([]byte, relayBufSize)
		_, _ = io.CopyBuffer(dst, src, buf)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	go pipe(remote, local)
	go pipe(local, remote)
	wg.Wait()
	local.Close()
	remote.Close()
}

func (s *Session) handleUDP(r *udp.ForwarderRequest) {
	id := r.ID()
	dst := netip.AddrPortFrom(addrFromTCPIP(id.LocalAddress), id.LocalPort)

	builtinDNS := dst.Addr() == s.gateway && dst.Port() == 53 && s.opts.Settings.BuiltinDNS
	if !builtinDNS && !s.allowedDestination(dst.Addr()) {
		return
	}

	var wq waiter.Queue
	ep, terr := r.CreateEndpoint(&wq)
	if terr != nil {
		return
	}
	local := gonet.NewUDPConn(&wq, ep)

	if builtinDNS {
		go s.serveDNS(local)
		return
	}
	go s.relayUDP(local, dst)
}

// relayUDP forwards datagrams of one client flow to dst until the flow has
// been idle (in both directions) for udpIdleTimeout.
func (s *Session) relayUDP(local net.Conn, dst netip.AddrPort) {
	defer local.Close()
	remote, err := s.opts.Dial(s.ctx, "udp", dst.String())
	if err != nil {
		s.log.Debug("vpn: udp dial failed", "dst", dst.String(), "err", err)
		return
	}
	defer remote.Close()
	stop := context.AfterFunc(s.ctx, func() {
		local.Close()
		remote.Close()
	})
	defer stop()

	var lastActive atomic.Int64
	lastActive.Store(time.Now().UnixNano())

	pipe := func(dst, src net.Conn) {
		buf := make([]byte, vpn.MaxMTU)
		for {
			_ = src.SetReadDeadline(time.Now().Add(udpIdleTimeout))
			n, err := src.Read(buf)
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() &&
					time.Since(time.Unix(0, lastActive.Load())) < udpIdleTimeout {
					continue // the other direction is still active
				}
				return
			}
			lastActive.Store(time.Now().UnixNano())
			if _, err := dst.Write(buf[:n]); err != nil {
				return
			}
		}
	}

	done := make(chan struct{})
	go func() {
		pipe(remote, local)
		close(done)
	}()
	pipe(local, remote)
	// One direction ended (idle or error): close both so the other exits.
	local.Close()
	remote.Close()
	<-done
}

func addrFromTCPIP(a tcpip.Address) netip.Addr {
	addr, _ := netip.AddrFromSlice(a.AsSlice())
	return addr.Unmap()
}
