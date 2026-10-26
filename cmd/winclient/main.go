//go:build windows

// Package main implements a Windows system-tray GUI wrapper for the Astron Link
// client. It provides the same tunnel / SOCKS5 / HTTP-proxy / VPN functionality
// as cmd/client, exposed through a tray icon and a management console window
// (Gio; internal/gui). Every setting is edited in the console's 설정 page —
// there is no file to open by hand.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/getlantern/systray"

	"github.com/EcoKG/reversproxy/internal/config"
	"github.com/EcoKG/reversproxy/internal/gui/clientui"
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
	exeDir     string
	configPath string
	logPath    string

	mStatusItem    *systray.MenuItem
	mToggleItem    *systray.MenuItem
	mAutostartItem *systray.MenuItem
)

func main() {
	// CLI-style subcommands (send-file / register-menu / unregister-menu) run
	// before anything else; each exits the process when matched.
	dispatchWinSubcommands()

	exeDir = "."
	if p, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(p)
	}
	// Relative paths in config.yaml (certificates, drop folder) resolve
	// against the program folder, also when started from the Run key.
	_ = os.Chdir(exeDir)
	configPath = filepath.Join(exeDir, "config.yaml")
	logPath = filepath.Join(exeDir, "winclient.log")

	acquireSingleInstance()
	migrateAutoStart()
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(trayStopped)
	systray.SetTitle("Astron Link")
	systray.SetTooltip("Astron Link 클라이언트")

	mStatus := systray.AddMenuItem("상태: 연결 안 됨", "현재 연결 상태")
	mStatus.Disable()
	systray.AddSeparator()
	mConsole := systray.AddMenuItem("관리 콘솔 열기", "상태 · 터널 · 로그 · 설정")
	mSettings := systray.AddMenuItem("설정", "관리 콘솔의 설정 탭")
	mLogs := systray.AddMenuItem("로그 보기", "관리 콘솔의 로그 탭")
	systray.AddSeparator()
	mToggle := systray.AddMenuItem("연결", "서버의 접속을 받기 시작 / 중지")
	mReconnect := systray.AddMenuItem("재연결", "연결을 끊고 다시 대기")
	mDrop := systray.AddMenuItem("수신함 열기", "받은 파일 폴더 열기")
	systray.AddSeparator()
	mAutostart := systray.AddMenuItemCheckbox("로그온 시 자동 실행", "Windows 로그온 시 자동 시작", isAutoStartEnabled())
	mAbout := systray.AddMenuItem("정보", "버전 정보")
	mQuit := systray.AddMenuItem("종료", "프로그램 종료")
	mStatusItem, mToggleItem, mAutostartItem = mStatus, mToggle, mAutostart

	// Rotate an oversized log, then send stdout/stderr (no console in GUI
	// mode) to the file: the connection loggers write JSON there and the
	// console's log page reads it back.
	rotateLogIfLarge(logPath)
	if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		os.Stdout = lf
		os.Stderr = lf
	}
	fmt.Fprintf(os.Stdout, "{\"msg\":\"winclient starting\",\"version\":%q,\"commit\":%q,\"build\":%q}\n",
		Version, Commit, BuildTime)

	go watchShowRequests()
	refreshContextMenu()

	firstRun := false
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := writeDefaultConfig(configPath); err != nil {
			logError("기본 설정 파일을 만들지 못했습니다", err)
		}
		firstRun = true
	}
	if cfg, err := config.LoadClientConfig(configPath); err == nil {
		restartFileTransfer(cfg)
	}

	go func() {
		defer recoverLog("startup")
		if firstRun {
			// The token must come from the server first.
			openConsole(clientui.PageSettings)
			return
		}
		if !connect() {
			openConsole(clientui.PageOverview)
		}
	}()

	for {
		select {
		case <-mConsole.ClickedCh:
			guard("console", func() { openConsole(clientui.PageOverview) })
		case <-mSettings.ClickedCh:
			guard("settings", func() { openConsole(clientui.PageSettings) })
		case <-mLogs.ClickedCh:
			guard("logs", func() { openConsole(clientui.PageLogs) })
		case <-mToggle.ClickedCh:
			guard("toggle", func() {
				if isActive() {
					disconnect()
				} else {
					go func() {
						defer recoverLog("connect")
						if !connect() {
							openConsole(clientui.PageOverview)
						}
					}()
				}
			})
		case <-mReconnect.ClickedCh:
			guard("reconnect", restartConnection)
		case <-mDrop.ClickedCh:
			guard("drop", func() {
				if err := openDropFolder(); err != nil {
					showError("Astron Link", err.Error())
				}
			})
		case <-mAbout.ClickedCh:
			guard("about", func() { openConsole(clientui.PageAbout) })
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
			stopFileTransfer()
			if done := disconnect(); done != nil {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
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

// setTray updates the tray icon, tooltip, status line and toggle label.
func setTray(phase clientui.Phase, text string) {
	icon := trayStopped
	switch phase {
	case clientui.Connected:
		icon = trayRunning
	case clientui.Failed:
		icon = trayError
	}
	systray.SetIcon(icon)
	if mStatusItem != nil {
		mStatusItem.SetTitle("상태: " + text)
	}
	if mToggleItem != nil {
		if phase == clientui.Waiting || phase == clientui.Connected {
			mToggleItem.SetTitle("연결 해제")
		} else {
			mToggleItem.SetTitle("연결")
		}
	}
	systray.SetTooltip("Astron Link 클라이언트 — " + text)
}
