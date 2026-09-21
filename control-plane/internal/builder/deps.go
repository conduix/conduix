package builder

import (
	"fmt"

	"github.com/conduix/conduix/control-plane/internal/dependency"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// resolvedDeps 는 이번 빌드에 참여하는 모든 stage 의 의존성 해소 결과다.
// 빌더는 이 구조체만 보고 go.mod 를 만든다 — 버전 결정 정책 자체는 dependency 패키지가 갖는다.
type resolvedDeps struct {
	Names    []string                   // sanitize 된 플러그인 이름(플러그인 목록 순서 보존)
	Pins     map[string]dependency.Pins // 이름 → 이 stage 가 쓸 모듈 버전
	Defaults map[string]string          // 모듈 → 레지스트리 기본 버전
	Forks    []dependency.Fork          // 기본과 다른 버전이라 복사·재작성이 필요한 모듈(W3)
	// Backfill 은 DepVersions 가 비어 있던 레거시 stage 의 새 고정값이다(plugin ID → JSON).
	// 빌드가 성공해야 저장한다 — 실패한 빌드의 버전을 stage 에 박아두지 않기 위함.
	Backfill map[string]string
}

// resolveDeps 는 각 stage 의 고정 버전을 확정한다.
// 레거시(DepVersions 비어 있음) stage 는 지금의 기본 버전으로 고정하되, 저장은 빌드 성공 후다.
func (rb *RunnerBuilder) resolveDeps(plugins []models.Plugin) (*resolvedDeps, error) {
	allowed, err := rb.activeAllowedModules()
	if err != nil {
		return nil, fmt.Errorf("query allowed modules: %w", err)
	}
	defaults := dependency.Defaults(allowed)

	out := &resolvedDeps{
		Names:    make([]string, 0, len(plugins)),
		Pins:     make(map[string]dependency.Pins, len(plugins)),
		Defaults: defaults,
		Backfill: map[string]string{},
	}
	idsByName := make(map[string]string, len(plugins))

	for _, p := range plugins {
		name := sanitizeName(p.Name)
		out.Names = append(out.Names, name)
		idsByName[name] = p.ID

		if pins := dependency.ParsePins(p.DepVersions); pins != nil {
			out.Pins[name] = pins
			continue
		}

		// 레거시 stage: 소스의 import 에서 기본 버전으로 고정값을 만든다.
		//
		// 해소가 실패해도(깨진 소스, retire 된 모듈을 아직 import 하는 stage) 빌드를
		// 세우지 않는다 — 이 stage 하나 때문에 다른 모든 stage 의 배포까지 막히기 때문이다.
		// 고정값 없이 두면 go mod tidy 가 메인 go.mod 의 require 로 해석하므로,
		// 해소 전과 같은 동작(그 stage 는 go build 단계에서 걸림)으로 떨어진다.
		imports, perr := dependency.ParseImports(p.SourceCode)
		if perr != nil {
			rb.logger.Warn("stage 소스 파싱 실패 — 고정 버전 없이 빌드 진행", "plugin", p.Name, "error", perr)
			continue
		}
		pins, rerr := dependency.ResolvePins(imports, nil, allowed)
		if rerr != nil {
			rb.logger.Warn("stage 의존성 해소 실패 — 고정 버전 없이 빌드 진행", "plugin", p.Name, "error", rerr)
			continue
		}
		out.Pins[name] = pins
		encoded, eerr := pins.Encode()
		if eerr != nil {
			rb.logger.Warn("고정 버전 직렬화 실패 — 백필 생략", "plugin", p.Name, "error", eerr)
			continue
		}
		if encoded != "" {
			out.Backfill[p.ID] = encoded
		}
	}

	out.Forks = dependency.CollectForks(idsByName, out.Names, out.Pins, defaults)
	return out, nil
}

// saveBackfilledPins 는 레거시 stage 의 고정 버전을 빌드 성공 후 저장한다.
func (rb *RunnerBuilder) saveBackfilledPins(resolved *resolvedDeps) {
	for id, encoded := range resolved.Backfill {
		if err := rb.db.Model(&models.Plugin{}).Where("id = ?", id).
			Update("dep_versions", encoded).Error; err != nil {
			rb.logger.Warn("dep_versions 백필 저장 실패 — 다음 빌드에서 재시도", "plugin_id", id, "error", err)
		}
	}
}
