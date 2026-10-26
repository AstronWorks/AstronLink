//go:build windows

// Windows shell integration for the file-transfer feature: an HKCU Explorer
// right-click verb ("파일 전송"), a tiny send-file mode that the verb invokes,
// and a resident loopback control endpoint that performs the actual transfer
// over the tunnel. A single binary thus serves the tray, the context-menu
// helper, and menu registration.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/AstronWorks/AstronLink/internal/config"
	"github.com/AstronWorks/AstronLink/internal/filetransfer"
)

const (
	menuVerb  = "ReversproxySend"
	menuLabel = "Astron Link로 파일 전송"
)

// dispatchWinSubcommands runs a CLI-style subcommand of the GUI binary and exits
// if one matched. Used by the Explorer context-menu helper (send-file) and by
// scripted menu (un)registration.
func dispatchWinSubcommands() {
	if len(os.Args) < 2 {
		return
	}
	switch os.Args[1] {
	case "register-menu":
		os.Exit(registerContextMenu())
	case "unregister-menu":
		os.Exit(unregisterContextMenu())
	case "send-file":
		os.Exit(runSendFileHelper(os.Args[2:]))
	}
}

// contextMenuRoots returns the HKCU shell-verb key paths for files and folders.
func contextMenuRoots() []string {
	return []string{
		`Software\Classes\*\shell\` + menuVerb,
		`Software\Classes\Directory\shell\` + menuVerb,
	}
}

// setContextMenu adds (on) or removes the per-user (HKCU, no admin) Explorer
// right-click verb that invokes this binary's send-file mode.
func setContextMenu(on bool) error {
	if !on {
		for _, root := range contextMenuRoots() {
			_ = registry.DeleteKey(registry.CURRENT_USER, root+`\command`)
			_ = registry.DeleteKey(registry.CURRENT_USER, root)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("실행 파일 경로 확인 실패: %w", err)
	}
	command := fmt.Sprintf(`"%s" send-file "%%1"`, exe)
	for _, root := range contextMenuRoots() {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, root, registry.SET_VALUE)
		if err != nil {
			return err
		}
		_ = k.SetStringValue("", menuLabel)
		_ = k.SetStringValue("Icon", exe)
		k.Close()
		ck, _, err := registry.CreateKey(registry.CURRENT_USER, root+`\command`, registry.SET_VALUE)
		if err != nil {
			return err
		}
		_ = ck.SetStringValue("", command)
		ck.Close()
	}
	return nil
}

// contextMenuRegistered reports whether the right-click verb exists.
func contextMenuRegistered() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, contextMenuRoots()[0]+`\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	k.Close()
	return true
}

// refreshContextMenu rewrites an existing registration so its label and
// executable path follow this build (e.g. after the program moved).
func refreshContextMenu() {
	if contextMenuRegistered() {
		_ = setContextMenu(true)
	}
}

// registerContextMenu is the register-menu subcommand.
func registerContextMenu() int {
	if err := setContextMenu(true); err != nil {
		showError("Astron Link", "우클릭 메뉴 등록 실패:\n"+err.Error())
		return 1
	}
	showInfo("Astron Link", "탐색기 우클릭 메뉴를 등록했습니다.\n(Windows 11은 \"추가 옵션 표시\" 안에 표시됩니다)")
	return 0
}

// unregisterContextMenu is the unregister-menu subcommand.
func unregisterContextMenu() int {
	_ = setContextMenu(false)
	showInfo("Astron Link", "탐색기 우클릭 메뉴를 해제했습니다.")
	return 0
}

// runSendFileHelper forwards the selected paths to the resident tray agent's
// control endpoint, which performs the actual transfer over the tunnel. The
// verb invokes this once per selected item, so each call carries one path.
func runSendFileHelper(paths []string) int {
	if len(paths) == 0 {
		return 0
	}
	addr := controlAddrFromConfig()
	client := &http.Client{Timeout: 10 * time.Second}
	failed := 0
	for _, p := range paths {
		req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/send", strings.NewReader(p))
		if err != nil {
			failed++
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			failed++
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			failed++
		}
	}
	if failed > 0 {
		showError("Astron Link", "전송 요청 실패 — 트레이 앱이 실행 중인지, 관리 콘솔 설정 > 파일 전송을 확인하십시오.")
		return 1
	}
	return 0
}

// controlAddrFromConfig reads the tray control address from config.yaml next to
// the executable, falling back to the default.
func controlAddrFromConfig() string {
	addr := "127.0.0.1:8077"
	exe, err := os.Executable()
	if err != nil {
		return addr
	}
	cfg, err := config.LoadClientConfig(filepath.Join(filepath.Dir(exe), "config.yaml"))
	if err == nil && cfg.FileTransfer.ControlAddr != "" {
		addr = cfg.FileTransfer.ControlAddr
	}
	return addr
}

// startControlServer runs the resident tray's loopback endpoint that the
// context-menu helper posts file paths to. Each request triggers a tunnel send.
// It returns once the listener is bound; the server stops when ctx is done.
func startControlServer(ctx context.Context, addr, sendEndpoint, token string, log *slog.Logger, notify func(msg string, ok bool)) error {
	if sendEndpoint == "" {
		return errors.New("file_transfer.send_endpoint is empty")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 32*1024))
		path := strings.TrimSpace(string(body))
		if path == "" {
			http.Error(w, "missing path", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		go func() {
			// Use the server lifetime ctx so an in-flight send is cancelled when
			// the tray quits (rootCtx is cancelled), rather than running detached.
			if err := filetransfer.SendFile(ctx, sendEndpoint, path, filetransfer.SendOptions{Token: token}, log); err != nil {
				log.Error("file transfer: send failed", "path", path, "err", err)
				notify("전송 실패: "+filepath.Base(path), false)
				return
			}
			log.Info("file transfer: sent", "path", path)
			notify("전송 완료: "+filepath.Base(path), true)
		}()
	})
	srv := &http.Server{Handler: mux}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("file transfer: control server stopped", "err", err)
		}
	}()
	return nil
}
