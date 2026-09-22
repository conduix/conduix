package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/shared/types"

	"github.com/conduix/conduix/control-plane/internal/dependency"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// resolveStagePins 는 native stage 소스의 import 를 검증하고 이 stage 가 쓸 모듈 버전을 확정한다.
// 검증(D5)과 버전 확정은 같은 규칙에서 나오므로 한 번에 처리한다 — 정책 본체는
// internal/dependency 에 있고 여기는 DB 조회만 얹는다(ADR-0005).
func resolveStagePins(db *database.DB, sourceCode string, existing dependency.Pins) (dependency.Pins, error) {
	imports, err := dependency.ParseImports(sourceCode)
	if err != nil {
		return nil, fmt.Errorf("소스 파싱 실패: %w", err)
	}
	allowed, err := activeAllowedModules(db)
	if err != nil {
		return nil, err
	}
	return dependency.ResolvePins(imports, existing, allowed)
}

// activeAllowedModules 는 status=active 인 허용 모듈을 조회한다(기본 버전의 원천).
func activeAllowedModules(db *database.DB) ([]models.AllowedModule, error) {
	var allowed []models.AllowedModule
	if err := db.Where("status = ?", "active").Order("module_path asc").Find(&allowed).Error; err != nil {
		return nil, fmt.Errorf("허용 모듈 조회 실패: %w", err)
	}
	return allowed, nil
}

// testRunnerMain 은 인-에디터 테스트 빌드에 주입하는 실행 러너(package main).
// 실제 RunnerBuilder 와 동일 계약을 쓴다: 사용자 소스를 별도 subpackage(pluginstage)로 두고
// import 해 `pluginstage.Stage{}`(구조체) 를 생성한다. 실제 빌드는 registry_custom.go 가
// `plugin_<name>.Stage{}` 를 쓰므로(runner_builder.GenerateRegistryCustom), 테스트도 struct Stage
// 를 요구해야 "에디터 테스트 통과=실제 빌드 통과" 가 성립한다.
// (구 방식은 사용자 소스를 package main 으로 같은 디렉토리에 둬 package clash + var Stage 요구로
// 실제 빌드 계약과 어긋났다 — BUG#6.)
const testRunnerMain = `package main

import (
	"encoding/json"
	"os"

	sdk "github.com/conduix/conduix/plugin-sdk"
	pluginstage "conduix-plugin-test/pluginstage"
)

func main() {
	var stage sdk.NativeStage = &pluginstage.Stage{}
	var in struct {
		Config     map[string]any   ` + "`json:\"config\"`" + `
		SampleData []map[string]any ` + "`json:\"sample_data\"`" + `
	}
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"error": "decode input: " + err.Error()})
		return
	}
	if err := stage.Init(in.Config); err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"error": "init: " + err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(in.SampleData))
	for _, rec := range in.SampleData {
		r, err := stage.Process(rec)
		if err != nil {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"error": "process: " + err.Error()})
			return
		}
		if r != nil {
			out = append(out, r)
		}
	}
	_ = stage.Close()
	json.NewEncoder(os.Stdout).Encode(map[string]any{"records": out})
}
`

// resolveAndEncodePins 는 resolveStagePins 결과를 Plugin.DepVersions 에 저장할 JSON 으로 만든다.
// existingDepVersions 가 비어 있으면(레거시 stage) 전부 기본 버전으로 새로 고정된다.
func resolveAndEncodePins(db *database.DB, sourceCode, existingDepVersions string) (string, error) {
	pins, err := resolveStagePins(db, sourceCode, dependency.ParsePins(existingDepVersions))
	if err != nil {
		return "", err
	}
	return pins.Encode()
}

// pinsByPluginName 은 이름으로 저장된 stage 의 고정 버전을 읽는다. 없으면 nil(= 기본 버전).
// 에디터 테스트 빌드와 LSP workspace 가 같은 값을 봐야 하므로 조회도 한 곳에 둔다.
func pinsByPluginName(db *database.DB, pluginName string) dependency.Pins {
	if pluginName == "" {
		return nil
	}
	var p models.Plugin
	if err := db.Where("name = ?", pluginName).First(&p).Error; err != nil {
		return nil
	}
	return dependency.ParsePins(p.DepVersions)
}

// suggestModulesTimeout 은 미등록 import 의 모듈 경로 제안에 쓰는 GOPROXY 조회 상한이다.
const suggestModulesTimeout = 5 * time.Second

// respondPinsError 는 stage 저장 시 의존성 해소 실패를 응답으로 바꾼다.
// 미등록 import 면 400 + BUSINESS_MISSING_MODULES 와 함께 Details 에 {import 경로: 제안 모듈 경로} 를
// 실어, UI 가 "추가하고 저장" 을 원클릭으로 제공할 수 있게 한다. 그 외(파싱 실패, DB 오류,
// single_version_only 위반)는 종전처럼 검증 실패 문자열이다.
func (h *PluginHandler) respondPinsError(c *gin.Context, err error) {
	var missing *dependency.MissingModulesError
	if errors.As(err, &missing) {
		// 제안은 부가 정보다 — GOPROXY 가 느리거나 죽어도 저장 거부 응답 자체는 빨리 나가야 한다.
		// 시간 안에 못 풀면 휴리스틱 경로로 채워진다.
		ctx, cancel := context.WithTimeout(c.Request.Context(), suggestModulesTimeout)
		defer cancel()
		details := suggestModulePaths(ctx, h.moduleResolver, missing.Imports)
		middleware.ErrorResponseWithDetails(c, http.StatusBadRequest, types.ErrCodeMissingModules, err.Error(), details)
		return
	}
	middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
}
