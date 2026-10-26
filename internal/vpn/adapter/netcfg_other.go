//go:build !linux && !windows

package adapter

import (
	"fmt"
	"log/slog"
	"runtime"

	"golang.zx2c4.com/wireguard/tun"
)

const (
	defaultInterfaceName = "rproxy0"
	privilegeHint        = ""
)

func newPlatformDevice(string, int) (tun.Device, error) {
	return nil, fmt.Errorf("vpn: VPN mode is not supported on %s", runtime.GOOS)
}

func newPlatformNetConfig(tun.Device, *slog.Logger) (NetConfig, error) {
	return nil, fmt.Errorf("vpn: VPN mode is not supported on %s", runtime.GOOS)
}
