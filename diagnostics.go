package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type Diagnostic struct {
	Title  string `json:"title"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func runtimeDiagnostics(dir string) []Diagnostic {
	checks := []Diagnostic{{"앱 관리자 인증", true, "현재 앱 계정으로 인증했어요"}, {"앱 화면 처리", true, "패키지의 CGI 프로그램이 실행됐어요"}}
	st, e := os.Stat(dir)
	owned := false
	if e == nil {
		if stat, ok := st.Sys().(*syscall.Stat_t); ok {
			owned = stat.Uid == uint32(os.Geteuid())
		}
	}
	checks = append(checks, Diagnostic{"패키지 작업 공간", e == nil && owned && st.Mode().Perm()&0077 == 0, "패키지 전용 계정과 비공개 폴더 권한을 확인해요"})
	socket, e := os.Stat(filepath.Join(dir, "service.sock"))
	checks = append(checks, Diagnostic{"서비스 통신 소켓 생성", e == nil && socket.Mode()&os.ModeSocket != 0, "없거나 실패하면 패키지 센터에서 앱을 중지한 뒤 다시 실행해 주세요"})
	running := false
	if raw, e := os.ReadFile(filepath.Join(dir, "service.pid")); e == nil {
		if pid, e := strconv.Atoi(strings.TrimSpace(string(raw))); e == nil && pid > 1 {
			expected, _ := filepath.EvalSymlinks("/var/packages/" + packageName + "/target/bin/repo-sync")
			actual, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
			running = expected != "" && actual == expected && syscall.Kill(pid, 0) == nil
		}
	}
	checks = append(checks, Diagnostic{"동기화 서비스 프로세스", running, "프로세스 실행 여부예요. 원격 저장소 연결 성공을 의미하지는 않아요"})
	return checks
}
