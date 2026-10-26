package client_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/AstronWorks/AstronLink/internal/client"
	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/control"
	"github.com/AstronWorks/AstronLink/internal/reconnect"
	"github.com/AstronWorks/AstronLink/internal/tunnel"
	"github.com/AstronWorks/AstronLink/internal/vpn"
	"github.com/AstronWorks/AstronLink/internal/vpn/adapter"
	"github.com/AstronWorks/AstronLink/internal/vpn/vpntest"
)

var quietLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// localIPv4 returns a non-loopback IPv4 address of this host. The exit node
// cannot be pointed at loopback from inside the virtual network, so the test
// target listens here.
func localIPv4(t *testing.T) netip.Addr {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("no interface addresses: %v", err)
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(ipn.IP.To4()); ok && ip.IsValid() && !ip.IsLoopback() {
				return ip
			}
		}
	}
	t.Skip("no non-loopback IPv4 address available")
	return netip.Addr{}
}

// TestVPNEndToEnd runs the real server control handler and the real client
// session handler over a TCP control connection. The registration handshake
// runs on that connection first; afterwards an "application" on the client's
// OS (a gVisor stack behind the TUN device) reaches a TCP service and the
// gateway DNS purely through the virtual network.
func TestVPNEndToEnd(t *testing.T) {
	hostIP := localIPv4(t)

	echo, err := net.Listen("tcp", net.JoinHostPort(hostIP.String(), "0"))
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()

	settings, err := vpn.ServerSettingsFromConfig(config.DefaultVPNServerConfig())
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	control.SetVPNSettings(settings)
	control.SetAllowPrivateSOCKSTargets(true) // the echo target is on a private/LAN address
	defer control.SetVPNSettings(vpn.ServerSettings{})
	defer control.SetAllowPrivateSOCKSTargets(false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Reversed control plane: the client listens, the server dials.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen control: %v", err)
	}
	defer ln.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Errorf("server dial: %v", err)
			return
		}
		control.HandleControlConn(ctx, conn, control.NewClientRegistry(), "token", quietLog,
			tunnel.NewManager(), "127.0.0.1:8444", tunnel.NewControlConnRegistry())
	}()

	clientConn, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept control: %v", err)
	}

	dev := vpntest.NewDevice()
	mgr := adapter.New(adapter.Options{
		Log:          quietLog,
		NewDevice:    dev.NewDeviceFunc(),
		NewNetConfig: dev.NewNetConfigFunc(),
	})
	defer mgr.Close()

	session := &client.ServerSession{
		ID:          clientConn.RemoteAddr().String(),
		Conn:        clientConn,
		Writer:      client.NewConnWriter(clientConn),
		Mux:         tunnel.NewSOCKSMux(),
		Addr:        clientConn.RemoteAddr().String(),
		ConnectedAt: time.Now(),
		VPN:         mgr,
	}
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client.HandleServerConn(ctx, session, "token", "client1", &reconnect.ClientConfig{}, quietLog)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !mgr.Status().Active {
		if time.Now().After(deadline) {
			t.Fatalf("VPN never became active; adapter calls: %q", dev.Calls())
		}
		time.Sleep(20 * time.Millisecond)
	}

	calls := strings.Join(dev.Calls(), "\n")
	if strings.Contains(calls, "bypass+ 127.0.0.1") {
		t.Fatalf("loopback server address must not get a bypass route:\n%s", calls)
	}
	for _, want := range []string{
		"addr 10.77.0.2/24 mtu=1500",
		"route+ 0.0.0.0/1",
		"route+ 128.0.0.0/1",
		"dns [10.77.0.1] full=true",
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("adapter configuration missing %q:\n%s", want, calls)
		}
	}
	if addr, ok := mgr.GatewayDataAddr(session.ID); !ok || addr != "10.77.0.1:8444" {
		t.Fatalf("GatewayDataAddr = %q, %v", addr, ok)
	}

	// TCP through the adapter → control connection → server stack → target.
	dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
	defer dcancel()
	target := netip.MustParseAddrPort(echo.Addr().String())
	conn, err := dev.DialTCP(dctx, target)
	if err != nil {
		t.Fatalf("dial %s through the VPN: %v", target, err)
	}
	payload := bytes.Repeat([]byte("through-the-adapter/"), 10000) // ~200KB
	go func() {
		_, _ = conn.Write(payload)
		_ = conn.(interface{ CloseWrite() error }).CloseWrite()
	}()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	got, err := io.ReadAll(conn)
	conn.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("echo through VPN: %d/%d bytes, err=%v", len(got), len(payload), err)
	}

	// DNS at the gateway is answered by the server's resolver.
	dns, err := dev.DialUDP(netip.MustParseAddrPort("10.77.0.1:53"))
	if err != nil {
		t.Fatalf("dial gateway DNS: %v", err)
	}
	q := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 7, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("localhost."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
	}
	raw, _ := q.Pack()
	if _, err := dns.Write(raw); err != nil {
		t.Fatalf("send DNS query: %v", err)
	}
	buf := make([]byte, 1500)
	_ = dns.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := dns.Read(buf)
	dns.Close()
	if err != nil {
		t.Fatalf("read DNS reply: %v", err)
	}
	var resp dnsmessage.Message
	if err := resp.Unpack(buf[:n]); err != nil || resp.RCode != dnsmessage.RCodeSuccess || len(resp.Answers) == 0 {
		t.Fatalf("DNS reply rcode=%v answers=%d err=%v", resp.RCode, len(resp.Answers), err)
	}

	// Server goes away: the session ends and the adapter routes/DNS are removed.
	cancel()
	for _, ch := range []chan struct{}{serverDone, clientDone} {
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatal("handlers did not exit after cancellation")
		}
	}
	if mgr.Status().Active {
		t.Fatal("VPN still active after the server session ended")
	}
	calls = strings.Join(dev.Calls(), "\n")
	for _, want := range []string{"dns-", "route- 0.0.0.0/1", "route- 128.0.0.0/1"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("teardown missing %q:\n%s", want, calls)
		}
	}
}

// TestVPNRefusedWhenServerDisabled checks that a server with VPN disabled
// answers with an error and the client keeps working without an adapter.
func TestVPNRefusedWhenServerDisabled(t *testing.T) {
	control.SetVPNSettings(vpn.ServerSettings{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			return
		}
		control.HandleControlConn(ctx, conn, control.NewClientRegistry(), "token", quietLog,
			tunnel.NewManager(), "127.0.0.1:8444", tunnel.NewControlConnRegistry())
	}()
	clientConn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}

	dev := vpntest.NewDevice()
	created := make(chan struct{}, 1)
	mgr := adapter.New(adapter.Options{
		Log: quietLog,
		NewDevice: func(name string, mtu int) (tun.Device, error) {
			created <- struct{}{}
			return dev, nil
		},
		NewNetConfig: dev.NewNetConfigFunc(),
	})
	defer mgr.Close()
	session := &client.ServerSession{
		ID: "s", Conn: clientConn, Writer: client.NewConnWriter(clientConn),
		Mux: tunnel.NewSOCKSMux(), Addr: clientConn.RemoteAddr().String(), VPN: mgr,
	}
	go client.HandleServerConn(ctx, session, "token", "client1", &reconnect.ClientConfig{}, quietLog)

	select {
	case <-created:
		t.Fatal("adapter created although the server refused the VPN session")
	case <-time.After(500 * time.Millisecond):
	}
	if mgr.Status().Active {
		t.Fatal("VPN active although the server refused it")
	}
}
