package dependency

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ForkModulePrefix 는 비기본 버전 복사본이 들어가는 가상 모듈 경로의 접두사다.
const ForkModulePrefix = "github.com/conduix/forked/"

// forkHashLen 은 디렉토리 이름 끝에 붙는 식별 해시의 길이(16진 문자 수).
const forkHashLen = 10

// ForkDirName 은 fork 복사본이 놓이는 디렉토리 이름(pipeline-runner/forked/<이름>)이다.
//
// (modulePath, version) → 이름 매핑은 단사여야 한다. 충돌하면 서로 다른 모듈의 소스가
// 같은 디렉토리를 덮어써 조용히 섞인다. 사람이 읽을 수 있게 mangle 한 부분만으로는
// 단사가 아니다 — 경로의 '/' 와 원본 '_' 가 둘 다 '_' 로 접히기 때문에
// "resty/v2" 와 "resty_v2" 가 같은 이름이 된다. 그래서 원본 (path, version) 의 해시를
// 접미사로 붙여 유일성은 해시가, 가독성은 mangle 부분이 맡는다.
func ForkDirName(modulePath, version string) string {
	sum := sha256.Sum256([]byte(modulePath + "\x00" + version))
	return mangle(modulePath) + "_" + mangle(version) + "_" + hex.EncodeToString(sum[:])[:forkHashLen]
}

// ForkPath 는 fork 복사본이 갖게 될 가상 모듈 경로다.
func ForkPath(modulePath, version string) string {
	return ForkModulePrefix + ForkDirName(modulePath, version)
}

// mangle 은 모듈 경로/버전에서 Go 모듈 경로 요소로 쓰기 곤란한 문자를 '_' 로 바꾼다.
func mangle(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		default:
			return '_'
		}
	}, s)
}
