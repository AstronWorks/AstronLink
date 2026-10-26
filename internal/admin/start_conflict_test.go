package admin_test

// admin 수신 주소가 이미 다른 소켓이 쥔 포트로 반쪽 바인드하지 않고 오류로 거부하는지 확인한다(임의 포트만 사용).

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/AstronWorks/AstronLink/internal/admin"
	"github.com/AstronWorks/AstronLink/internal/control"
	"github.com/AstronWorks/AstronLink/internal/logger"
	"github.com/AstronWorks/AstronLink/internal/stats"
	"github.com/AstronWorks/AstronLink/internal/tunnel"
)

func newAdminServer() *admin.Server {
	return admin.New(control.NewClientRegistry(), tunnel.NewManager(), stats.NewRegistry(), &stats.ServerStats{}, logger.New("test-admin-start"), "")
}

// holdAnyPort listens on network/addr and returns the chosen port; it skips the test when the stack is unavailable.
func holdAnyPort(t *testing.T, network, addr string) string {
	t.Helper()
	ln, err := net.Listen(network, addr)
	if err != nil {
		t.Skipf("%s %s unavailable: %v", network, addr, err)
	}
	t.Cleanup(func() { ln.Close() })
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", ln.Addr(), err)
	}
	return port
}

func TestAdminStartRejectsWhenIPv6LoopbackHeld(t *testing.T) {
	port := holdAnyPort(t, "tcp6", "[::1]:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := newAdminServer().Start(ctx, "0.0.0.0:"+port)
	if err == nil {
		t.Fatalf("admin started on 0.0.0.0:%s although [::1]:%s is held", port, port)
	}
	for _, want := range []string{port, "already held"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestAdminStartRejectsWhenIPv4LoopbackHeld(t *testing.T) {
	port := holdAnyPort(t, "tcp4", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := newAdminServer().Start(ctx, "[::]:"+port)
	if err == nil {
		t.Fatalf("admin started on [::]:%s although 127.0.0.1:%s is held", port, port)
	}
	for _, want := range []string{port, "already held"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestAdminStartSucceedsOnFreePort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := newAdminServer().Start(ctx, ":0"); err != nil {
		t.Fatalf("admin :0 should start: %v", err)
	}
}
