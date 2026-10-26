//go:build windows

package main

import "golang.org/x/sys/windows/registry"

const prefsKeyPath = `Software\AstronLink\Server`

// prefDark reports whether the console uses the dark theme: the saved choice,
// else the Windows app theme.
func prefDark() bool {
	if k, err := registry.OpenKey(registry.CURRENT_USER, prefsKeyPath, registry.QUERY_VALUE); err == nil {
		v, _, err := k.GetStringValue("Theme")
		k.Close()
		if err == nil && (v == "dark" || v == "light") {
			return v == "dark"
		}
	}
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	light, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && light == 0
}

// setPrefDark saves the console theme.
func setPrefDark(dark bool) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, prefsKeyPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	v := "light"
	if dark {
		v = "dark"
	}
	_ = k.SetStringValue("Theme", v)
}
