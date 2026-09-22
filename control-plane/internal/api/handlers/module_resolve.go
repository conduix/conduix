package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// goProxyResolver 는 GOPROXY 로 모듈 버전과 "import 경로가 속한 모듈 경로" 를 알아낸다.
// 사용자는 소스에 import 경로(서브패키지 포함)를 쓰지만 레지스트리는 모듈 경로를 받는다.
// 그 간극을 사용자가 머리로 메우지 않게 하려면 서버가 접두사를 짚어 줘야 한다.
type goProxyResolver struct {
	base   string
	client *http.Client
}

func newGoProxyResolver(base string, client *http.Client) *goProxyResolver {
	if base == "" {
		base = "https://proxy.golang.org"
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &goProxyResolver{base: base, client: client}
}

// maxResolveProbes 는 import 경로 하나를 해소할 때 GOPROXY 에 보낼 최대 요청 수다.
// github 류는 3세그먼트라 첫 시도에 맞고, 깊은 서브패키지도 뒤에서부터 4번이면 모듈 루트에 닿는다.
// 프록시 장애 시 저장 흐름이 한없이 느려지지 않게 하는 상한이다.
const maxResolveProbes = 4

// latest 는 {module}/@latest 의 Version 을 돌려준다. 404 등 비정상 응답은 에러.
// module path 는 GOPROXY 규약상 대문자를 !소문자로 인코딩해야 하나, 흔한 모듈은 소문자라
// 우선 그대로 질의하고 실패 시 에러를 그대로 노출한다(대문자 인코딩은 후속).
func (r *goProxyResolver) latest(ctx context.Context, modulePath string) (string, error) {
	url := fmt.Sprintf("%s/%s/@latest", strings.TrimRight(r.base, "/"), modulePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("goproxy %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var info struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("parse goproxy response: %w", err)
	}
	if info.Version == "" {
		return "", fmt.Errorf("empty version from goproxy")
	}
	return info.Version, nil
}

// resolveImport 는 import 경로에서 모듈 경로와 그 최신 버전을 찾는다.
// 긴 접두사부터 @latest 를 질의해 처음 성공하는 것이 모듈 루트다 — 서브패키지 경로는
// GOPROXY 에 모듈로 존재하지 않아 404 가 난다. 모두 실패하면 휴리스틱 경로와 에러를 함께 돌려준다.
func (r *goProxyResolver) resolveImport(ctx context.Context, importPath string) (modulePath, version string, err error) {
	var lastErr error
	probes := 0
	for _, candidate := range modulePathCandidates(importPath) {
		if probes >= maxResolveProbes {
			break
		}
		probes++
		v, perr := r.latest(ctx, candidate)
		if perr == nil {
			return candidate, v, nil
		}
		lastErr = perr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no module path candidates for %q", importPath)
	}
	return heuristicModulePath(importPath), "", lastErr
}

// modulePathCandidates 는 import 경로의 접두사를 긴 것부터 나열한다. 호스트 하나만 남는
// 1세그먼트는 모듈이 될 수 없어 제외한다.
func modulePathCandidates(importPath string) []string {
	segs := strings.Split(strings.Trim(importPath, "/"), "/")
	var out []string
	for n := len(segs); n >= 2; n-- {
		out = append(out, strings.Join(segs[:n], "/"))
	}
	return out
}

// heuristicModulePath 는 GOPROXY 없이 쓰는 폴백이다. 코드 호스팅 서비스는 "호스트/소유자/저장소"
// 3세그먼트가 모듈 루트인 경우가 대부분이고, 그 외(gopkg.in/yaml.v3, golang.org/x/...)는
// 2~3세그먼트가 섞여 있어 원 경로를 그대로 제안한다 — 틀린 추측보다 사용자가 고칠 수 있는 원문이 낫다.
func heuristicModulePath(importPath string) string {
	segs := strings.Split(strings.Trim(importPath, "/"), "/")
	switch segs[0] {
	case "github.com", "gitlab.com", "bitbucket.org", "codeberg.org":
		if len(segs) >= 3 {
			return strings.Join(segs[:3], "/")
		}
	case "golang.org":
		// golang.org/x/<name>
		if len(segs) >= 3 && segs[1] == "x" {
			return strings.Join(segs[:3], "/")
		}
	}
	return importPath
}

// suggestModulePaths 는 미등록 import 목록을 {import 경로: 제안 모듈 경로} 로 만든다.
// resolver 가 nil 이거나 프록시가 실패하면 휴리스틱으로 채운다 — 제안이 비어서 UI 가
// 버튼을 못 만드는 것보다 사용자가 고칠 수 있는 추측이 낫다.
func suggestModulePaths(ctx context.Context, resolver *goProxyResolver, imports []string) map[string]string {
	out := make(map[string]string, len(imports))
	for _, imp := range imports {
		if resolver == nil {
			out[imp] = heuristicModulePath(imp)
			continue
		}
		mod, _, err := resolver.resolveImport(ctx, imp)
		if err != nil || mod == "" {
			mod = heuristicModulePath(imp)
		}
		out[imp] = mod
	}
	return out
}
