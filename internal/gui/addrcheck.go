package gui

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ListenAddrError checks a listener address ("host:port", host optional) and
// returns a message for the form, or "".
func ListenAddrError(v string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(v))
	if err != nil {
		return "host:port 형식이어야 합니다 (예: :8444, 0.0.0.0:8444)"
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return "포트는 1–65535 사이여야 합니다"
	}
	if host != "" && net.ParseIP(strings.Trim(host, "[]")) == nil && host != "localhost" {
		return "IP 주소만 쓸 수 있습니다 (비우면 모든 주소)"
	}
	return ""
}

// DialAddrError checks an address to connect to (host required).
func DialAddrError(v string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(v))
	if err != nil || host == "" {
		return "IP:포트 또는 호스트:포트 형식이어야 합니다 (예: 192.168.1.10:8443)"
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return "포트는 1–65535 사이여야 합니다"
	}
	return ""
}

// PortError checks a port number; zero is accepted when allowZero.
func PortError(p int, allowZero bool) string {
	if allowZero && p == 0 {
		return ""
	}
	if p < 1 || p > 65535 {
		if allowZero {
			return "0–65535 사이여야 합니다 (0 은 자동)"
		}
		return "1–65535 사이여야 합니다"
	}
	return ""
}

// HostPort splits addr into host and numeric port (0 when invalid).
func HostPort(addr string) (string, int) {
	h, p, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", 0
	}
	n, _ := strconv.Atoi(p)
	return h, n
}

// PortClash reports a message when two listeners would bind the same port.
// a and b are host:port; an empty host means all addresses.
func PortClash(a, b string) string {
	ha, pa := HostPort(a)
	hb, pb := HostPort(b)
	if pa == 0 || pa != pb {
		return ""
	}
	if ha == hb || ha == "" || hb == "" || ha == "0.0.0.0" || hb == "0.0.0.0" {
		return fmt.Sprintf("다른 리스너와 포트 %d 이(가) 겹칩니다", pa)
	}
	return ""
}

// SplitList splits a comma/space/semicolon/newline separated list.
func SplitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '\n' || r == '\t' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
