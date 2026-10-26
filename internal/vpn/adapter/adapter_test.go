package adapter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/tun"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/vpn"
)

var testLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeDevice is an in-memory tun.Device: fromOS feeds Read, Write records to toOS.
type fakeDevice struct {
	fromOS chan []byte
	toOS   chan []byte
	closed chan struct{}
	once   sync.Once
	events chan tun.Event
}

func newFakeDevice() *fakeDevice {
	return &fakeDevice{
		fromOS: make(chan []byte, 16),
		toOS:   make(chan []byte, 16),
		closed: make(chan struct{}),
		events: make(chan tun.Event),
	}
}

func (d *fakeDevice) File() *os.File           { return nil }
func (d *fakeDevice) MTU() (int, error)        { return 1500, nil }
func (d *fakeDevice) Name() (string, error)    { return "fake0", nil }
func (d *fakeDevice) Events() <-chan tun.Event { return d.events }
func (d *fakeDevice) BatchSize() int           { return 1 }

func (d *fakeDevice) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	select {
	case pkt := <-d.fromOS:
		sizes[0] = copy(bufs[0][offset:], pkt)
		return 1, nil
	case <-d.closed:
		return 0, os.ErrClosed
	}
}

func (d *fakeDevice) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		d.toOS <- append([]byte(nil), b[offset:]...)
	}
	return len(bufs), nil
}

func (d *fakeDevice) Close() error {
	d.once.Do(func() { close(d.closed) })
	return nil
}

// fakeNetConfig records every OS change as a short string.
type fakeNetConfig struct {
	mu       sync.Mutex
	calls    []string
	failNext map[string]error
}

func (f *fakeNetConfig) record(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	for prefix, err := range f.failNext {
		if strings.HasPrefix(call, prefix) {
			delete(f.failNext, prefix)
			return err
		}
	}
	return nil
}

func (f *fakeNetConfig) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *fakeNetConfig) SetAddress(addr netip.Prefix, mtu int) error {
	return f.record(fmt.Sprintf("addr %s mtu=%d", addr, mtu))
}
func (f *fakeNetConfig) AddRoute(dst netip.Prefix) error { return f.record("route+ " + dst.String()) }
func (f *fakeNetConfig) DeleteRoute(dst netip.Prefix) error {
	return f.record("route- " + dst.String())
}
func (f *fakeNetConfig) AddBypass(dst netip.Prefix) (func() error, error) {
	if err := f.record("bypass+ " + dst.String()); err != nil {
		return nil, err
	}
	return func() error { return f.record("bypass- " + dst.String()) }, nil
}
func (f *fakeNetConfig) SetDNS(servers []netip.Addr, full bool) error {
	return f.record(fmt.Sprintf("dns %v full=%v", servers, full))
}
func (f *fakeNetConfig) ClearDNS() error { return f.record("dns-") }

// packetSink is a PacketWriter collecting packets sent to a server.
type packetSink struct{ ch chan []byte }

func newSink() *packetSink { return &packetSink{ch: make(chan []byte, 16)} }

func (s *packetSink) WritePacket(pkt []byte) error {
	s.ch <- append([]byte(nil), pkt...)
	return nil
}

type harness struct {
	m       *Manager
	dev     *fakeDevice
	nc      *fakeNetConfig
	created int
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	h := &harness{dev: newFakeDevice(), nc: &fakeNetConfig{failNext: map[string]error{}}}
	opts.Log = testLog
	opts.NewDevice = func(name string, mtu int) (tun.Device, error) {
		h.created++
		return h.dev, nil
	}
	opts.NewNetConfig = func(tun.Device, *slog.Logger) (NetConfig, error) { return h.nc, nil }
	h.m = New(opts)
	t.Cleanup(h.m.Close)
	return h
}

func params(addr, gw string, dataPort int) vpn.Params {
	g := netip.MustParseAddr(gw)
	return vpn.Params{
		Address:  netip.MustParsePrefix(addr),
		Gateway:  g,
		MTU:      1500,
		DNS:      []netip.Addr{g},
		DataPort: dataPort,
	}
}

func expectCalls(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("OS changes:\n  got  %q\n  want %q", got, want)
	}
}

func TestAttachActivatesAdapter(t *testing.T) {
	h := newHarness(t, Options{})
	release := h.m.TrackPeer(netip.MustParseAddr("203.0.113.5"))
	defer release()

	if st := h.m.Status(); st.Active {
		t.Fatal("active before any session attached")
	}
	if err := h.m.Attach("s1", params("10.77.0.2/24", "10.77.0.1", 8444), newSink()); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if h.created != 1 {
		t.Fatalf("adapter created %d times; want 1", h.created)
	}
	expectCalls(t, h.nc.take(),
		"addr 10.77.0.2/24 mtu=1500",
		"bypass+ 203.0.113.5/32", // control connection stays on the physical path
		"route+ 0.0.0.0/1",
		"route+ 128.0.0.0/1",
		"dns [10.77.0.1] full=true",
	)
	st := h.m.Status()
	if !st.Active || st.Address.String() != "10.77.0.2/24" || st.InterfaceName != "fake0" {
		t.Fatalf("status = %+v", st)
	}
	if addr, ok := h.m.GatewayDataAddr("s1"); !ok || addr != "10.77.0.1:8444" {
		t.Fatalf("GatewayDataAddr = %q, %v", addr, ok)
	}
	if _, ok := h.m.GatewayDataAddr("other"); ok {
		t.Fatal("GatewayDataAddr returned an address for a non-active session")
	}
}

func TestPacketsFlowBothWays(t *testing.T) {
	h := newHarness(t, Options{})
	sink := newSink()
	if err := h.m.Attach("s1", params("10.77.0.2/24", "10.77.0.1", 0), sink); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	out := []byte{0x45, 1, 2, 3}
	h.dev.fromOS <- out
	select {
	case got := <-sink.ch:
		if !bytes.Equal(got, out) {
			t.Fatalf("server got %x; want %x", got, out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("packet read from the adapter never reached the session")
	}

	in := []byte{0x45, 9, 8, 7}
	h.m.Inbound(in)
	select {
	case got := <-h.dev.toOS:
		if !bytes.Equal(got, in) {
			t.Fatalf("adapter got %x; want %x", got, in)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("inbound packet never written to the adapter")
	}
}

func TestFailoverAndTeardown(t *testing.T) {
	h := newHarness(t, Options{Routes: []netip.Prefix{netip.MustParsePrefix("192.168.50.0/24")}})
	release := h.m.TrackPeer(netip.MustParseAddr("198.51.100.1"))
	a, b := newSink(), newSink()
	p := params("10.77.0.2/24", "10.77.0.1", 0)
	if err := h.m.Attach("A", p, a); err != nil {
		t.Fatalf("Attach A: %v", err)
	}
	if err := h.m.Attach("B", p, b); err != nil {
		t.Fatalf("Attach B: %v", err)
	}
	h.nc.take()

	// Same addressing: the standby takes over without touching the OS.
	h.m.Detach("A")
	expectCalls(t, h.nc.take())
	if st := h.m.Status(); !st.Active || st.Session != "B" {
		t.Fatalf("after failover status = %+v", st)
	}
	h.dev.fromOS <- []byte{0x45}
	select {
	case <-b.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("packets not routed to the standby session after failover")
	}

	release()
	h.m.Detach("B")
	expectCalls(t, h.nc.take(),
		"dns-",
		"route- 192.168.50.0/24",
		"bypass- 198.51.100.1/32",
	)
	if st := h.m.Status(); st.Active {
		t.Fatalf("still active after the last session left: %+v", st)
	}

	// Next handshake re-applies everything on the same adapter.
	if err := h.m.Attach("C", p, newSink()); err != nil {
		t.Fatalf("Attach C: %v", err)
	}
	expectCalls(t, h.nc.take(),
		"addr 10.77.0.2/24 mtu=1500",
		"route+ 192.168.50.0/24",
		"dns [10.77.0.1] full=false",
	)
	if h.created != 1 {
		t.Fatalf("adapter re-created (%d); it must be reused", h.created)
	}
}

func TestFailoverWithDifferentAddressingReconfigures(t *testing.T) {
	h := newHarness(t, Options{Routes: []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")}})
	if err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err != nil {
		t.Fatal(err)
	}
	if err := h.m.Attach("B", params("10.88.0.2/24", "10.88.0.1", 0), newSink()); err != nil {
		t.Fatal(err)
	}
	h.nc.take()
	h.m.Detach("A")
	expectCalls(t, h.nc.take(),
		"dns-",
		"route- 10.20.0.0/16",
		"addr 10.88.0.2/24 mtu=1500",
		"route+ 10.20.0.0/16",
		"dns [10.88.0.1] full=false",
	)
}

func TestTrackPeerWhileActiveIsSticky(t *testing.T) {
	h := newHarness(t, Options{})
	if err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err != nil {
		t.Fatal(err)
	}
	h.nc.take()
	release := h.m.TrackPeer(netip.MustParseAddr("192.0.2.44"))
	expectCalls(t, h.nc.take(), "bypass+ 192.0.2.44/32")
	// The server may reconnect from the same address while the VPN is up, so
	// the bypass stays until the VPN goes inactive.
	release()
	expectCalls(t, h.nc.take())
	h.m.Detach("A")
	calls := h.nc.take()
	if calls[len(calls)-1] != "bypass- 192.0.2.44/32" {
		t.Fatalf("bypass not removed on teardown: %q", calls)
	}
}

func TestAttachFailureRollsBack(t *testing.T) {
	h := newHarness(t, Options{})
	h.m.TrackPeer(netip.MustParseAddr("203.0.113.5"))
	h.nc.failNext["route+ 128.0.0.0/1"] = errors.New("boom")

	err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Attach error = %v; want the route failure", err)
	}
	expectCalls(t, h.nc.take(),
		"addr 10.77.0.2/24 mtu=1500",
		"bypass+ 203.0.113.5/32",
		"route+ 0.0.0.0/1",
		"route+ 128.0.0.0/1",
		"route- 0.0.0.0/1",
		"bypass- 203.0.113.5/32",
	)
	if st := h.m.Status(); st.Active {
		t.Fatal("active after a failed Attach")
	}
	if err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err != nil {
		t.Fatalf("retry Attach: %v", err)
	}
}

// attachLoopback attaches a full-or-split tunnel session to a server on this host and returns the harness and Attach error.
func attachLoopback(t *testing.T, peer string, routes []netip.Prefix) (*harness, error) {
	t.Helper()
	h := newHarness(t, Options{Routes: routes})
	h.m.TrackPeer(netip.MustParseAddr(peer))
	return h, h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink())
}

// expectLoopbackRefused fails unless Attach was refused with no OS change and an inactive adapter.
func expectLoopbackRefused(t *testing.T, h *harness, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "full tunnel to a server on this host") {
		t.Fatalf("Attach error = %v; want a full-tunnel-on-this-host refusal", err)
	}
	expectCalls(t, h.nc.take())
	if st := h.m.Status(); st.Active {
		t.Fatal("active after a refused Attach")
	}
}

func TestFullTunnelRefusedForLoopbackPeer(t *testing.T) {
	h, err := attachLoopback(t, "127.0.0.1", nil)
	expectLoopbackRefused(t, h, err)
}

func TestFullTunnelRefusedForIPv6LoopbackPeer(t *testing.T) {
	h, err := attachLoopback(t, "::1", nil)
	expectLoopbackRefused(t, h, err)
}

func TestFullTunnelRefusedForMappedLoopbackPeer(t *testing.T) {
	h, err := attachLoopback(t, "::ffff:127.0.0.1", nil)
	expectLoopbackRefused(t, h, err)
}

func TestExplicitDefaultRouteRefusedForLoopbackPeer(t *testing.T) {
	h, err := attachLoopback(t, "127.0.0.1", []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")})
	expectLoopbackRefused(t, h, err)
}

func TestSplitTunnelAllowedForLoopbackPeer(t *testing.T) {
	h, err := attachLoopback(t, "127.0.0.1", []netip.Prefix{netip.MustParsePrefix("198.18.0.0/15")})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	for _, c := range h.nc.take() {
		if strings.HasPrefix(c, "bypass") {
			t.Fatalf("loopback server must not get a bypass route: %q", c)
		}
	}
}

func TestLoopbackPeerReleaseAllowsFullTunnel(t *testing.T) {
	h := newHarness(t, Options{})
	loop := netip.MustParseAddr("127.0.0.1")
	r1, r2 := h.m.TrackPeer(loop), h.m.TrackPeer(loop)
	r1()
	r1() // a second call of the same release must not decrement again
	err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink())
	expectLoopbackRefused(t, h, err)
	r2()
	if err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err != nil {
		t.Fatalf("Attach after every loopback peer released: %v", err)
	}
}

func TestLoopbackPeerTrackConcurrentWithAttach(t *testing.T) {
	h := newHarness(t, Options{})
	loop := netip.MustParseAddr("127.0.0.1")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			h.m.TrackPeer(loop)()
		}()
		go func(i int) {
			defer wg.Done()
			_ = h.m.Attach(fmt.Sprintf("s%d", i), params("10.77.0.2/24", "10.77.0.1", 0), newSink())
		}(i)
	}
	wg.Wait()
	for i := 0; i < 16; i++ {
		h.m.Detach(fmt.Sprintf("s%d", i))
	}
	if err := h.m.Attach("final", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err != nil {
		t.Fatalf("counter leaked after concurrent track/release: %v", err)
	}
}

func TestStandbyLoopbackSessionRefusedWhileRemoteActive(t *testing.T) {
	h := newHarness(t, Options{})
	h.m.TrackPeer(netip.MustParseAddr("203.0.113.5"))
	if err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err != nil {
		t.Fatalf("Attach A: %v", err)
	}
	h.nc.take()
	h.m.TrackPeer(netip.MustParseAddr("127.0.0.1"))
	err := h.m.Attach("B", params("10.77.0.2/24", "10.77.0.1", 0), newSink())
	if err == nil || !strings.Contains(err.Error(), "full tunnel to a server on this host") {
		t.Fatalf("standby Attach error = %v; want a full-tunnel-on-this-host refusal", err)
	}
	h.m.Detach("A")
	if st := h.m.Status(); st.Active {
		t.Fatal("loopback session became active after the remote session detached")
	}
}

func TestRefusedReattachKeepsActiveSessionDetachable(t *testing.T) {
	h := newHarness(t, Options{})
	h.m.TrackPeer(netip.MustParseAddr("203.0.113.5"))
	if err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err != nil {
		t.Fatalf("Attach A: %v", err)
	}
	h.nc.take()
	h.m.TrackPeer(netip.MustParseAddr("127.0.0.1"))
	if err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err == nil {
		t.Fatal("re-Attach with a loopback server and a full tunnel was accepted")
	}
	h.m.Detach("A")
	if st := h.m.Status(); st.Active {
		t.Fatal("adapter still active after Detach of the only session")
	}
	removed := false
	for _, c := range h.nc.take() {
		if strings.HasPrefix(c, "route- ") {
			removed = true
		}
	}
	if !removed {
		t.Fatal("adapter routes were not removed after Detach")
	}
}

func TestDNSOverrideAndDeviceErrors(t *testing.T) {
	h := newHarness(t, Options{DNS: []netip.Addr{netip.MustParseAddr("1.1.1.1")}})
	if err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err != nil {
		t.Fatal(err)
	}
	calls := h.nc.take()
	if calls[len(calls)-1] != "dns [1.1.1.1] full=true" {
		t.Fatalf("DNS override not applied: %q", calls)
	}

	m := New(Options{
		Log:       testLog,
		NewDevice: func(string, int) (tun.Device, error) { return nil, errors.New("permission denied") },
	})
	defer m.Close()
	if err := m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err == nil ||
		!strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Attach with failing device = %v", err)
	}
}

func TestCloseDestroysAdapter(t *testing.T) {
	h := newHarness(t, Options{})
	if err := h.m.Attach("A", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err != nil {
		t.Fatal(err)
	}
	h.nc.take()
	h.m.Close()
	select {
	case <-h.dev.closed:
	default:
		t.Fatal("device not closed")
	}
	if err := h.m.Attach("B", params("10.77.0.2/24", "10.77.0.1", 0), newSink()); err == nil {
		t.Fatal("Attach after Close succeeded")
	}
	h.m.Detach("A") // must not panic
	h.m.Inbound([]byte{0x45})
}

func TestOptionsFromConfig(t *testing.T) {
	o, err := OptionsFromConfig(config.VPNClientConfig{
		Enabled: true, Routes: []string{"10.0.0.0/8", "192.0.2.1"}, Bypass: []string{"172.16.0.0/12"}, DNS: []string{"9.9.9.9"}, MTU: 1400,
	}, testLog)
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if len(o.Routes) != 2 || o.Routes[1].String() != "192.0.2.1/32" || len(o.Bypass) != 1 || o.MTU != 1400 {
		t.Fatalf("options = %+v", o)
	}
	for _, bad := range []config.VPNClientConfig{
		{Routes: []string{"nope"}},
		{Bypass: []string{"10.0.0.0/33"}},
		{DNS: []string{"x"}},
		{MTU: 100},
	} {
		if _, err := OptionsFromConfig(bad, testLog); err == nil {
			t.Errorf("config %+v accepted", bad)
		}
	}
}
