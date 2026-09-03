package executor

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// decodeConfig map 설정을 구조체의 yaml 태그를 메타데이터로 삼아 디코드한다.
// 필드별 수동 복사는 구조체에 필드가 추가될 때마다 매퍼도 같이 고쳐야 하는
// 동기화 버그(auth api_key 유실 등)를 만들므로 금지 — 새 필드는 태그만 달면
// 자동 반영된다. 타입이 안 맞는 필드는 건너뛰고 나머지는 정상 디코드된다
// (yaml.TypeError 는 부분 디코드 후 반환되는 에러라 치명 에러와 구분해 삼킨다).
func decodeConfig(cfg map[string]any, out any) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := yaml.Unmarshal(b, out); err != nil {
		var typeErr *yaml.TypeError
		if errors.As(err, &typeErr) {
			slog.Default().Warn("config fields skipped due to type mismatch", "errors", typeErr.Errors)
			return nil
		}
		return fmt.Errorf("unmarshal config: %w", err)
	}
	return nil
}

// parseConnectionString connection_string을 driver와 dsn으로 파싱
// 지원 형식:
//   - URL 형식: mysql://user:pass@host:3306/dbname, postgres://user:pass@host:5432/dbname
//   - Go MySQL DSN: user:pass@tcp(host:3306)/dbname
//   - Go PostgreSQL DSN: user:pass@host:5432/dbname?sslmode=disable
//
// 반환:
//   - driver: mysql, postgres
//   - dsn: Go SQL driver에서 사용하는 DSN 형식
func parseConnectionString(connStr string) (driver string, dsn string, err error) {
	// URL 형식인지 확인 (scheme:// 으로 시작)
	if strings.HasPrefix(connStr, "mysql://") || strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") {
		return parseURLConnectionString(connStr)
	}

	// Go DSN 형식 감지
	// MySQL: user:pass@tcp(host:port)/dbname
	// PostgreSQL: user:pass@host:port/dbname
	if strings.Contains(connStr, "@tcp(") {
		// MySQL Go DSN 형식
		return "mysql", connStr, nil
	}

	// PostgreSQL Go DSN 형식 (user:pass@host:port/dbname)
	if strings.Contains(connStr, "@") && strings.Contains(connStr, "/") {
		// sslmode가 없으면 추가
		if !strings.Contains(connStr, "sslmode=") {
			if strings.Contains(connStr, "?") {
				connStr += "&sslmode=disable"
			} else {
				connStr += "?sslmode=disable"
			}
		}
		return "postgres", connStr, nil
	}

	return "", "", fmt.Errorf("invalid connection string format: %s (use mysql://... or postgres://... or Go DSN format)", connStr)
}

// parseURLConnectionString URL 형식의 connection string 파싱
func parseURLConnectionString(connStr string) (driver string, dsn string, err error) {
	// URL 파싱
	u, err := url.Parse(connStr)
	if err != nil {
		return "", "", fmt.Errorf("invalid connection string: %w", err)
	}

	driver = u.Scheme

	// 사용자 정보
	username := u.User.Username()
	password, _ := u.User.Password()

	// 호스트:포트
	host := u.Hostname()
	port := u.Port()

	// 데이터베이스 (경로에서 추출, 맨 앞 / 제거)
	database := strings.TrimPrefix(u.Path, "/")

	// 쿼리 파라미터
	query := u.RawQuery

	switch driver {
	case "mysql":
		// MySQL DSN 형식: user:password@tcp(host:port)/dbname?params
		if port == "" {
			port = "3306"
		}
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", username, password, host, port, database)
		if query != "" {
			dsn += "?" + query
		}

	case "postgres", "postgresql":
		driver = "postgres"
		// PostgreSQL DSN 형식: user:password@host:port/dbname?params
		if port == "" {
			port = "5432"
		}
		dsn = fmt.Sprintf("%s:%s@%s:%s/%s", username, password, host, port, database)
		if query != "" {
			dsn += "?" + query
		} else {
			// sslmode 기본값 추가
			dsn += "?sslmode=disable"
		}

	default:
		return "", "", fmt.Errorf("unsupported database driver: %s (supported: mysql, postgres)", driver)
	}

	return driver, dsn, nil
}

// configToInputV2 map[string]any를 config.InputV2로 변환
// yaml 태그 기반 자동 디코드 — InputV2 에 필드가 추가되면 여기 수정 없이 반영된다.
func configToInputV2(cfg map[string]any) config.InputV2 {
	result := config.InputV2{}
	if err := decodeConfig(cfg, &result); err != nil {
		slog.Default().Warn("failed to decode input config", "error", err)
	}

	// connection_string 파싱 (명시적 driver/dsn 이 우선)
	if result.Driver == "" && result.DSN == "" {
		if connStr, ok := cfg["connection_string"].(string); ok && connStr != "" {
			if driver, dsn, err := parseConnectionString(connStr); err == nil {
				result.Driver = driver
				result.DSN = dsn
			}
		}
	}

	return result
}
