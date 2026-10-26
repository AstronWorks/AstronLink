package client

import "testing"

func TestResolveDataAddr(t *testing.T) {
	cases := []struct{ data, server, want string }{
		{"[::]:8444", "203.0.113.7:51234", "203.0.113.7:8444"},
		{"0.0.0.0:8444", "203.0.113.7:51234", "203.0.113.7:8444"},
		{":8444", "[2001:db8::1]:40000", "[2001:db8::1]:8444"},
		{"198.51.100.2:8444", "203.0.113.7:51234", "198.51.100.2:8444"},
		{"proxy.example.com:8444", "203.0.113.7:51234", "proxy.example.com:8444"},
		{"garbage", "203.0.113.7:51234", "garbage"},
	}
	for _, c := range cases {
		if got := resolveDataAddr(c.data, c.server); got != c.want {
			t.Errorf("resolveDataAddr(%q, %q) = %q; want %q", c.data, c.server, got, c.want)
		}
	}
}
