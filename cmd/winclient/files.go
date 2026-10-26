//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/getlantern/systray"

	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/filetransfer"
	"github.com/AstronWorks/AstronLink/internal/logger"
)

// ft is the file-transfer receiver and the Explorer-menu control endpoint.
// They live independently of the server connection and restart when the
// settings change.
var ft struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	recvAddr string
	dropDir  string
}

// restartFileTransfer stops the running file-transfer listeners and starts
// them again from cfg when enabled.
func restartFileTransfer(cfg *config.ClientConfig) {
	stopFileTransfer()
	f := cfg.FileTransfer
	dir := f.DropDir
	if dir != "" && !filepath.IsAbs(dir) {
		dir = filepath.Join(exeDir, dir)
	}
	ft.mu.Lock()
	ft.dropDir = dir
	ft.mu.Unlock()
	if !f.Enabled {
		return
	}
	log := logger.NewWithLevel("winclient", cfg.LogLevel)
	ctx, cancel := context.WithCancel(context.Background())
	notify := func(msg string, ok bool) {
		systray.SetTooltip("Astron Link — " + msg)
		if !ok {
			logError(msg, nil)
		}
	}
	recvAddr := ""
	if recv, err := filetransfer.NewReceiver(dir, f.Token, f.MaxFileSize, log); err != nil {
		log.Error("file transfer: receiver init failed", "err", err)
	} else {
		recv.OnComplete(func(p string, _ int64) { notify("파일 도착: "+filepath.Base(p), true) })
		if addr, err := filetransfer.StartReceiver(ctx, f.ReceiveAddr, recv); err != nil {
			log.Error("file transfer: receiver start failed", "err", err)
		} else {
			recvAddr = addr
			log.Info("file transfer receiver listening", "addr", addr, "drop_dir", recv.Dir())
		}
	}
	if f.ControlAddr != "" && f.SendEndpoint != "" {
		if err := startControlServer(ctx, f.ControlAddr, f.SendEndpoint, f.Token, log, notify); err != nil {
			log.Error("file transfer: control server failed", "err", err)
		} else {
			log.Info("file transfer control server listening", "addr", f.ControlAddr)
		}
	}
	ft.mu.Lock()
	ft.cancel, ft.recvAddr = cancel, recvAddr
	ft.mu.Unlock()
}

func stopFileTransfer() {
	ft.mu.Lock()
	cancel := ft.cancel
	ft.cancel, ft.recvAddr = nil, ""
	ft.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func fileReceiverAddr() string {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return ft.recvAddr
}

// openDropFolder shows the received-files folder in Explorer.
func openDropFolder() error {
	ft.mu.Lock()
	dir := ft.dropDir
	ft.mu.Unlock()
	if dir == "" {
		return errors.New("수신 폴더가 설정되지 않았습니다. 설정 > 파일 전송에서 저장 폴더를 지정하십시오.")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return exec.Command("explorer", dir).Start()
}
