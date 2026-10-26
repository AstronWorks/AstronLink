package tunnel

import (
	"context"
	"io"
	"net"
	"sync"
	"time"
)

var relayBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32*1024)
		return &buf
	},
}

// RelayBiDirectional copies data between a and b in both directions until both
// directions reach EOF, a copy error occurs, or ctx is cancelled; EOF on one side
// half-closes the peer's write side so the reverse direction keeps flowing.
// Both connections are closed before this function returns.
func RelayBiDirectional(ctx context.Context, a, b net.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// When context is cancelled, unblock any blocked Read/Write by setting
	// a deadline in the past.
	go func() {
		<-ctx.Done()
		past := time.Now().Add(-1 * time.Second)
		a.SetDeadline(past)
		b.SetDeadline(past)
	}()

	done := make(chan struct{}, 2)

	relay := func(dst, src net.Conn) {
		bufp := relayBufPool.Get().(*[]byte)
		defer relayBufPool.Put(bufp)
		_, err := io.CopyBuffer(dst, src, *bufp)
		tc, ok := dst.(*net.TCPConn)
		if ok {
			tc.CloseWrite()
		}
		// A copy error or a peer that cannot half-close ends both directions.
		if err != nil || !ok {
			cancel()
		}
		done <- struct{}{}
	}

	go relay(a, b)
	go relay(b, a)
	<-done
	<-done

	a.Close()
	b.Close()
	return nil
}
