//go:build linux

package adapter

import (
	"fmt"
	"log/slog"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/tun"
)

const (
	defaultInterfaceName = "rproxy0"
	privilegeHint        = "creating a TUN adapter requires root or CAP_NET_ADMIN"
)

func newPlatformDevice(name string, mtu int) (tun.Device, error) {
	return tun.CreateTUN(name, mtu)
}

// linuxNetConfig configures the adapter with iproute2 ("ip") and, for DNS,
// systemd-resolved ("resolvectl") when available.
type linuxNetConfig struct {
	name string
	log  *slog.Logger
}

func newPlatformNetConfig(dev tun.Device, log *slog.Logger) (NetConfig, error) {
	name, err := dev.Name()
	if err != nil {
		return nil, fmt.Errorf("vpn: adapter name: %w", err)
	}
	if _, err := exec.LookPath("ip"); err != nil {
		return nil, fmt.Errorf("vpn: the 'ip' command (iproute2) is required: %w", err)
	}
	return &linuxNetConfig{name: name, log: log}, nil
}

func (l *linuxNetConfig) SetAddress(addr netip.Prefix, mtu int) error {
	if _, err := runCmd("ip", "addr", "flush", "dev", l.name); err != nil {
		return err
	}
	if _, err := runCmd("ip", "addr", "add", addr.String(), "dev", l.name); err != nil {
		return err
	}
	_, err := runCmd("ip", "link", "set", "dev", l.name, "mtu", strconv.Itoa(mtu), "up")
	return err
}

func (l *linuxNetConfig) AddRoute(dst netip.Prefix) error {
	_, err := runCmd("ip", family(dst), "route", "replace", dst.String(), "dev", l.name)
	return err
}

func (l *linuxNetConfig) DeleteRoute(dst netip.Prefix) error {
	_, err := runCmd("ip", family(dst), "route", "del", dst.String(), "dev", l.name)
	return err
}

func (l *linuxNetConfig) AddBypass(dst netip.Prefix) (func() error, error) {
	via, dev, err := l.physicalPath(dst)
	if err != nil {
		return nil, err
	}
	spec := []string{dst.String()}
	if via != "" {
		spec = append(spec, "via", via)
	}
	spec = append(spec, "dev", dev)
	if _, err := runCmd("ip", append([]string{family(dst), "route", "replace"}, spec...)...); err != nil {
		return nil, err
	}
	return func() error {
		_, err := runCmd("ip", append([]string{family(dst), "route", "del"}, spec...)...)
		return err
	}, nil
}

// physicalPath returns the next hop and device the kernel uses for dst,
// ignoring the adapter: when the adapter already captures dst, the main
// table's default route is used instead.
func (l *linuxNetConfig) physicalPath(dst netip.Prefix) (via, dev string, err error) {
	out, err := runCmd("ip", family(dst), "route", "get", dst.Addr().String())
	if err == nil {
		via, dev = parseRouteFields(firstLine(out))
		if dev != "" && dev != l.name {
			return via, dev, nil
		}
	}
	out, err = runCmd("ip", family(dst), "route", "show", "default")
	if err != nil {
		return "", "", err
	}
	for _, line := range strings.Split(out, "\n") {
		via, dev = parseRouteFields(line)
		if dev != "" && dev != l.name {
			return via, dev, nil
		}
	}
	return "", "", fmt.Errorf("vpn: no physical route to %s", dst)
}

func (l *linuxNetConfig) SetDNS(servers []netip.Addr, fullTunnel bool) error {
	if _, err := exec.LookPath("resolvectl"); err != nil {
		l.log.Warn("vpn: resolvectl (systemd-resolved) not found — adapter DNS not configured; set the resolver manually",
			"dns", servers)
		return nil
	}
	args := []string{"dns", l.name}
	for _, s := range servers {
		args = append(args, s.String())
	}
	if _, err := runCmd("resolvectl", args...); err != nil {
		return err
	}
	if fullTunnel {
		// "~." makes this link the preferred resolver for every domain.
		if _, err := runCmd("resolvectl", "domain", l.name, "~."); err != nil {
			return err
		}
	}
	return nil
}

func (l *linuxNetConfig) ClearDNS() error {
	if _, err := exec.LookPath("resolvectl"); err != nil {
		return nil
	}
	_, err := runCmd("resolvectl", "revert", l.name)
	return err
}

func runCmd(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func family(p netip.Prefix) string {
	if p.Addr().Is4() {
		return "-4"
	}
	return "-6"
}

// parseRouteFields extracts the "via" and "dev" values from one line of
// `ip route` output, e.g. "default via 192.168.1.1 dev eth0 proto dhcp".
func parseRouteFields(line string) (via, dev string) {
	f := strings.Fields(line)
	for i := 0; i+1 < len(f); i++ {
		switch f[i] {
		case "via":
			via = f[i+1]
		case "dev":
			dev = f[i+1]
		}
	}
	return via, dev
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
