package netstack

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"

	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/vpn"
)

var testLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// clientOS is a gVisor stack standing in for the client operating system: it
// owns 10.77.0.2/24 and its only link is the VPN session under test.
type clientOS struct {
	stack *stack.Stack
	ep    *channel.Endpoint
}

func newClientOS(t *testing.T, addr netip.Prefix) *clientOS {
	t.Helper()
	st := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	ep := channel.New(1024, 1500, "")
	if err := st.CreateNIC(1, ep); err != nil {
		t.Fatalf("client CreateNIC: %v", err)
	}
	pa := tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{Address: tcpip.AddrFrom4(addr.Addr().As4()), PrefixLen: addr.Bits()},
	}
	if err := st.AddProtocolAddress(1, pa, stack.AddressProperties{}); err != nil {
		t.Fatalf("client AddProtocolAddress: %v", err)
	}
	st.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: 1}})
	t.Cleanup(func() {
		ep.Close()
		st.Close()
	})
	return &clientOS{stack: st, ep: ep}
}

// deliver injects a packet coming from the server into the client stack.
func (c *clientOS) deliver(pkt []byte) error {
	pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(pkt)})
	c.ep.InjectInbound(ipv4.ProtocolNumber, pkb)
	pkb.DecRef()
	return nil
}

// pump forwards packets the client stack emits into sink until ctx ends.
func (c *clientOS) pump(ctx context.Context, sink func([]byte)) {
	for {
		pkt := c.ep.ReadContext(ctx)
		if pkt == nil {
			return
		}
		v := pkt.ToView()
		pkt.DecRef()
		sink(v.AsSlice())
		v.Release()
	}
}

func (c *clientOS) dialTCP(ctx context.Context, dst netip.AddrPort) (*gonet.TCPConn, error) {
	return gonet.DialContextTCP(ctx, c.stack, fullAddr(dst), ipv4.ProtocolNumber)
}

func (c *clientOS) dialUDP(dst netip.AddrPort) (*gonet.UDPConn, error) {
	fa := fullAddr(dst)
	return gonet.DialUDP(c.stack, nil, &fa, ipv4.ProtocolNumber)
}

func fullAddr(ap netip.AddrPort) tcpip.FullAddress {
	return tcpip.FullAddress{NIC: 1, Addr: tcpip.AddrFrom4(ap.Addr().As4()), Port: ap.Port()}
}

func testSettings(t *testing.T) vpn.ServerSettings {
	t.Helper()
	s, err := vpn.ServerSettingsFromConfig(config.DefaultVPNServerConfig())
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	return s
}

// newTestSession wires a Session to a clientOS and returns both.
func newTestSession(t *testing.T, opts SessionOptions) (*Session, *clientOS) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	opts.Settings = testSettings(t)
	opts.Log = testLog
	cos := newClientOS(t, opts.Settings.ClientAddr)
	opts.WritePacket = cos.deliver

	sess, err := NewSession(ctx, opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(sess.Close)
	go cos.pump(ctx, sess.Inject)
	return sess, cos
}

// startEcho starts a TCP echo server on loopback and returns its address.
func startEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

// redirectDial maps every dial to target while recording the requested address.
func redirectDial(target string, seen chan<- string) DialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		select {
		case seen <- network + " " + address:
		default:
		}
		var d net.Dialer
		return d.DialContext(ctx, network, target)
	}
}

func TestSessionForwardsTCP(t *testing.T) {
	seen := make(chan string, 4)
	_, cos := newTestSession(t, SessionOptions{Dial: redirectDial(startEcho(t), seen)})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := cos.dialTCP(ctx, netip.MustParseAddrPort("203.0.113.10:443"))
	if err != nil {
		t.Fatalf("dial through VPN: %v", err)
	}
	defer conn.Close()

	if got := <-seen; got != "tcp 203.0.113.10:443" {
		t.Fatalf("server dialled %q; want the original destination", got)
	}

	payload := bytes.Repeat([]byte("vpn-payload-"), 20000) // ~240KB, many segments
	go func() {
		_, _ = conn.Write(payload)
		_ = conn.CloseWrite()
	}()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("echo mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

func TestSessionRefusesWhenDialFails(t *testing.T) {
	_, cos := newTestSession(t, SessionOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("ssrf guard: refusing")
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c, err := cos.dialTCP(ctx, netip.MustParseAddrPort("203.0.113.10:80")); err == nil {
		c.Close()
		t.Fatal("dial succeeded; want connection refused (RST)")
	}
}

func TestSessionGatewayTCP(t *testing.T) {
	var dialled bool
	var mu sync.Mutex
	_, cos := newTestSession(t, SessionOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			mu.Lock()
			dialled = true
			mu.Unlock()
			return nil, errors.New("unexpected dial")
		},
		GatewayTCP: map[uint16]func(net.Conn){
			8444: func(c net.Conn) {
				defer c.Close()
				_, _ = c.Write([]byte("data-port"))
			},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := cos.dialTCP(ctx, netip.MustParseAddrPort("10.77.0.1:8444"))
	if err != nil {
		t.Fatalf("dial gateway data port: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, _ := io.ReadAll(conn)
	conn.Close()
	if string(got) != "data-port" {
		t.Fatalf("gateway handler reply = %q", got)
	}

	if c, err := cos.dialTCP(ctx, netip.MustParseAddrPort("10.77.0.1:9090")); err == nil {
		c.Close()
		t.Fatal("unmapped gateway port accepted; want refused")
	}
	if c, err := cos.dialTCP(ctx, netip.MustParseAddrPort("10.77.0.9:80")); err == nil {
		c.Close()
		t.Fatal("in-subnet destination accepted; want refused")
	}
	mu.Lock()
	defer mu.Unlock()
	if dialled {
		t.Fatal("gateway/subnet destinations must never reach the exit dialer")
	}
}

func TestSessionForwardsUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(append([]byte("echo:"), buf[:n]...), addr)
		}
	}()

	seen := make(chan string, 4)
	_, cos := newTestSession(t, SessionOptions{Dial: redirectDial(pc.LocalAddr().String(), seen)})

	conn, err := cos.dialUDP(netip.MustParseAddrPort("203.0.113.20:5000"))
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	defer conn.Close()
	buf := make([]byte, 2048)
	for i := 0; i < 3; i++ {
		msg := []byte("datagram-" + string(rune('a'+i)))
		if _, err := conn.Write(msg); err != nil {
			t.Fatalf("write: %v", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("read reply %d: %v", i, err)
		}
		if want := "echo:" + string(msg); string(buf[:n]) != want {
			t.Fatalf("reply = %q; want %q", buf[:n], want)
		}
	}
	if got := <-seen; got != "udp 203.0.113.20:5000" {
		t.Fatalf("server dialled %q", got)
	}
}

// fakeResolver answers from a static table keyed by relative names.
type fakeResolver map[string][]netip.Addr

func (f fakeResolver) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	var out []netip.Addr
	for _, a := range f[host] {
		if network == "ip" || (network == "ip4") == a.Is4() {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return out, nil
}

func dnsQuery(t *testing.T, conn net.Conn, name string, qtype dnsmessage.Type) dnsmessage.Message {
	t.Helper()
	q := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 0x4242, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(name), Type: qtype, Class: dnsmessage.ClassINET}},
	}
	raw, err := q.Pack()
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if _, err := conn.Write(raw); err != nil {
		t.Fatalf("write query: %v", err)
	}
	buf := make([]byte, 1500)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	var resp dnsmessage.Message
	if err := resp.Unpack(buf[:n]); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if resp.Header.ID != 0x4242 || !resp.Header.Response {
		t.Fatalf("bad reply header: %+v", resp.Header)
	}
	return resp
}

func TestSessionGatewayDNS(t *testing.T) {
	_, cos := newTestSession(t, SessionOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("gateway DNS must not use the exit dialer")
		},
		Resolver: fakeResolver{"example.test": {netip.MustParseAddr("192.0.2.7")}},
	})
	conn, err := cos.dialUDP(netip.MustParseAddrPort("10.77.0.1:53"))
	if err != nil {
		t.Fatalf("dial dns: %v", err)
	}
	defer conn.Close()

	resp := dnsQuery(t, conn, "example.test.", dnsmessage.TypeA)
	if resp.RCode != dnsmessage.RCodeSuccess || len(resp.Answers) != 1 {
		t.Fatalf("A reply: rcode=%v answers=%d", resp.RCode, len(resp.Answers))
	}
	if a, ok := resp.Answers[0].Body.(*dnsmessage.AResource); !ok || netip.AddrFrom4(a.A) != netip.MustParseAddr("192.0.2.7") {
		t.Fatalf("A answer = %v", resp.Answers[0].Body)
	}

	// Name exists but has no AAAA: NODATA, not NXDOMAIN.
	resp = dnsQuery(t, conn, "example.test.", dnsmessage.TypeAAAA)
	if resp.RCode != dnsmessage.RCodeSuccess || len(resp.Answers) != 0 {
		t.Fatalf("AAAA reply: rcode=%v answers=%d; want NOERROR/0", resp.RCode, len(resp.Answers))
	}

	resp = dnsQuery(t, conn, "missing.test.", dnsmessage.TypeA)
	if resp.RCode != dnsmessage.RCodeNameError {
		t.Fatalf("missing name rcode = %v; want NXDOMAIN", resp.RCode)
	}

	resp = dnsQuery(t, conn, "example.test.", dnsmessage.TypeTXT)
	if resp.RCode != dnsmessage.RCodeSuccess || len(resp.Answers) != 0 {
		t.Fatalf("TXT reply: rcode=%v answers=%d; want NODATA", resp.RCode, len(resp.Answers))
	}
}

func TestSessionConfig(t *testing.T) {
	sess, _ := newTestSession(t, SessionOptions{
		RequestedMTU: 1400,
		Dial:         func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("x") },
	})
	vc := sess.Config(8444)
	p, err := vpn.ParamsFromConfig(vc)
	if err != nil {
		t.Fatalf("ParamsFromConfig(%+v): %v", vc, err)
	}
	if p.Address.String() != "10.77.0.2/24" || p.Gateway.String() != "10.77.0.1" || p.MTU != 1400 || p.DataPort != 8444 {
		t.Fatalf("params = %+v", p)
	}
	if len(p.DNS) != 1 || p.DNS[0] != p.Gateway {
		t.Fatalf("default DNS = %v; want the gateway", p.DNS)
	}
}
