package executor

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

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

// configToSourceV2 map[string]any를 config.SourceV2로 변환
func configToSourceV2(cfg map[string]any) config.SourceV2 {
	result := config.SourceV2{}

	// Type
	if v, ok := cfg["type"].(string); ok {
		result.Type = v
	}

	// File
	if v, ok := cfg["path"].(string); ok {
		result.Path = v
	}
	if v, ok := cfg["paths"].([]any); ok {
		for _, p := range v {
			if ps, ok := p.(string); ok {
				result.Paths = append(result.Paths, ps)
			}
		}
	}
	if v, ok := cfg["format"].(string); ok {
		result.Format = v
	}

	// SQL
	if v, ok := cfg["driver"].(string); ok {
		result.Driver = v
	}
	if v, ok := cfg["dsn"].(string); ok {
		result.DSN = v
	}
	// connection_string 파싱 (driver와 dsn이 없고 connection_string이 있는 경우)
	if result.Driver == "" && result.DSN == "" {
		if connStr, ok := cfg["connection_string"].(string); ok && connStr != "" {
			driver, dsn, err := parseConnectionString(connStr)
			if err == nil {
				result.Driver = driver
				result.DSN = dsn
			}
		}
	}
	if v, ok := cfg["query"].(string); ok {
		result.Query = v
	}
	if v, ok := cfg["params"].([]any); ok {
		for _, p := range v {
			if ps, ok := p.(string); ok {
				result.Params = append(result.Params, ps)
			}
		}
	}

	// HTTP
	if v, ok := cfg["url"].(string); ok {
		result.URL = v
	}
	if v, ok := cfg["method"].(string); ok {
		result.Method = v
	}
	if v, ok := cfg["headers"].(map[string]any); ok {
		result.Headers = make(map[string]string)
		for k, val := range v {
			if vs, ok := val.(string); ok {
				result.Headers[k] = vs
			}
		}
	}
	if v, ok := cfg["body"].(string); ok {
		result.Body = v
	}

	// Auth
	if authCfg, ok := cfg["auth"].(map[string]any); ok {
		result.Auth = &config.AuthConfig{}
		if v, ok := authCfg["type"].(string); ok {
			result.Auth.Type = v
		}
		if v, ok := authCfg["username"].(string); ok {
			result.Auth.Username = v
		}
		if v, ok := authCfg["password"].(string); ok {
			result.Auth.Password = v
		}
		if v, ok := authCfg["token"].(string); ok {
			result.Auth.Token = v
		}
		if v, ok := authCfg["client_id"].(string); ok {
			result.Auth.ClientID = v
		}
		if v, ok := authCfg["client_secret"].(string); ok {
			result.Auth.ClientSecret = v
		}
		if v, ok := authCfg["token_url"].(string); ok {
			result.Auth.TokenURL = v
		}
	}

	// Kafka
	if v, ok := cfg["brokers"].([]any); ok {
		for _, b := range v {
			if bs, ok := b.(string); ok {
				result.Brokers = append(result.Brokers, bs)
			}
		}
	}
	if v, ok := cfg["topics"].([]any); ok {
		for _, t := range v {
			if ts, ok := t.(string); ok {
				result.Topics = append(result.Topics, ts)
			}
		}
	}
	if v, ok := cfg["group_id"].(string); ok {
		result.GroupID = v
	}
	if v, ok := cfg["start_offset"].(string); ok {
		result.StartOffset = v
	}
	if v, ok := cfg["min_bytes"].(float64); ok {
		result.MinBytes = int(v)
	}
	if v, ok := cfg["max_bytes"].(float64); ok {
		result.MaxBytes = int(v)
	}
	if v, ok := cfg["max_wait"].(float64); ok {
		result.MaxWait = int(v)
	}
	if v, ok := cfg["commit_interval"].(float64); ok {
		result.CommitInterval = int(v)
	}

	// SQL Event Table
	if v, ok := cfg["table"].(string); ok {
		result.Table = v
	}
	if v, ok := cfg["id_column"].(string); ok {
		result.IDColumn = v
	}
	if v, ok := cfg["timestamp_column"].(string); ok {
		result.TimestampColumn = v
	}
	if v, ok := cfg["columns"].([]any); ok {
		for _, c := range v {
			if cs, ok := c.(string); ok {
				result.Columns = append(result.Columns, cs)
			}
		}
	}
	if v, ok := cfg["where"].(string); ok {
		result.Where = v
	}
	if v, ok := cfg["order_by"].(string); ok {
		result.OrderBy = v
	}
	if v, ok := cfg["batch_size"].(float64); ok {
		result.BatchSize = int(v)
	}
	if v, ok := cfg["poll_interval"].(float64); ok {
		result.PollInterval = int(v)
	}

	// Pagination
	if paginationCfg, ok := cfg["pagination"].(map[string]any); ok {
		result.Pagination = &config.PaginationConfig{}
		if v, ok := paginationCfg["type"].(string); ok {
			result.Pagination.Type = v
		}
		if v, ok := paginationCfg["page_param"].(string); ok {
			result.Pagination.PageParam = v
		}
		if v, ok := paginationCfg["param_name"].(string); ok {
			result.Pagination.ParamName = v
		}
		if v, ok := paginationCfg["per_page_param"].(string); ok {
			result.Pagination.PerPageParam = v
		}
		if v, ok := paginationCfg["start_page"].(float64); ok {
			result.Pagination.StartPage = int(v)
		}
		if v, ok := paginationCfg["start_value"].(float64); ok {
			result.Pagination.StartValue = int(v)
		}
		if v, ok := paginationCfg["per_page"].(float64); ok {
			result.Pagination.PerPage = int(v)
		}
		if v, ok := paginationCfg["max_pages"].(float64); ok {
			result.Pagination.MaxPages = int(v)
		}
		if v, ok := paginationCfg["data_field"].(string); ok {
			result.Pagination.DataField = v
		}
		if v, ok := paginationCfg["next_field"].(string); ok {
			result.Pagination.NextField = v
		}
		if v, ok := paginationCfg["total_field"].(string); ok {
			result.Pagination.TotalField = v
		}
		if v, ok := paginationCfg["offset_param"].(string); ok {
			result.Pagination.OffsetParam = v
		}
		if v, ok := paginationCfg["offset_path"].(string); ok {
			result.Pagination.OffsetPath = v
		}
		if v, ok := paginationCfg["url_path"].(string); ok {
			result.Pagination.URLPath = v
		}
	}

	// CDC
	if v, ok := cfg["host"].(string); ok {
		result.Host = v
	}
	if v, ok := cfg["port"].(float64); ok {
		result.Port = int(v)
	}
	if v, ok := cfg["username"].(string); ok {
		result.Username = v
	}
	if v, ok := cfg["password"].(string); ok {
		result.Password = v
	}
	if v, ok := cfg["database"].(string); ok {
		result.Database = v
	}
	if v, ok := cfg["tables"].([]any); ok {
		for _, t := range v {
			if ts, ok := t.(string); ok {
				result.Tables = append(result.Tables, ts)
			}
		}
	}
	if v, ok := cfg["server_id"].(float64); ok {
		result.ServerID = uint32(v)
	}
	if v, ok := cfg["slot_name"].(string); ok {
		result.SlotName = v
	}

	// Rate Limit
	if rateLimitCfg, ok := cfg["rate_limit"].(map[string]any); ok {
		result.RateLimit = &config.RateLimitSourceConfig{}
		if v, ok := rateLimitCfg["enabled"].(bool); ok {
			result.RateLimit.Enabled = v
		}
		if v, ok := rateLimitCfg["rate"].(float64); ok {
			result.RateLimit.Rate = int(v)
		}
		if v, ok := rateLimitCfg["interval"].(string); ok {
			result.RateLimit.Interval = v
		}
		if v, ok := rateLimitCfg["burst"].(float64); ok {
			result.RateLimit.Burst = int(v)
		}
		if v, ok := rateLimitCfg["strategy"].(string); ok {
			result.RateLimit.Strategy = v
		}
	}

	return result
}
