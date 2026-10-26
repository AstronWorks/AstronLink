//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// Per-user logon autostart via the HKCU Run key (no administrator rights).
const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "AstronLinkClient"
	// legacyRunValueName is the entry written before the Astron Link rename.
	legacyRunValueName = "Reversproxy"
)

// isAutoStartEnabled reports whether the client is registered to launch at logon
// (under the current or the pre-rename name).
func isAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	for _, name := range []string{runValueName, legacyRunValueName} {
		if v, _, err := k.GetStringValue(name); err == nil && v != "" {
			return true
		}
	}
	return false
}

// setAutoStart enables or disables launching the client at logon for this user.
// It always removes the pre-rename entry, whose path may point at the old
// executable name.
func setAutoStart(enable bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("open Run key: %w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(legacyRunValueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("remove old autostart entry: %w", err)
	}
	if !enable {
		if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("remove autostart entry: %w", err)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	// Quote the path so a path containing spaces is launched correctly.
	if err := k.SetStringValue(runValueName, `"`+exe+`"`); err != nil {
		return fmt.Errorf("write autostart entry: %w", err)
	}
	return nil
}

// migrateAutoStart re-registers a pre-rename autostart entry under the new
// name and this executable's path.
func migrateAutoStart() {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	_, _, legacyErr := k.GetStringValue(legacyRunValueName)
	k.Close()
	if legacyErr == nil {
		_ = setAutoStart(true)
	}
}
