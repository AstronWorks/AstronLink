//go:build windows

package adapter

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const (
	defaultInterfaceName = "AstronLink"
	privilegeHint        = "creating the Wintun adapter requires Administrator rights and wintun.dll (https://www.wintun.net) next to the executable"
)

func init() {
	tun.WintunTunnelType = "AstronLink"
}

// newPlatformDevice creates a Wintun adapter. The adapter GUID is derived from
// the name so Windows recognises it as the same network on every start
// instead of creating "Network 2, 3, ..." profiles.
func newPlatformDevice(name string, mtu int) (tun.Device, error) {
	sum := sha256.Sum256([]byte("reversproxy-wintun:" + name))
	guid := windows.GUID{
		Data1: uint32(sum[0])<<24 | uint32(sum[1])<<16 | uint32(sum[2])<<8 | uint32(sum[3]),
		Data2: uint16(sum[4])<<8 | uint16(sum[5]),
		Data3: uint16(sum[6])<<8 | uint16(sum[7]),
	}
	copy(guid.Data4[:], sum[8:16])
	return tun.CreateTUNWithRequestedGUID(name, &guid, mtu)
}

// windowsNetConfig configures the Wintun adapter through the IP Helper API.
type windowsNetConfig struct {
	luid winipcfg.LUID
	nt   *tun.NativeTun
	log  *slog.Logger
}

func newPlatformNetConfig(dev tun.Device, log *slog.Logger) (NetConfig, error) {
	nt, ok := dev.(*tun.NativeTun)
	if !ok {
		return nil, fmt.Errorf("vpn: unexpected TUN device type %T", dev)
	}
	return &windowsNetConfig{luid: winipcfg.LUID(nt.LUID()), nt: nt, log: log}, nil
}

func addrFamily(a netip.Addr) winipcfg.AddressFamily {
	if a.Is4() {
		return windows.AF_INET
	}
	return windows.AF_INET6
}

func (w *windowsNetConfig) SetAddress(addr netip.Prefix, mtu int) error {
	fam := addrFamily(addr.Addr())
	if err := w.luid.SetIPAddressesForFamily(fam, []netip.Prefix{addr}); err != nil {
		return fmt.Errorf("set address: %w", err)
	}
	iface, err := w.luid.IPInterface(fam)
	if err != nil {
		return fmt.Errorf("get interface: %w", err)
	}
	iface.NLMTU = uint32(mtu)
	// Lowest metric so the adapter's routes and DNS win over the physical NIC.
	iface.UseAutomaticMetric = false
	iface.Metric = 0
	if fam == windows.AF_INET {
		iface.SitePrefixLength = 0
	}
	if err := iface.Set(); err != nil {
		return fmt.Errorf("set interface: %w", err)
	}
	w.nt.ForceMTU(mtu)
	return nil
}

func onLink(dst netip.Prefix) netip.Addr {
	if dst.Addr().Is4() {
		return netip.IPv4Unspecified()
	}
	return netip.IPv6Unspecified()
}

func (w *windowsNetConfig) AddRoute(dst netip.Prefix) error {
	err := w.luid.AddRoute(dst, onLink(dst), 0)
	if errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
		return nil
	}
	return err
}

func (w *windowsNetConfig) DeleteRoute(dst netip.Prefix) error {
	return w.luid.DeleteRoute(dst, onLink(dst))
}

// AddBypass copies the best non-adapter route covering dst into a dedicated
// route for dst on the same interface and next hop.
func (w *windowsNetConfig) AddBypass(dst netip.Prefix) (func() error, error) {
	fam := addrFamily(dst.Addr())
	rows, err := winipcfg.GetIPForwardTable2(fam)
	if err != nil {
		return nil, fmt.Errorf("read routing table: %w", err)
	}
	ifMetric := make(map[winipcfg.LUID]uint32)
	interfaceMetric := func(luid winipcfg.LUID) (uint32, bool) {
		if m, ok := ifMetric[luid]; ok {
			return m, true
		}
		if row, err := luid.Interface(); err != nil || row.OperStatus != winipcfg.IfOperStatusUp {
			return 0, false
		}
		iface, err := luid.IPInterface(fam)
		if err != nil {
			return 0, false
		}
		ifMetric[luid] = iface.Metric
		return iface.Metric, true
	}

	var best *winipcfg.MibIPforwardRow2
	bestBits, bestMetric := -1, uint32(0)
	for i := range rows {
		r := &rows[i]
		if r.InterfaceLUID == w.luid || r.Loopback {
			continue
		}
		pfx := r.DestinationPrefix.Prefix()
		if !pfx.Contains(dst.Addr()) || pfx.Bits() > dst.Bits() {
			continue
		}
		im, up := interfaceMetric(r.InterfaceLUID)
		if !up {
			continue
		}
		metric := r.Metric + im
		if pfx.Bits() > bestBits || (pfx.Bits() == bestBits && metric < bestMetric) {
			best, bestBits, bestMetric = r, pfx.Bits(), metric
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no physical route to %s", dst)
	}
	luid, nextHop := best.InterfaceLUID, best.NextHop.Addr()
	if err := luid.AddRoute(dst, nextHop, 0); err != nil {
		if errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
			return func() error { return nil }, nil // pre-existing route: leave it alone
		}
		return nil, fmt.Errorf("add bypass route: %w", err)
	}
	return func() error { return luid.DeleteRoute(dst, nextHop) }, nil
}

func (w *windowsNetConfig) SetDNS(servers []netip.Addr, _ bool) error {
	var v4, v6 []netip.Addr
	for _, s := range servers {
		if s.Is4() {
			v4 = append(v4, s)
		} else {
			v6 = append(v6, s)
		}
	}
	if err := w.luid.SetDNS(windows.AF_INET, v4, nil); err != nil {
		return err
	}
	if len(v6) > 0 {
		return w.luid.SetDNS(windows.AF_INET6, v6, nil)
	}
	return nil
}

func (w *windowsNetConfig) ClearDNS() error {
	err4 := w.luid.FlushDNS(windows.AF_INET)
	err6 := w.luid.FlushDNS(windows.AF_INET6)
	return errors.Join(err4, err6)
}
