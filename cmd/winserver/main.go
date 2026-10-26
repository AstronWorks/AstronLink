//go:build windows

// Package main implements a Windows system-tray GUI wrapper for the reversproxy
// SERVER. It runs the full server (data listener, HTTP/HTTPS proxies, admin API,
// client dial loops) in the user session and exposes status and control
// through a tray icon and a management console window (Gio; internal/gui).
// Every setting is edited in the console's 설정 page — there is no file to
// open by hand.
//
// A Windows service runs in session 0 and cannot show a tray icon, so this app
// is meant to run interactively (launch at logon via the 자동 실행 toggle)
// instead of as the NSSM service.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/getlantern/systray"

	"github.com/EcoKG/reversproxy/internal/gui/serverui"
)

var (
	//go:embed assets/tray-running.ico
	trayRunning []byte
	//go:embed assets/tray-stopped.ico
	trayStopped []byte
	//go:embed assets/tray-error.ico
	trayError []byte
)

var (
	configPath string
	logPath    string

	mStatusItem    *systray.MenuItem
	mAutostartItem *systray.MenuItem
)

func main() {
	exeDir := "."
	if p, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(p)
	}
	// Relative paths in server.yaml (certificates, drop folder) resolve
	// against the program folder, also when started from the Run key, whose
	// working directory is System32.
	_ = os.Chdir(exeDir)
	configPath = filepath.Join(exeDir, "server.yaml")
	logPath = filepath.Join(exeDir, "server.log")

	// Only one instance per user session (prevents double port binding); a
	// second launch asks the running one to show its console.
	acquireSingleInstance()
	migrateAutoStart()
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(trayStopped)
	systray.SetTitle("Astron Link Server")
	systray.SetTooltip("Astron Link 서버")

	mStatus := systray.AddMenuItem("상태: 시작 중...", "서버 상태")
	mStatus.Disable()
	systray.AddSeparator()
	mConsole := systray.AddMenuItem("관리 콘솔 열기", "상태 · 클라이언트 · 터널 · 로그 · 설정")
	mSettings := systray.AddMenuItem("설정", "관리 콘솔의 설정 탭")
	mLogs := systray.AddMenuItem("로그 보기", "관리 콘솔의 로그 탭")
	mRestart := systray.AddMenuItem("서버 재시작", "리스너와 클라이언트 접속을 다시 시작")
	systray.AddSeparator()
	mAutostart := systray.AddMenuItemCheckbox("로그온 시 자동 실행", "Windows 로그온 시 자동 시작", isAutoStartEnabled())
	mAbout := systray.AddMenuItem("정보", "버전 정보")
	mQuit := systray.AddMenuItem("종료", "서버 종료")
	mStatusItem, mAutostartItem = mStatus, mAutostart

	// Rotate an oversized log before opening, then route stdout/stderr (no
	// console in GUI mode) to the file so the log page can show it.
	rotateLogIfLarge(logPath)
	if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		os.Stdout = lf
		os.Stderr = lf
	}
	fmt.Fprintf(os.Stdout, "{\"msg\":\"winserver starting\",\"version\":%q,\"commit\":%q,\"build\":%q}\n",
		Version, Commit, BuildTime)

	go watchShowRequests()

	// First run: write a working default config (random token) and open the
	// settings page so the user can add the clients to dial.
	firstRun := false
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := writeDefaultConfig(configPath); err != nil {
			logError("기본 설정 파일을 만들지 못했습니다", err)
		}
		firstRun = true
	}
	go func() {
		defer recoverLog("startup")
		if ok := startServer(); !ok || firstRun {
			page := serverui.PageOverview
			if firstRun {
				page = serverui.PageSettings
			}
			openConsole(page)
		}
	}()

	for {
		select {
		case <-mConsole.ClickedCh:
			guard("console", func() { openConsole(serverui.PageOverview) })
		case <-mSettings.ClickedCh:
			guard("settings", func() { openConsole(serverui.PageSettings) })
		case <-mLogs.ClickedCh:
			guard("logs", func() { openConsole(serverui.PageLogs) })
		case <-mRestart.ClickedCh:
			guard("restart", restartServer)
		case <-mAbout.ClickedCh:
			guard("about", func() { openConsole(serverui.PageAbout) })
		case <-mAutostart.ClickedCh:
			guard("autostart", func() {
				enable := !mAutostart.Checked()
				if err := setAutoStart(enable); err != nil {
					showError("Astron Link", "자동 실행 설정 실패:\n"+err.Error())
					return
				}
				syncAutostartItem(enable)
			})
		case <-mQuit.ClickedCh:
			closeConsole()
			if done := stopServer(); done != nil {
				select {
				case <-done:
				case <-time.After(6 * time.Second):
				}
			}
			systray.Quit()
			return
		}
	}
}

func onExit() {}

// guard runs a tray-menu action with panic recovery so a panic cannot kill the
// menu loop.
func guard(where string, fn func()) {
	defer recoverLog("menu." + where)
	fn()
}

// syncAutostartItem mirrors the autostart state in the tray checkbox (the
// console can change it too).
func syncAutostartItem(on bool) {
	if mAutostartItem == nil {
		return
	}
	if on {
		mAutostartItem.Check()
	} else {
		mAutostartItem.Uncheck()
	}
}

// setTray updates the tray icon, tooltip and status line.
func setTray(phase serverui.Phase, text string) {
	icon := trayStopped
	switch phase {
	case serverui.Running, serverui.Starting:
		icon = trayRunning
	case serverui.Failed:
		icon = trayError
	}
	systray.SetIcon(icon)
	if mStatusItem != nil {
		mStatusItem.SetTitle("상태: " + text)
	}
	systray.SetTooltip("Astron Link 서버 — " + text)
}
