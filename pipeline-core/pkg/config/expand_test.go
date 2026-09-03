package config

import (
	"reflect"
	"testing"
)

func TestExpandEnvInConfig(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret-123")
	t.Setenv("TEST_HOST", "db.internal")

	in := map[string]any{
		"api_key":   "${TEST_API_KEY}",
		"plain":     "no-vars-here",
		"dsn":       "user:pw@tcp(${TEST_HOST}:3306)/db",
		"count":     42, // 비문자열은 그대로
		"nested":    map[string]any{"key": "${TEST_API_KEY}"},
		"list":      []any{"${TEST_HOST}", "static", 7},
		"undefined": "${NOT_SET_VAR}", // 미설정 → 빈 문자열 (os.ExpandEnv 동작)
	}
	got := ExpandEnvInConfig(in)

	want := map[string]any{
		"api_key":   "secret-123",
		"plain":     "no-vars-here",
		"dsn":       "user:pw@tcp(db.internal:3306)/db",
		"count":     42,
		"nested":    map[string]any{"key": "secret-123"},
		"list":      []any{"db.internal", "static", 7},
		"undefined": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expand mismatch:\n got:  %#v\n want: %#v", got, want)
	}

	// 원본 불변 (fan-out 시 다른 소비자 오염 방지)
	if in["api_key"] != "${TEST_API_KEY}" {
		t.Errorf("original config mutated: %v", in["api_key"])
	}
	if in["nested"].(map[string]any)["key"] != "${TEST_API_KEY}" {
		t.Errorf("nested original mutated")
	}
}

func TestExpandEnvInConfig_Nil(t *testing.T) {
	if got := ExpandEnvInConfig(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// env 미설정 시 평문 config 는 그대로 통과해야 한다 (기존 동작 보존)
func TestExpandEnvInConfig_PlainUnchanged(t *testing.T) {
	in := map[string]any{"key": "plain-value", "n": 1}
	got := ExpandEnvInConfig(in)
	if got["key"] != "plain-value" || got["n"] != 1 {
		t.Errorf("plain config altered: %v", got)
	}
}
