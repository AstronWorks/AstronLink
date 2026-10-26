//go:build windows

package main

// Build metadata, stamped via -ldflags "-X main.Version=... -X main.Commit=... -X main.BuildTime=...".
var (
	Version   = "dev"
	Commit    = ""
	BuildTime = ""
)

func valOr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
