package tunnel

// 바인드 직후의 소유 확인(confirmWildcardOwned)을 사전 검사와 분리해 직접 시험한다(임의 포트만 사용).

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// listenOrSkip listens on network/addr and skips the test when that stack is unavailable.
func listenOrSkip(t *testing.T, network, addr string) net.Listener {
	t.Helper()
	ln, err := net.Listen(network, addr)
	if err != nil {
		t.Skipf("%s %s unavailable: %v", network, addr, err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func portOf(ln net.Listener) string {
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func TestConfirmWildcardOwnedRejectsIPv4HeldElsewhere(t *testing.T) {
	own := listenOrSkip(t, "tcp6", "[::]:0")
	port := portOf(own)
	listenOrSkip(t, "tcp4", "0.0.0.0:"+port)

	err := confirmWildcardOwned(own, ":"+port)
	if err == nil {
		t.Fatalf("confirmWildcardOwned accepted :%s although another socket holds IPv4", port)
	}
	for _, want := range []string{port, "127.0.0.1", "already held"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestConfirmWildcardOwnedRejectsIPv6HeldElsewhere(t *testing.T) {
	own := listenOrSkip(t, "tcp4", "0.0.0.0:0")
	port := portOf(own)
	listenOrSkip(t, "tcp6", "[::]:"+port)

	err := confirmWildcardOwned(own, ":"+port)
	if err == nil {
		t.Fatalf("confirmWildcardOwned accepted :%s although another socket holds IPv6", port)
	}
	for _, want := range []string{port, "::1", "already held"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestConfirmWildcardOwnedAcceptsOwnDualStack(t *testing.T) {
	own := listenOrSkip(t, "tcp", ":0")
	if err := confirmWildcardOwned(own, ":0"); err != nil {
		t.Fatalf("own dual-stack listener rejected: %v", err)
	}

	// 확인이 건 마감이 풀려 있어야 이후 연결을 수락한다.
	time.Sleep(portProbeTimeout + 100*time.Millisecond)
	accepted := make(chan error, 1)
	go func() {
		c, err := own.Accept()
		if err == nil {
			c.Close()
		}
		accepted <- err
	}()
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", portOf(own)), time.Second)
	if err != nil {
		t.Fatalf("dial own listener: %v", err)
	}
	defer c.Close()
	select {
	case err := <-accepted:
		if err != nil {
			t.Fatalf("Accept after confirm: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return after confirm")
	}
}

func TestConfirmWildcardOwnedIgnoresRefusedStack(t *testing.T) {
	own := listenOrSkip(t, "tcp4", "0.0.0.0:0")
	if err := confirmWildcardOwned(own, ":"+portOf(own)); err != nil {
		t.Fatalf("refused IPv6 loopback should pass: %v", err)
	}
}
