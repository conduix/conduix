package dependency

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ModuleSource 는 (모듈, 버전)의 소스 디렉토리를 얻는 방법이다.
// 기본 구현은 `go mod download`, 테스트는 testdata 를 돌려주는 대역을 쓴다.
type ModuleSource interface {
	Dir(ctx context.Context, modulePath, version string) (string, error)
}

// CommandRunner 는 빌더의 runCommand 시그니처다(GOPROXY/GOCACHE env 를 갖춘 실행기).
type CommandRunner func(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error)

// GoModDownloadSource 는 `go mod download -json <path>@<version>` 의 Dir 를 쓴다.
// 모듈 캐시 경로라 읽기 전용이다 — 호출부가 복사한 뒤 쓰기 권한을 준다.
type GoModDownloadSource struct {
	Run CommandRunner
	// WorkDir 은 go 명령을 실행할 디렉토리(모듈 컨텍스트가 필요하다).
	WorkDir string
}

// Dir 은 모듈 소스가 풀린 캐시 디렉토리 경로를 돌려준다.
func (s *GoModDownloadSource) Dir(ctx context.Context, modulePath, version string) (string, error) {
	out, err := s.Run(ctx, s.WorkDir, nil, "go", "mod", "download", "-json", modulePath+"@"+version)
	if err != nil {
		return "", fmt.Errorf("go mod download %s@%s: %w (%s)", modulePath, version, err, out)
	}
	var info struct {
		Dir   string `json:"Dir"`
		Error string `json:"Error"`
	}
	// runCommand 는 stdout/stderr 를 한 버퍼에 합치므로 경고·진행 로그가 섞일 수 있다.
	// JSON 객체 부분만 잘라 읽는다.
	start := strings.IndexByte(out, '{')
	end := strings.LastIndexByte(out, '}')
	if start < 0 || end <= start {
		return "", fmt.Errorf("go mod download %s@%s: JSON 출력을 찾지 못했습니다 (%s)", modulePath, version, out)
	}
	if err := json.Unmarshal([]byte(out[start:end+1]), &info); err != nil {
		return "", fmt.Errorf("go mod download %s@%s 출력 해석 실패: %w (%s)", modulePath, version, err, out)
	}
	if info.Error != "" {
		return "", fmt.Errorf("go mod download %s@%s: %s", modulePath, version, info.Error)
	}
	if info.Dir == "" {
		return "", fmt.Errorf("go mod download %s@%s: Dir 가 비어 있습니다", modulePath, version)
	}
	return info.Dir, nil
}
