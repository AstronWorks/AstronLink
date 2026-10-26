package config_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	wantModule  = "module github.com/AstronWorks/AstronLink"
	legacyOwner = "Eco" + "KG"
)

// repoRoot is the module root relative to this package directory.
const repoRoot = "../.."

// 모든 패키지가 새 모듈 경로로 컴파일되어야 한다.
func TestModulePathBuildAll(t *testing.T) {
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./...: %v\n%s", err, out)
	}
}

// 추적 대상 텍스트 파일에 옛 소유자 표기가 대소문자 무관하게 남아 있으면 안 된다.
func TestNoLegacyModulePathResidue(t *testing.T) {
	skipDirs := map[string]bool{".git": true, ".astron-logs": true, ".serena": true, "dist": true, "node_modules": true}
	needle := []byte(strings.ToLower(legacyOwner))
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 5<<20 {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return nil
		}
		if bytes.Contains(bytes.ToLower(b), needle) {
			t.Errorf("옛 표기 잔존: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// go.mod 는 새 모듈 경로를 선언하고 replace 지시문이 없어야 한다.
func TestNoReplaceDirective(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if strings.TrimSpace(lines[0]) != wantModule {
		t.Fatalf("go.mod 1행 = %q, want %q", lines[0], wantModule)
	}
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "replace") {
			t.Errorf("go.mod:%d replace 지시문: %q", i+1, l)
		}
	}
}

// 제어 채널 인증서 CN 은 호환을 위해 reversproxy 로 유지한다.
func TestControlTLSCertCNUnchanged(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "internal", "control", "tls.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `CommonName: "reversproxy"`) {
		t.Fatal(`internal/control/tls.go 에 CommonName: "reversproxy" 가 없습니다`)
	}
}

// 빌드 스크립트·Makefile 이 선언한 산출물 이름은 바뀌지 않아야 한다.
func TestArtifactNamesUnchanged(t *testing.T) {
	names := []string{
		"astronlink-server", "astronlink-client",
		"astronlink-server-cli.exe", "astronlink-client.exe", "astronlink-server.exe",
	}
	for _, f := range []string{"Makefile", filepath.Join("scripts", "build.ps1")} {
		b, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			if !strings.Contains(string(b), n) {
				t.Errorf("%s 에 산출물 이름 %q 가 없습니다", f, n)
			}
		}
	}
}
