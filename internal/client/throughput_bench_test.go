package client_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/EcoKG/reversproxy/internal/client"
	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/control"
	"github.com/EcoKG/reversproxy/internal/reconnect"
	"github.com/EcoKG/reversproxy/internal/socks"
	"github.com/EcoKG/reversproxy/internal/tunnel"
	"github.com/EcoKG/reversproxy/internal/vpn"
	"github.com/EcoKG/reversproxy/internal/vpn/adapter"
	"github.com/EcoKG/reversproxy/internal/vpn/vpntest"
)

// benchPayload is uploaded once per benchmark iteration.
const benchPayload = 8 << 20

// benchEnv is one server/client pair joined by a TLS 1.3 control connection
// (the production transport), with a SOCKS5 listener and a VPN adapter on the
// client and a discard sink reachable from the server.
type benchEnv struct {
	sink      netip.AddrPort
	socksAddr string
	dev       *vpntest.Device
}

func newBenchEnv(b *testing.B) *benchEnv {
	b.Helper()
	hostIP := localIPv4Bench(b)

	sink, err := net.Listen("tcp", net.JoinHostPort(hostIP.String(), "0"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { sink.Close() })
	go func() {
		for {
			c, err := sink.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				// Ack once the whole payload arrived (no reliance on half-close).
				if _, err := io.CopyN(io.Discard, c, benchPayload); err == nil {
					_, _ = c.Write([]byte{1})
				}
			}()
		}
	}()

	settings, err := vpn.ServerSettingsFromConfig(config.DefaultVPNServerConfig())
	if err != nil {
		b.Fatal(err)
	}
	control.SetVPNSettings(settings)
	control.SetAllowPrivateSOCKSTargets(true)
	b.Cleanup(func() {
		control.SetVPNSettings(vpn.ServerSettings{})
		control.SetAllowPrivateSOCKSTargets(false)
	})

	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)

	dir := b.TempDir()
	cert, err := control.LoadOrGenerateCert(filepath.Join(dir, "c.crt"), filepath.Join(dir, "c.key"))
	if err != nil {
		b.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", control.NewServerTLSConfig(cert))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := tls.Dial("tcp", ln.Addr().String(), control.NewClientTLSConfig(true))
		if err != nil {
			return
		}
		control.HandleControlConn(ctx, conn, control.NewClientRegistry(), "token", quietLog,
			tunnel.NewManager(), "127.0.0.1:8444", tunnel.NewControlConnRegistry())
	}()
	conn, err := ln.Accept()
	if err != nil {
		b.Fatal(err)
	}

	dev := vpntest.NewDevice()
	mgr := adapter.New(adapter.Options{Log: quietLog, NewDevice: dev.NewDeviceFunc(), NewNetConfig: dev.NewNetConfigFunc()})
	b.Cleanup(mgr.Close)

	pool := client.NewServerPool()
	session := &client.ServerSession{
		ID: conn.RemoteAddr().String(), Conn: conn, Writer: client.NewConnWriter(conn),
		Mux: tunnel.NewSOCKSMux(), Addr: conn.RemoteAddr().String(), ConnectedAt: time.Now(), VPN: mgr,
	}
	pool.Add(session)
	go client.HandleServerConn(ctx, session, "token", "bench", &reconnect.ClientConfig{}, quietLog)

	if err := socks.StartClientSOCKSProxy(ctx, "127.0.0.1:0", pool, quietLog, "", ""); err != nil {
		b.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for !mgr.Status().Active {
		if time.Now().After(deadline) {
			b.Fatal("VPN never became active")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return &benchEnv{
		sink:      netip.MustParseAddrPort(sink.Addr().String()),
		socksAddr: socks.LastClientSOCKSAddr,
		dev:       dev,
	}
}

func localIPv4Bench(b *testing.B) netip.Addr {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(ipn.IP.To4()); ok && !ip.IsLoopback() {
				return ip
			}
		}
	}
	b.Skip("no non-loopback IPv4 address")
	return netip.Addr{}
}

// upload sends benchPayload bytes on c and waits for the sink's one-byte ack.
func upload(b *testing.B, c net.Conn, payload []byte) {
	defer c.Close()
	if _, err := c.Write(payload); err != nil {
		b.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
	ack := make([]byte, 1)
	if _, err := io.ReadFull(c, ack); err != nil {
		b.Fatal(err)
	}
}

func socks5Connect(b *testing.B, proxy string, dst netip.AddrPort) net.Conn {
	c, err := net.Dial("tcp", proxy)
	if err != nil {
		b.Fatal(err)
	}
	buf := make([]byte, 10)
	ip := dst.Addr().As4()
	req := []byte{5, 1, 0, 5, 1, 0, 1, ip[0], ip[1], ip[2], ip[3], 0, 0}
	binary.BigEndian.PutUint16(req[11:], dst.Port())
	if _, err := c.Write(req); err != nil {
		b.Fatal(err)
	}
	if _, err := io.ReadFull(c, buf[:2]); err != nil || buf[1] != 0 {
		b.Fatalf("socks5 method: %v %x", err, buf[:2])
	}
	if _, err := io.ReadFull(c, buf); err != nil || buf[1] != 0 {
		b.Fatalf("socks5 connect: %v %x", err, buf)
	}
	return c
}

// BenchmarkUploadThroughput compares one-way TCP upload throughput:
//   - direct: plain TCP to the sink (baseline of the host)
//   - socks5: client SOCKS5 listener → mux frames over the TLS control
//     connection → server dials the sink
//   - vpn:    an application on the client OS (gVisor stack standing in for
//     the kernel) → TUN device → MsgVPNPacket over the same TLS control
//     connection → server userspace stack → server dials the sink
//
//	go test -run '^$' -bench BenchmarkUploadThroughput -benchtime 20x ./internal/client/
func BenchmarkUploadThroughput(b *testing.B) {
	env := newBenchEnv(b)
	payload := bytes.Repeat([]byte("reversproxy-bench"), benchPayload/17+1)[:benchPayload]

	b.Run("direct", func(b *testing.B) {
		b.SetBytes(benchPayload)
		for i := 0; i < b.N; i++ {
			c, err := net.Dial("tcp", env.sink.String())
			if err != nil {
				b.Fatal(err)
			}
			upload(b, c, payload)
		}
	})
	b.Run("socks5", func(b *testing.B) {
		b.SetBytes(benchPayload)
		for i := 0; i < b.N; i++ {
			upload(b, socks5Connect(b, env.socksAddr, env.sink), payload)
		}
	})
	b.Run("vpn", func(b *testing.B) {
		b.SetBytes(benchPayload)
		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			c, err := env.dev.DialTCP(ctx, env.sink)
			cancel()
			if err != nil {
				b.Fatal(err)
			}
			upload(b, c, payload)
		}
	})
}
