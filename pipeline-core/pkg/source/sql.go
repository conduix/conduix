package source

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	// SQL 드라이버는 사용 시 import
	// _ "github.com/go-sql-driver/mysql"
	// _ "github.com/lib/pq"
)

// SQLSource SQL 데이터 소스
type SQLSource struct {
	driver      string
	dsn         string
	query       string
	params      []string
	incremental *config.IncrementalConfig
	tlsConfig   *config.DBTLSConfig
	db          *sql.DB
}

// NewSQLSource SQL 소스 생성
func NewSQLSource(cfg config.SourceV2) (*SQLSource, error) {
	dsn := cfg.DSN

	// TLS 설정이 있으면 DSN에 TLS 파라미터 추가
	if cfg.DBTLS != nil && cfg.DBTLS.Enabled {
		var err error
		dsn, err = buildTLSEnabledDSN(cfg.Driver, cfg.DSN, cfg.DBTLS)
		if err != nil {
			return nil, fmt.Errorf("failed to configure TLS: %w", err)
		}
	}

	return &SQLSource{
		driver:      cfg.Driver,
		dsn:         dsn,
		query:       cfg.Query,
		params:      cfg.Params,
		incremental: cfg.Incremental,
		tlsConfig:   cfg.DBTLS,
	}, nil
}

// buildTLSEnabledDSN TLS 설정이 적용된 DSN 생성
func buildTLSEnabledDSN(driver, dsn string, tlsCfg *config.DBTLSConfig) (string, error) {
	switch driver {
	case "mysql":
		return buildMySQLTLSDSN(dsn, tlsCfg)
	case "postgres":
		return buildPostgreSQLTLSDSN(dsn, tlsCfg)
	default:
		return dsn, nil // 다른 드라이버는 DSN 그대로 반환
	}
}

// buildMySQLTLSDSN MySQL TLS DSN 생성
// MySQL DSN format: user:password@tcp(host:port)/dbname?param=value
func buildMySQLTLSDSN(dsn string, tlsCfg *config.DBTLSConfig) (string, error) {
	// tls.Config 생성
	tlsConfig, err := buildDBTLSConfig(tlsCfg)
	if err != nil {
		return "", err
	}

	// MySQL TLS 설정 등록
	tlsConfigName := "custom-tls"
	if err := mysql.RegisterTLSConfig(tlsConfigName, tlsConfig); err != nil {
		return "", fmt.Errorf("failed to register TLS config: %w", err)
	}

	// DSN에 tls 파라미터 추가
	if strings.Contains(dsn, "?") {
		dsn = dsn + "&tls=" + tlsConfigName
	} else {
		dsn = dsn + "?tls=" + tlsConfigName
	}

	slog.Default().Info("SQL MySQL TLS enabled", "mode", tlsCfg.Mode)
	return dsn, nil
}

// buildPostgreSQLTLSDSN PostgreSQL TLS DSN 생성
// PostgreSQL DSN format: postgresql://user:password@host:port/dbname?sslmode=value
func buildPostgreSQLTLSDSN(dsn string, tlsCfg *config.DBTLSConfig) (string, error) {
	// sslmode 결정
	sslMode := "require" // 기본값
	if tlsCfg.Mode != "" {
		sslMode = tlsCfg.Mode
	}

	// DSN 파라미터 추가
	params := []string{}

	// sslmode 파라미터
	if !strings.Contains(dsn, "sslmode=") {
		params = append(params, "sslmode="+sslMode)
	}

	// CA 인증서
	if tlsCfg.CACert != "" {
		caCert := expandEnvVars(tlsCfg.CACert)
		params = append(params, "sslrootcert="+caCert)
	}

	// 클라이언트 인증서 (mTLS)
	if tlsCfg.ClientCert != "" && tlsCfg.ClientKey != "" {
		clientCert := expandEnvVars(tlsCfg.ClientCert)
		clientKey := expandEnvVars(tlsCfg.ClientKey)
		params = append(params, "sslcert="+clientCert)
		params = append(params, "sslkey="+clientKey)
	}

	// 파라미터 추가
	if len(params) > 0 {
		if strings.Contains(dsn, "?") {
			dsn = dsn + "&" + strings.Join(params, "&")
		} else {
			dsn = dsn + "?" + strings.Join(params, "&")
		}
	}

	slog.Default().Info("SQL PostgreSQL TLS enabled", "sslmode", sslMode)
	return dsn, nil
}

// buildDBTLSConfig TLS 설정 객체 생성
func buildDBTLSConfig(cfg *config.DBTLSConfig) (*tls.Config, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	tlsConfig := &tls.Config{}

	// SSL 모드에 따른 설정
	switch strings.ToLower(cfg.Mode) {
	case "skip-verify", "prefer", "allow":
		tlsConfig.InsecureSkipVerify = true
	case "required", "require", "verify-ca":
		tlsConfig.InsecureSkipVerify = false
	case "verify-identity", "verify-full":
		tlsConfig.InsecureSkipVerify = false
		if cfg.ServerName != "" {
			tlsConfig.ServerName = cfg.ServerName
		}
	default:
		// 기본값: 인증서 검증 수행
		tlsConfig.InsecureSkipVerify = false
	}

	// CA 인증서 로드
	if cfg.CACert != "" {
		caCert, err := os.ReadFile(expandEnvVars(cfg.CACert))
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// 클라이언트 인증서 (mTLS)
	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(
			expandEnvVars(cfg.ClientCert),
			expandEnvVars(cfg.ClientKey),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

func (s *SQLSource) Name() string {
	return "sql"
}

func (s *SQLSource) Open(ctx context.Context) error {
	db, err := sql.Open(s.driver, s.dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 연결 테스트
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	s.db = db
	return nil
}

func (s *SQLSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		// 파라미터 준비
		params := make([]any, len(s.params))
		for i, p := range s.params {
			params[i] = p
		}

		rows, err := s.db.QueryContext(ctx, s.query, params...)
		if err != nil {
			errs <- fmt.Errorf("query failed: %w", err)
			return
		}
		defer rows.Close()

		// 컬럼 정보
		columns, err := rows.Columns()
		if err != nil {
			errs <- fmt.Errorf("failed to get columns: %w", err)
			return
		}

		for rows.Next() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// 값 스캔
			values := make([]any, len(columns))
			valuePtrs := make([]any, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				errs <- fmt.Errorf("scan failed: %w", err)
				return
			}

			// map으로 변환
			data := make(map[string]any)
			for i, col := range columns {
				val := values[i]
				// []byte를 string으로 변환
				if b, ok := val.([]byte); ok {
					val = string(b)
				}
				data[col] = val
			}

			records <- Record{
				Data: data,
				Metadata: Metadata{
					Source:    "sql",
					Origin:    s.dsn,
					Timestamp: time.Now().UnixMilli(),
				},
			}
		}

		if err := rows.Err(); err != nil {
			errs <- fmt.Errorf("rows error: %w", err)
		}
	}()

	return records, errs
}

func (s *SQLSource) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
