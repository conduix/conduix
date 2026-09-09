package models

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// GORM 의 Find/First 는 SELECT * 를 내므로 목록 조회에서도 32MB급 Binary 를 전부 읽는다.
// 실측: 17행 453MB, /runner/versions 응답 5.3초. 이 목록으로 Select 해 그것을 막는다.
func TestRunnerVersionMetaColumns_ExcludesHeavyColumns(t *testing.T) {
	cols := RunnerVersionMetaColumns()

	for _, heavy := range []string{"binary", "build_log"} {
		if slices.Contains(cols, heavy) {
			t.Fatalf("%q 는 목록 조회에서 제외돼야 한다(용량이 크다)", heavy)
		}
	}
}

// 바이너리 존재 여부는 BinarySize 로 판정한다 — 그 컬럼이 빠지면 정상 버전을
// "바이너리 없음" 으로 오판해 실행을 막는다.
func TestRunnerVersionMetaColumns_KeepsBinarySize(t *testing.T) {
	if !slices.Contains(RunnerVersionMetaColumns(), "binary_size") {
		t.Fatal("binary_size 가 있어야 Binary 를 읽지 않고 존재 여부를 판정할 수 있다")
	}
}

// 화면·판정에 쓰는 필드가 빠지면 값이 조용히 비어 표시된다.
// 모델 필드와 맞대어, 의도적으로 제외한 것 외에 누락이 없는지 본다.
//
// gorm 의 컬럼명 변환 규칙(연속 대문자 처리 등)을 테스트에서 재현하면 그 자체가
// 틀릴 수 있으므로, 필드명 → 컬럼명 대응을 명시한다.
func TestRunnerVersionMetaColumns_CoversModelFields(t *testing.T) {
	// 필드명 → 실제 컬럼명. 의도적 제외는 빈 문자열.
	want := map[string]string{
		"ID":           "id",
		"BuildNumber":  "build_number",
		"Status":       "status",
		"ImageTag":     "image_tag",
		"ImageDigest":  "image_digest",
		"Binary":       "", // 32MB급 — DownloadBinary 에서만 읽는다
		"BinarySize":   "binary_size",
		"SourceHash":   "source_hash",
		"PluginIDs":    "plugin_ids",
		"PluginHashes": "plugin_hashes",
		"RevisionSeq":  "revision_seq",
		"Trigger":      "trigger",
		"ParentID":     "parent_id",
		"BuildLog":     "", // mediumtext — 단건 조회에서만 읽는다
		"Error":        "error",
		"DurationMs":   "duration_ms",
		"CreatedBy":    "created_by",
		"StartedAt":    "started_at",
		"FinishedAt":   "finished_at",
		"CreatedAt":    "created_at",
	}

	cols := RunnerVersionMetaColumns()
	have := make(map[string]bool, len(cols))
	for _, c := range cols {
		have[c] = true
	}

	rt := reflect.TypeOf(RunnerVersion{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		col, known := want[name]
		if !known {
			t.Errorf("모델에 새 필드 %s 가 생겼다 — 메타 목록에 넣을지 판단해 이 테스트에 등록하라", name)
			continue
		}
		if col == "" {
			if have[strings.ToLower(name)] {
				t.Errorf("%s 는 제외 대상인데 메타 목록에 있다", name)
			}
			continue
		}
		if !have[col] {
			t.Errorf("컬럼 %q(필드 %s)가 메타 목록에 없다 — 화면에서 빈 값으로 보인다", col, name)
		}
	}
}
