package executor

import "testing"

// 워크플로우 실행 경로(configToInputV2)에서 api_key 인증 설정이 유실되던 버그 회귀 테스트
func TestConfigToInputV2_AuthAPIKey(t *testing.T) {
	cfg := map[string]any{
		"url": "https://apis.example.com/info",
		"auth": map[string]any{
			"type":         "api_key",
			"api_key":      "${DATA_GO_KR_SERVICE_KEY}",
			"api_key_in":   "query",
			"api_key_name": "serviceKey",
		},
	}

	result := configToInputV2(cfg)

	if result.Auth == nil {
		t.Fatal("expected Auth to be set")
	}
	if result.Auth.Type != "api_key" {
		t.Errorf("expected auth type api_key, got %q", result.Auth.Type)
	}
	if result.Auth.APIKey != "${DATA_GO_KR_SERVICE_KEY}" {
		t.Errorf("expected APIKey to be preserved, got %q", result.Auth.APIKey)
	}
	if result.Auth.APIKeyIn != "query" {
		t.Errorf("expected APIKeyIn query, got %q", result.Auth.APIKeyIn)
	}
	if result.Auth.APIKeyName != "serviceKey" {
		t.Errorf("expected APIKeyName serviceKey, got %q", result.Auth.APIKeyName)
	}
}

// 태그 기반 디코드의 핵심 목적 검증: 수동 매퍼 시절 누락됐던 필드들이
// 매퍼 수정 없이 구조체 태그만으로 매핑되는지 (확장성 회귀 테스트)
func TestConfigToInputV2_AutoMapsTaggedFields(t *testing.T) {
	cfg := map[string]any{
		"url": "https://apis.example.com/info",
		"auth": map[string]any{
			"type":          "oauth2",
			"grant_type":    "refresh_token",
			"refresh_token": "rt-123",
		},
		"incremental": map[string]any{
			"column":    "updated_at",
			"state_key": "restroom",
		},
		"pagination": map[string]any{
			"type":       "page_increment",
			"page_param": "pageNo",
			"per_page":   2000.0, // JSON 경유 숫자는 float64
		},
	}

	result := configToInputV2(cfg)

	if result.Auth == nil || result.Auth.GrantType != "refresh_token" || result.Auth.RefreshToken != "rt-123" {
		t.Errorf("expected oauth2 refresh fields mapped, got %+v", result.Auth)
	}
	if result.Incremental == nil || result.Incremental.Column != "updated_at" || result.Incremental.StateKey != "restroom" {
		t.Errorf("expected incremental mapped, got %+v", result.Incremental)
	}
	if result.Pagination == nil || result.Pagination.PerPage != 2000 {
		t.Errorf("expected pagination.per_page 2000, got %+v", result.Pagination)
	}
}

// 일부 필드 타입 불일치 시 해당 필드만 건너뛰고 나머지는 디코드되는지
func TestDecodeConfig_TypeMismatchSkipsOnlyBadField(t *testing.T) {
	cfg := map[string]any{
		"url":        "https://apis.example.com/info",
		"batch_size": "not-a-number",
	}

	result := configToInputV2(cfg)

	if result.URL != "https://apis.example.com/info" {
		t.Errorf("expected url mapped despite sibling type mismatch, got %q", result.URL)
	}
	if result.BatchSize != 0 {
		t.Errorf("expected mismatched batch_size skipped, got %d", result.BatchSize)
	}
}

// connection_string 파싱과 명시적 driver/dsn 우선순위 보존 검증
func TestConfigToInputV2_ConnectionString(t *testing.T) {
	result := configToInputV2(map[string]any{
		"connection_string": "mysql://user:pw@dbhost:3307/mydb",
	})
	if result.Driver != "mysql" || result.DSN != "user:pw@tcp(dbhost:3307)/mydb" {
		t.Errorf("expected parsed driver/dsn, got %q %q", result.Driver, result.DSN)
	}

	explicit := configToInputV2(map[string]any{
		"driver":            "postgres",
		"dsn":               "explicit-dsn",
		"connection_string": "mysql://user:pw@dbhost:3307/mydb",
	})
	if explicit.Driver != "postgres" || explicit.DSN != "explicit-dsn" {
		t.Errorf("expected explicit driver/dsn to win, got %q %q", explicit.Driver, explicit.DSN)
	}
}
