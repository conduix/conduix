package seed

import (
	"strings"
	"testing"
)

// SEED_* env 미설정 시 placeholder 로 폴백해야 프로덕션 동작이 불변이다.
func TestLoadEndpoints_DefaultsWhenUnset(t *testing.T) {
	for _, k := range []string{
		"SEED_MOCK_REST_URL", "SEED_MOCK_SOURCE_DSN", "SEED_MOCK_TARGET_DSN",
		"SEED_MOCK_PG_DSN", "SEED_MOCK_KAFKA_BROKER", "SEED_MOCK_CDC_HOST",
	} {
		t.Setenv(k, "")
	}
	ep := loadEndpoints()
	if ep.restBaseURL != "https://api.example.com" {
		t.Errorf("restBaseURL fallback: got %q", ep.restBaseURL)
	}
	if !strings.Contains(ep.sourceDSN, "localhost:3306") {
		t.Errorf("sourceDSN fallback: got %q", ep.sourceDSN)
	}
	if ep.kafkaBroker != "localhost:9092" {
		t.Errorf("kafkaBroker fallback: got %q", ep.kafkaBroker)
	}
}

// SEED_* env 설정 시 mock DNS 가 실제 파이프라인 config 에 주입되어야 한다.
func TestLoadEndpoints_EnvOverride(t *testing.T) {
	t.Setenv("SEED_MOCK_REST_URL", "http://mock-rest:8080")
	t.Setenv("SEED_MOCK_SOURCE_DSN", "root:pw@tcp(mock-mysql:3306)/sourcedb")
	t.Setenv("SEED_MOCK_KAFKA_BROKER", "mock-kafka:9092")

	ep := loadEndpoints()
	if ep.restBaseURL != "http://mock-rest:8080" {
		t.Errorf("restBaseURL override: got %q", ep.restBaseURL)
	}

	// REST → MySQL 샘플의 소스 URL 이 실제로 mock DNS 를 쓰는지 관통 검증
	in := restInput(ep.restBaseURL + "/orders")
	if got := in.Config["url"]; got != "http://mock-rest:8080/orders" {
		t.Errorf("rest input url: got %v", got)
	}
	// Kafka 소스 브로커 주입 검증
	mi := mysqlInput(ep, "orders")
	if got := mi.Config["dsn"]; got != "root:pw@tcp(mock-mysql:3306)/sourcedb" {
		t.Errorf("mysql input dsn: got %v", got)
	}
}
