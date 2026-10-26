package tunnel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

const relayTestWait = 5 * time.Second

// tcpPair returns a connected loopback TCP pair on a random port.
func tcpPair(t *testing.T) (client, server *net.TCPConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type accepted struct {
		c   net.Conn
		err error
	}
	ch := make(chan accepted, 1)
	go func() {
		c, err := ln.Accept()
		ch <- accepted{c, err}
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	a := <-ch
	if a.err != nil {
		c.Close()
		t.Fatalf("accept: %v", a.err)
	}
	t.Cleanup(func() { c.Close(); a.c.Close() })
	return c.(*net.TCPConn), a.c.(*net.TCPConn)
}

// relayRig is cliA <-> [relA RelayBiDirectional relB] <-> srvB over loopback TCP.
type relayRig struct {
	cliA, relA, relB, srvB *net.TCPConn
	cancel                 context.CancelFunc
	ret                    chan struct{}
}

func newRelayRig(t *testing.T) *relayRig {
	t.Helper()
	r := &relayRig{ret: make(chan struct{})}
	r.cliA, r.relA = tcpPair(t)
	r.relB, r.srvB = tcpPair(t)
	var ctx context.Context
	ctx, r.cancel = context.WithCancel(context.Background())
	t.Cleanup(r.cancel)
	deadline := time.Now().Add(relayTestWait)
	r.cliA.SetDeadline(deadline)
	r.srvB.SetDeadline(deadline)
	go func() {
		RelayBiDirectional(ctx, r.relA, r.relB)
		close(r.ret)
	}()
	return r
}

func (r *relayRig) waitReturn() error {
	select {
	case <-r.ret:
		return nil
	case <-time.After(relayTestWait):
		return fmt.Errorf("RelayBiDirectional did not return within %v", relayTestWait)
	}
}

func (r *relayRig) closeAll() {
	r.cliA.Close()
	r.relA.Close()
	r.relB.Close()
	r.srvB.Close()
}

// halfCloseRoundTrip sends payload A->B, half-closes A, checks B sees payload then EOF,
// replies B->A with the reversed payload, half-closes B and waits for the relay to return.
func halfCloseRoundTrip(r *relayRig, payload []byte) error {
	writeErr := make(chan error, 1)
	go func() {
		if _, err := r.cliA.Write(payload); err != nil {
			writeErr <- fmt.Errorf("A write: %w", err)
			return
		}
		writeErr <- r.cliA.CloseWrite()
	}()

	got, err := io.ReadAll(r.srvB)
	if err != nil {
		return fmt.Errorf("B read to EOF: %w", err)
	}
	if err := <-writeErr; err != nil {
		return err
	}
	if !bytes.Equal(got, payload) {
		return fmt.Errorf("A->B payload mismatch: got %d bytes, want %d", len(got), len(payload))
	}

	reply := make([]byte, len(payload)+3)
	for i := range reply {
		reply[i] = byte(len(reply) - i)
	}
	go func() {
		r.srvB.Write(reply)
		r.srvB.CloseWrite()
	}()
	back, err := io.ReadAll(r.cliA)
	if err != nil {
		return fmt.Errorf("A read after half-close: %w", err)
	}
	if !bytes.Equal(back, reply) {
		return fmt.Errorf("B->A reply mismatch: got %d bytes, want %d", len(back), len(reply))
	}
	return r.waitReturn()
}

func TestRelayBiDirectional_HalfCloseKeepsReverseDirection(t *testing.T) {
	r := newRelayRig(t)
	if err := halfCloseRoundTrip(r, []byte("xyz")); err != nil {
		t.Fatal(err)
	}
}

func TestRelayBiDirectional_EmptyHalfCloseKeepsReverseDirection(t *testing.T) {
	r := newRelayRig(t)
	if err := halfCloseRoundTrip(r, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRelayBiDirectional_HalfClosePropagatesEOF(t *testing.T) {
	r := newRelayRig(t)
	if _, err := r.cliA.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := r.cliA.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r.srvB)
	if err != nil || string(got) != "abc" {
		t.Fatalf("B got %q err=%v, want \"abc\" then EOF", got, err)
	}
	// A is still readable: B closes fully and A sees EOF.
	r.srvB.Close()
	if _, err := io.ReadAll(r.cliA); err != nil {
		t.Fatalf("A read after B close: %v", err)
	}
	if err := r.waitReturn(); err != nil {
		t.Fatal(err)
	}
}

func TestRelayBiDirectional_LargePayloadOrderAfterHalfClose(t *testing.T) {
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	r := newRelayRig(t)
	r.cliA.SetDeadline(time.Now().Add(20 * time.Second))
	r.srvB.SetDeadline(time.Now().Add(20 * time.Second))
	if err := halfCloseRoundTrip(r, payload); err != nil {
		t.Fatal(err)
	}
}

func TestRelayBiDirectional_BothClosedReturnsAndReleasesGoroutines(t *testing.T) {
	base := runtime.NumGoroutine()
	r := newRelayRig(t)
	if err := halfCloseRoundTrip(r, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	r.closeAll()
	r.cancel()

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines not released: base=%d now=%d", base, runtime.NumGoroutine())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRelayBiDirectional_LongLivedBidirectionalNotClosedEarly(t *testing.T) {
	r := newRelayRig(t)
	buf := make([]byte, 4)
	for i := range 6 {
		msg := fmt.Appendf(nil, "a%03d", i)
		if _, err := r.cliA.Write(msg); err != nil {
			t.Fatalf("round %d A write: %v", i, err)
		}
		if _, err := io.ReadFull(r.srvB, buf); err != nil || !bytes.Equal(buf, msg) {
			t.Fatalf("round %d B got %q err=%v", i, buf, err)
		}
		reply := fmt.Appendf(nil, "b%03d", i)
		if _, err := r.srvB.Write(reply); err != nil {
			t.Fatalf("round %d B write: %v", i, err)
		}
		if _, err := io.ReadFull(r.cliA, buf); err != nil || !bytes.Equal(buf, reply) {
			t.Fatalf("round %d A got %q err=%v", i, buf, err)
		}
		select {
		case <-r.ret:
			t.Fatalf("relay returned early at round %d", i)
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func TestRelayBiDirectional_ResetTearsDownOtherDirection(t *testing.T) {
	r := newRelayRig(t)
	if _, err := r.cliA.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	r.cliA.SetLinger(0)
	r.cliA.Close()

	if err := r.waitReturn(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, r.srvB); err != nil {
		// A reset or timeout error is acceptable; reaching here means B was released.
		t.Logf("B read ended with: %v", err)
	}
}

func TestRelayBiDirectional_NonTCPConnEOFTearsDown(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	defer a1.Close()
	defer b2.Close()

	ret := make(chan struct{})
	go func() {
		RelayBiDirectional(context.Background(), a2, b1)
		close(ret)
	}()

	go func() {
		a1.Write([]byte("hi"))
		a1.Close()
	}()
	b2.SetReadDeadline(time.Now().Add(relayTestWait))
	got, err := io.ReadAll(b2)
	if err != nil || string(got) != "hi" {
		t.Fatalf("b2 got %q err=%v", got, err)
	}
	select {
	case <-ret:
	case <-time.After(relayTestWait):
		t.Fatal("RelayBiDirectional did not return after EOF on non-TCP conn")
	}
}

func TestRelayBiDirectional_ContextCancelUnblocks(t *testing.T) {
	r := newRelayRig(t)
	time.Sleep(50 * time.Millisecond)
	r.cancel()
	if err := r.waitReturn(); err != nil {
		t.Fatal(err)
	}
}

func TestRelayBiDirectional_ConcurrentHalfCloseRace(t *testing.T) {
	const n = 20
	rigs := make([]*relayRig, n)
	for i := range rigs {
		rigs[i] = newRelayRig(t)
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i, r := range rigs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := bytes.Repeat([]byte{byte(i)}, 1000+i)
			if err := halfCloseRoundTrip(r, payload); err != nil {
				errs <- fmt.Errorf("relay %d: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
