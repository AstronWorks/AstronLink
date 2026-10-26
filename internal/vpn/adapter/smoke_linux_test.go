//go:build linux

package adapter

import (
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/EcoKG/reversproxy/internal/vpn"
)

// TestRealTUNSmoke drives a real TUN adapter and the real iproute2
// configuration. It changes the host's network configuration (split route to
// the 198.18.0.0/15 benchmark range only), so it runs only when explicitly
// requested:
//
//	sudo RPROXY_TUN_SMOKE=1 go test -run TestRealTUNSmoke ./internal/vpn/adapter/
func TestRealTUNSmoke(t *testing.T) {
	if os.Getenv("RPROXY_TUN_SMOKE") != "1" {
		t.Skip("set RPROXY_TUN_SMOKE=1 (as root) to run against a real TUN adapter")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}

	m := New(Options{
		InterfaceName: "rptest0",
		Routes:        []netip.Prefix{netip.MustParsePrefix("198.18.0.0/15")},
		Log:           testLog,
	})
	defer m.Close()

	peer := netip.MustParseAddr("192.0.2.99")
	release := m.TrackPeer(peer)
	defer release()

	sink := newSink()
	p := vpn.Params{
		Address: netip.MustParsePrefix("10.77.0.2/24"),
		Gateway: netip.MustParseAddr("10.77.0.1"),
		MTU:     1400,
	}
	if err := m.Attach("smoke", p, sink); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	mustContain(t, run(t, "ip", "-4", "addr", "show", "dev", "rptest0"), "10.77.0.2/24")
	mustContain(t, run(t, "ip", "-4", "route", "show", "198.18.0.0/15"), "dev rptest0")
	mustContain(t, run(t, "ip", "-4", "route", "show", "192.0.2.99"), "192.0.2.99")
	mustContain(t, run(t, "ip", "link", "show", "dev", "rptest0"), "mtu 1400")

	// An application packet to a routed destination must come out of the
	// adapter towards the active session.
	sock, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(10, 77, 0, 2)})
	if err != nil {
		t.Fatalf("bind on adapter address: %v", err)
	}
	defer sock.Close()
	dst := &net.UDPAddr{IP: net.IPv4(198, 18, 0, 5), Port: 9999}
	if _, err := sock.WriteToUDP([]byte("ping"), dst); err != nil {
		t.Fatalf("send: %v", err)
	}
	var pkt []byte
	deadline := time.After(5 * time.Second)
	for pkt == nil {
		select {
		case got := <-sink.ch:
			// Skip unrelated traffic the OS may emit on a new interface.
			if len(got) >= 28 && got[0]>>4 == 4 && got[9] == 17 && net.IP(got[16:20]).Equal(dst.IP) {
				pkt = got
			}
		case <-deadline:
			t.Fatal("UDP packet to a routed destination never left the adapter")
		}
	}
	srcPort := binary.BigEndian.Uint16(pkt[20:22])
	if string(pkt[28:]) != "ping" {
		t.Fatalf("payload = %q", pkt[28:])
	}

	// A reply written to the adapter must reach the application socket.
	m.Inbound(udpPacket(netip.MustParseAddr("198.18.0.5"), 9999, netip.MustParseAddr("10.77.0.2"), srcPort, []byte("pong")))
	_ = sock.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	n, from, err := sock.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("reply never delivered to the socket: %v", err)
	}
	if string(buf[:n]) != "pong" || !from.IP.Equal(dst.IP) {
		t.Fatalf("reply = %q from %v", buf[:n], from)
	}

	// Last session gone: routes and bypass removed, adapter kept.
	m.Detach("smoke")
	if out := run(t, "ip", "-4", "route", "show", "198.18.0.0/15"); strings.TrimSpace(out) != "" {
		t.Fatalf("adapter route left behind: %q", out)
	}
	if out := run(t, "ip", "-4", "route", "show", "192.0.2.99"); strings.TrimSpace(out) != "" {
		t.Fatalf("bypass route left behind: %q", out)
	}

	m.Close()
	if err := exec.Command("ip", "link", "show", "dev", "rptest0").Run(); err == nil {
		t.Fatal("adapter still present after Close")
	}
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
	return string(out)
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("expected %q in:\n%s", sub, s)
	}
}

// udpPacket builds an IPv4/UDP packet with valid checksums.
func udpPacket(src netip.Addr, sport uint16, dst netip.Addr, dport uint16, payload []byte) []byte {
	pkt := make([]byte, 28+len(payload))
	ip := pkt[:20]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:], uint16(len(pkt)))
	ip[8] = 64
	ip[9] = 17
	s4, d4 := src.As4(), dst.As4()
	copy(ip[12:16], s4[:])
	copy(ip[16:20], d4[:])
	binary.BigEndian.PutUint16(ip[10:], checksum(ip, 0))

	udp := pkt[20:]
	binary.BigEndian.PutUint16(udp[0:], sport)
	binary.BigEndian.PutUint16(udp[2:], dport)
	binary.BigEndian.PutUint16(udp[4:], uint16(len(udp)))
	copy(udp[8:], payload)
	var pseudo uint32
	for i := 0; i < 4; i += 2 {
		pseudo += uint32(binary.BigEndian.Uint16(s4[i:])) + uint32(binary.BigEndian.Uint16(d4[i:]))
	}
	pseudo += 17 + uint32(len(udp))
	binary.BigEndian.PutUint16(udp[6:], checksum(udp, pseudo))
	return pkt
}

func checksum(b []byte, initial uint32) uint16 {
	sum := initial
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}
