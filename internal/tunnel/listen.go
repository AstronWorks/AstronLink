package tunnel

import (
	"fmt"
	"net"
	"time"
)

// portProbeTimeout bounds each loopback connect used to detect a held port.
const portProbeTimeout = 500 * time.Millisecond

// listenTCP listens on addr, refusing when another listener already holds the port.
// Windows lets a second socket half-bind a port held on another address family, which silently splits traffic.
func listenTCP(addr string) (net.Listener, error) {
	if err := ensurePortFree(addr); err != nil {
		return nil, err
	}
	return net.Listen("tcp", addr)
}

// ensurePortFree returns an error when a listener already accepts connections on addr's port.
// A wildcard host is probed on both loopback stacks; any other host is probed as given.
func ensurePortFree(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "0" {
		return nil
	}
	probes := []string{host}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		probes = []string{"127.0.0.1", "::1"}
	}
	for _, h := range probes {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(h, port), portProbeTimeout)
		if err == nil {
			conn.Close()
			return fmt.Errorf("port %s is already held by another listener (reachable at %s); refusing to bind %s", port, h, addr)
		}
	}
	return nil
}
