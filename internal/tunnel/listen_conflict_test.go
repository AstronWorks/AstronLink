package tunnel_test

// 같은 포트를 다른 프로세스·주소 계열이 이미 쥐고 있을 때 서버 리스너가
// 반쪽 바인드로 조용히 시작하지 않고 오류로 거부하는지 확인한다(임의 포트만 사용).

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/AstronWorks/AstronLink/internal/logger"
	"github.com/AstronWorks/AstronLink/internal/tunnel"
)

// listenerStarters maps each server listener to a function that starts it on addr.
func listenerStarters() map[string]func(ctx context.Context, addr string) error {
	mgr := tunnel.NewManager()
	ctrlConns := tunnel.NewControlConnRegistry()
	log := logger.New("test-listen-conflict")
	return map[string]func(ctx context.Context, addr string) error{
		"http": func(ctx context.Context, addr string) error {
			return tunnel.StartHTTPProxy(ctx, addr, mgr, ctrlConns, "127.0.0.1:0", log)
		},
		"https": func(ctx context.Context, addr string) error {
			return tunnel.StartHTTPSProxy(ctx, addr, mgr, ctrlConns, "127.0.0.1:0", log)
		},
		"data": func(ctx context.Context, addr string) error {
			_, err := tunnel.StartDataListener(ctx, addr, mgr, log)
			return err
		},
	}
}

// holdPort listens on network/addr and returns the chosen port; it skips the test when the stack is unavailable.
func holdPort(t *testing.T, network, addr string) (port string, release func()) {
	t.Helper()
	ln, err := net.Listen(network, addr)
	if err != nil {
		t.Skipf("%s %s unavailable: %v", network, addr, err)
	}
	_, port, err = net.SplitHostPort(ln.Addr().String())
	if err != nil {
		ln.Close()
		t.Fatalf("SplitHostPort(%q): %v", ln.Addr(), err)
	}
	return port, func() { ln.Close() }
}

func TestListenerRejectsWildcardWhenPortHeld(t *testing.T) {
	holders := []struct {
		name, network, addr string
	}{
		{"IPv4 any", "tcp4", "0.0.0.0:0"},
		{"IPv4 loopback", "tcp4", "127.0.0.1:0"},
		{"IPv6 loopback", "tcp6", "[::1]:0"},
		{"IPv6 any", "tcp6", "[::]:0"},
	}
	for name, start := range listenerStarters() {
		for _, h := range holders {
			t.Run(name+"/held on "+h.name, func(t *testing.T) {
				port, release := holdPort(t, h.network, h.addr)
				defer release()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				err := start(ctx, ":"+port)
				if err == nil {
					t.Fatalf("wildcard bind on :%s succeeded although %s already holds the port", port, h.name)
				}
				if !strings.Contains(err.Error(), port) {
					t.Errorf("error should name the port %s: %v", port, err)
				}
			})
		}
	}
}

func TestListenerRejectsSpecificAddrWhenPortHeld(t *testing.T) {
	for name, start := range listenerStarters() {
		t.Run(name, func(t *testing.T) {
			port, release := holdPort(t, "tcp4", "0.0.0.0:0")
			defer release()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := start(ctx, "127.0.0.1:"+port); err == nil {
				t.Fatalf("bind on 127.0.0.1:%s succeeded although 0.0.0.0 holds the port", port)
			}
		})
	}
}

func TestListenerStartsOnFreePort(t *testing.T) {
	for name, start := range listenerStarters() {
		t.Run(name+"/port 0", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := start(ctx, ":0"); err != nil {
				t.Fatalf("wildcard :0 should start: %v", err)
			}
		})
		t.Run(name+"/port freed after holder closes", func(t *testing.T) {
			port, release := holdPort(t, "tcp4", "0.0.0.0:0")
			release()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := start(ctx, ":"+port); err != nil {
				t.Fatalf("released port :%s should start: %v", port, err)
			}
		})
	}
}

// TestListenerReportsBoundAddrOnFreePort pins the pre-existing behaviour: free ports start and the real bound address is reported.
func TestListenerReportsBoundAddrOnFreePort(t *testing.T) {
	mgr := tunnel.NewManager()
	ctrlConns := tunnel.NewControlConnRegistry()
	log := logger.New("test-listen-bound")
	freePort := func(t *testing.T) string {
		port, release := holdPort(t, "tcp4", "127.0.0.1:0")
		release()
		return port
	}
	cases := []struct {
		name string
		addr func(t *testing.T) string
		want func(addr, got string) bool
	}{
		{"wildcard with port", func(t *testing.T) string { return ":" + freePort(t) }, func(addr, got string) bool { return strings.HasSuffix(got, addr) }},
		{"loopback with port", func(t *testing.T) string { return "127.0.0.1:" + freePort(t) }, func(addr, got string) bool { return got == addr }},
		{"loopback random port", func(t *testing.T) string { return "127.0.0.1:0" }, func(addr, got string) bool {
			return strings.HasPrefix(got, "127.0.0.1:") && !strings.HasSuffix(got, ":0")
		}},
		{"wildcard random port", func(t *testing.T) string { return ":0" }, func(addr, got string) bool { return !strings.HasSuffix(got, ":0") }},
	}
	for _, c := range cases {
		t.Run("http/"+c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			addr := c.addr(t)
			if err := tunnel.StartHTTPProxy(ctx, addr, mgr, ctrlConns, "127.0.0.1:0", log); err != nil {
				t.Fatalf("StartHTTPProxy(%q): %v", addr, err)
			}
			if !c.want(addr, tunnel.LastHTTPAddr) {
				t.Fatalf("LastHTTPAddr = %q for %q", tunnel.LastHTTPAddr, addr)
			}
		})
		t.Run("https/"+c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			addr := c.addr(t)
			if err := tunnel.StartHTTPSProxy(ctx, addr, mgr, ctrlConns, "127.0.0.1:0", log); err != nil {
				t.Fatalf("StartHTTPSProxy(%q): %v", addr, err)
			}
			if !c.want(addr, tunnel.LastHTTPSAddr) {
				t.Fatalf("LastHTTPSAddr = %q for %q", tunnel.LastHTTPSAddr, addr)
			}
		})
		t.Run("data/"+c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			addr := c.addr(t)
			bound, err := tunnel.StartDataListener(ctx, addr, mgr, log)
			if err != nil {
				t.Fatalf("StartDataListener(%q): %v", addr, err)
			}
			if !c.want(addr, bound) {
				t.Fatalf("bound = %q for %q", bound, addr)
			}
		})
	}
}
