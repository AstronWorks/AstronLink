//go:build linux

package adapter

import "testing"

func TestParseRouteFields(t *testing.T) {
	cases := []struct{ line, via, dev string }{
		{"8.8.8.8 via 192.168.1.1 dev eth0 src 192.168.1.5 uid 0", "192.168.1.1", "eth0"},
		{"default via 10.0.0.1 dev wlp2s0 proto dhcp metric 600", "10.0.0.1", "wlp2s0"},
		{"192.168.1.20 dev eth0 src 192.168.1.5 uid 0", "", "eth0"},
		{"", "", ""},
	}
	for _, c := range cases {
		via, dev := parseRouteFields(c.line)
		if via != c.via || dev != c.dev {
			t.Errorf("parseRouteFields(%q) = %q, %q; want %q, %q", c.line, via, dev, c.via, c.dev)
		}
	}
}
