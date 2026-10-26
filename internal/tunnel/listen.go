package tunnel

import (
	"fmt"
	"net"
	"strconv"
	"time"
)

// portProbeTimeout bounds each loopback connect used to detect a held port.
const portProbeTimeout = 500 * time.Millisecond

// loopbackHosts are the loopback addresses of both IP stacks, probed to detect a held port.
var loopbackHosts = []string{"127.0.0.1", "::1"}

// ListenTCP listens on addr, refusing when another listener already holds the port.
// Windows lets a second socket half-bind a port held on another address family, which silently splits traffic.
// A wildcard bind is verified after binding so a listener that owns only one stack is closed instead of served.
func ListenTCP(addr string) (net.Listener, error) {
	if err := ensurePortFree(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if host, _, splitErr := net.SplitHostPort(addr); splitErr == nil && isWildcardHost(host) {
		if err := confirmWildcardOwned(ln, addr); err != nil {
			ln.Close()
			return nil, err
		}
	}
	return ln, nil
}

// ensurePortFree returns an error when a listener already accepts connections on addr's port.
// A wildcard host is probed on both loopback stacks; any other host is probed as given.
func ensurePortFree(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "0" {
		return nil
	}
	probes := []string{host}
	if isWildcardHost(host) {
		probes = loopbackHosts
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

// isWildcardHost reports whether host binds every interface (empty, 0.0.0.0 or ::).
func isWildcardHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "" || (ip != nil && ip.IsUnspecified())
}

// confirmWildcardOwned fails when a loopback connect to ln's port succeeds but never reaches ln,
// meaning another socket holds that address family. Connection refused means the stack is simply unused.
func confirmWildcardOwned(ln net.Listener, addr string) error {
	tl, ok := ln.(*net.TCPListener)
	if !ok {
		return nil
	}
	port := strconv.Itoa(tl.Addr().(*net.TCPAddr).Port)
	for _, h := range loopbackHosts {
		if !reachesListener(tl, net.JoinHostPort(h, port)) {
			return fmt.Errorf("port %s is already held by another listener (reachable at %s); refusing to bind %s", port, h, addr)
		}
	}
	return tl.SetDeadline(time.Time{})
}

// reachesListener dials target and reports whether tl accepts that very connection; a refused dial counts as reached.
func reachesListener(tl *net.TCPListener, target string) bool {
	conn, err := net.DialTimeout("tcp", target, portProbeTimeout)
	if err != nil {
		return true
	}
	defer conn.Close()
	local := conn.LocalAddr().(*net.TCPAddr)
	tl.SetDeadline(time.Now().Add(portProbeTimeout))
	for {
		accepted, err := tl.Accept()
		if err != nil {
			return false
		}
		remote, _ := accepted.RemoteAddr().(*net.TCPAddr)
		accepted.Close()
		if remote != nil && remote.Port == local.Port && remote.IP.Equal(local.IP) {
			return true
		}
	}
}
