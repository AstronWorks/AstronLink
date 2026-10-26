//go:build windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"

	"github.com/AstronWorks/AstronLink/internal/gui/clientui"
)

const (
	instanceMutexName = `Local\AstronLinkClientWin`
	showEventName     = `Local\AstronLinkClientWinShow`
)

// Kept for the process lifetime so the named objects are not released early.
var singleInstanceHandle, showEvent windows.Handle

// acquireSingleInstance ensures only one client runs per user session; a
// second instance would double-bind the listen port. The second launch
// signals the first one to show its console and exits. Uses session-scoped
// (Local\) objects to match the per-user (HKCU) autostart. Fail-open on
// unexpected errors so a transient failure never blocks startup.
func acquireSingleInstance() {
	name, err := windows.UTF16PtrFromString(instanceMutexName)
	if err != nil {
		return
	}
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if ev, err := windows.UTF16PtrFromString(showEventName); err == nil {
			if e, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, ev); err == nil {
				_ = windows.SetEvent(e)
				_ = windows.CloseHandle(e)
				os.Exit(0)
			}
		}
		showInfo("Astron Link", "Astron Link 클라이언트가 이미 실행 중입니다. 트레이 아이콘을 확인하십시오.")
		os.Exit(0)
	}
	if err != nil {
		return // unexpected error: proceed (fail-open)
	}
	singleInstanceHandle = h
	if ev, err := windows.UTF16PtrFromString(showEventName); err == nil {
		showEvent, _ = windows.CreateEvent(nil, 0, 0, ev) // auto-reset
	}
}

// watchShowRequests opens the console whenever another launch signals.
func watchShowRequests() {
	defer recoverLog("show-requests")
	if showEvent == 0 {
		return
	}
	for {
		ev, err := windows.WaitForSingleObject(showEvent, windows.INFINITE)
		if err != nil || ev != windows.WAIT_OBJECT_0 {
			return
		}
		openConsole(clientui.PageOverview)
	}
}
