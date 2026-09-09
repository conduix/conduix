package services

import (
	"strings"
	"testing"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// 코어 변경으로 빌드가 필요할 때 stage 이름·seq 를 나열하면 무의미하다.
// 실측: "stage []가 seq #8에서 수정되었습니다. 현재 runner는 seq #8 기준 빌드입니다" —
// stage 목록이 비고 seq 가 같아 사용자가 이유를 알 수 없었다.
func TestBuildRequiredError_CoreChangedMessage(t *testing.T) {
	err := &BuildRequiredError{
		Reason:             BuildReasonCoreChanged,
		PendingPlugins:     nil, // 코어 변경이면 비어 있다
		LatestReadyVersion: "rv-8a80887a",
		LatestReadySeq:     8,
		LatestSeq:          8,
	}

	msg := err.Error()
	if strings.Contains(msg, "stage []") {
		t.Fatalf("빈 stage 목록을 노출해서는 안 된다: %s", msg)
	}
	if !strings.Contains(msg, "코어") {
		t.Fatalf("코어 변경임을 알려야 한다: %s", msg)
	}
	if !strings.Contains(msg, "rv-8a80887a") {
		t.Fatalf("어느 runner 가 낡았는지 알려야 한다: %s", msg)
	}
}

// stage 수정 케이스는 기존 메시지를 유지한다 — 어느 stage 를 봐야 하는지가 핵심 정보다.
func TestBuildRequiredError_PluginChangedMessage(t *testing.T) {
	err := &BuildRequiredError{
		Reason:         BuildReasonPluginChanged,
		PendingPlugins: []models.Plugin{{Name: "geocode_kakao"}},
		LatestReadySeq: 7,
		LatestSeq:      9,
	}

	msg := err.Error()
	if !strings.Contains(msg, "geocode_kakao") {
		t.Fatalf("수정된 stage 이름이 있어야 한다: %s", msg)
	}
	if !strings.Contains(msg, "#9") || !strings.Contains(msg, "#7") {
		t.Fatalf("seq 비교가 있어야 한다: %s", msg)
	}
}

// Reason 미지정(구 코드 경로)도 plugin 메시지로 폴백해야 한다 — 빈 메시지보다 낫다.
func TestBuildRequiredError_DefaultsToPluginMessage(t *testing.T) {
	err := &BuildRequiredError{
		PendingPlugins: []models.Plugin{{Name: "x"}},
		LatestReadySeq: 1,
		LatestSeq:      2,
	}
	if !strings.Contains(err.Error(), "stage [x]") {
		t.Fatalf("폴백 메시지가 stage 형식이어야 한다: %s", err.Error())
	}
}

// ready 버전이 없을 때는 "무엇을 하라" 가 명확해야 한다.
func TestBuildRequiredError_NoReadyVersionMessage(t *testing.T) {
	err := &BuildRequiredError{Reason: BuildReasonNoReadyVersion}
	msg := err.Error()
	if strings.Contains(msg, "stage []") {
		t.Fatalf("빈 stage 목록 노출: %s", msg)
	}
	if !strings.Contains(msg, "빌드") {
		t.Fatalf("빌드하라는 안내가 있어야 한다: %s", msg)
	}
}

// ready 지만 바이너리가 없는 경우 — 어느 버전이 문제인지 알려야 다시 빌드할 수 있다.
func TestBuildRequiredError_BinaryMissingMessage(t *testing.T) {
	err := &BuildRequiredError{Reason: BuildReasonBinaryMissing, LatestReadyVersion: "rv-abc"}
	msg := err.Error()
	if !strings.Contains(msg, "rv-abc") {
		t.Fatalf("문제 버전을 알려야 한다: %s", msg)
	}
	if strings.Contains(msg, "stage []") {
		t.Fatalf("빈 stage 목록 노출: %s", msg)
	}
}
