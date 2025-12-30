package provisioner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/shared/types"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// SQLProvisioner SQL 테이블 생성 Provisioner
type SQLProvisioner struct {
	sinkType types.SinkType
}

// NewSQLProvisioner SQL Provisioner 생성
func NewSQLProvisioner() *SQLProvisioner {
	return &SQLProvisioner{
		sinkType: types.SinkTypeSQL,
	}
}

func (p *SQLProvisioner) SinkType() types.SinkType {
	return p.sinkType
}

func (p *SQLProvisioner) SupportedTypes() []types.ProvisioningType {
	return []types.ProvisioningType{
		types.ProvisioningTypeTableCreation,
		types.ProvisioningTypeExternal,
	}
}

// SQLProvisioningConfig SQL 프로비저닝 설정
type SQLProvisioningConfig struct {
	Driver    string            `json:"driver"`     // mysql, postgres
	Host      string            `json:"host"`       // DB 호스트
	Port      int               `json:"port"`       // DB 포트
	Database  string            `json:"database"`   // 데이터베이스 이름
	Username  string            `json:"username"`   // 사용자명
	Password  string            `json:"password"`   // 비밀번호
	TableName string            `json:"table_name"` // 생성할 테이블 이름
	Columns   []SQLColumn       `json:"columns"`    // 컬럼 정의
	SSLMode   string            `json:"ssl_mode"`   // SSL 모드 (postgres)
	Charset   string            `json:"charset"`    // 문자셋 (mysql)
	Extra     map[string]string `json:"extra"`      // 추가 옵션
}

// SQLColumn SQL 컬럼 정의
type SQLColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`        // VARCHAR(255), INT, TEXT, TIMESTAMP 등
	Nullable   bool   `json:"nullable"`    // NULL 허용 여부
	PrimaryKey bool   `json:"primary_key"` // Primary Key 여부
	Default    string `json:"default"`     // 기본값
}

func (p *SQLProvisioner) Provision(ctx context.Context, req *types.ProvisioningRequest) (*types.ProvisioningResult, error) {
	// 외부 프로비저닝인 경우
	if req.Type == types.ProvisioningTypeExternal {
		return &types.ProvisioningResult{
			ID:         uuid.New().String(),
			RequestID:  req.ID,
			PipelineID: req.PipelineID,
			SinkType:   req.SinkType,
			Status:     types.ProvisioningStatusPending,
			Message:    "Waiting for external SQL table provisioning",
			Metadata: map[string]any{
				"external_url": req.ExternalURL,
				"callback_url": req.CallbackURL,
			},
		}, nil
	}

	// 설정 파싱
	config, err := p.parseConfig(req.Config)
	if err != nil {
		return nil, fmt.Errorf("invalid sql config: %w", err)
	}

	// 테이블 생성
	if err := p.createTable(ctx, config); err != nil {
		now := time.Now()
		return &types.ProvisioningResult{
			ID:          uuid.New().String(),
			RequestID:   req.ID,
			PipelineID:  req.PipelineID,
			SinkType:    req.SinkType,
			Status:      types.ProvisioningStatusFailed,
			Message:     "Failed to create SQL table",
			ErrorDetail: err.Error(),
			CompletedAt: &now,
		}, nil
	}

	now := time.Now()
	return &types.ProvisioningResult{
		ID:         uuid.New().String(),
		RequestID:  req.ID,
		PipelineID: req.PipelineID,
		SinkType:   req.SinkType,
		Status:     types.ProvisioningStatusCompleted,
		TableName:  config.TableName,
		Message:    fmt.Sprintf("SQL table '%s' created successfully in database '%s'", config.TableName, config.Database),
		Metadata: map[string]any{
			"driver":   config.Driver,
			"host":     config.Host,
			"port":     config.Port,
			"database": config.Database,
			"columns":  len(config.Columns),
		},
		CompletedAt: &now,
	}, nil
}

func (p *SQLProvisioner) parseConfig(config map[string]any) (*SQLProvisioningConfig, error) {
	cfg := &SQLProvisioningConfig{
		Driver:  "mysql",
		Port:    3306,
		SSLMode: "disable",
		Charset: "utf8mb4",
	}

	// Driver
	if driver, ok := config["driver"].(string); ok && driver != "" {
		cfg.Driver = driver
		if driver == "postgres" {
			cfg.Port = 5432
		}
	}

	// Host
	if host, ok := config["host"].(string); ok && host != "" {
		cfg.Host = host
	} else {
		return nil, fmt.Errorf("host is required")
	}

	// Port
	if port, ok := config["port"].(float64); ok {
		cfg.Port = int(port)
	} else if port, ok := config["port"].(int); ok {
		cfg.Port = port
	}

	// Database
	if database, ok := config["database"].(string); ok && database != "" {
		cfg.Database = database
	} else {
		return nil, fmt.Errorf("database is required")
	}

	// Username
	if username, ok := config["username"].(string); ok && username != "" {
		cfg.Username = username
	} else {
		return nil, fmt.Errorf("username is required")
	}

	// Password
	if password, ok := config["password"].(string); ok {
		cfg.Password = password
	}

	// TableName
	if tableName, ok := config["table_name"].(string); ok && tableName != "" {
		cfg.TableName = tableName
	} else {
		return nil, fmt.Errorf("table_name is required")
	}

	// Columns
	if columns, ok := config["columns"].([]any); ok {
		for _, col := range columns {
			if colMap, ok := col.(map[string]any); ok {
				column := SQLColumn{}
				if name, ok := colMap["name"].(string); ok {
					column.Name = name
				}
				if colType, ok := colMap["type"].(string); ok {
					column.Type = colType
				}
				if nullable, ok := colMap["nullable"].(bool); ok {
					column.Nullable = nullable
				}
				if pk, ok := colMap["primary_key"].(bool); ok {
					column.PrimaryKey = pk
				}
				if def, ok := colMap["default"].(string); ok {
					column.Default = def
				}
				cfg.Columns = append(cfg.Columns, column)
			}
		}
	}

	// 컬럼이 없으면 기본 스키마 사용
	if len(cfg.Columns) == 0 {
		cfg.Columns = p.getDefaultColumns()
	}

	// SSLMode (postgres)
	if sslMode, ok := config["ssl_mode"].(string); ok {
		cfg.SSLMode = sslMode
	}

	// Charset (mysql)
	if charset, ok := config["charset"].(string); ok {
		cfg.Charset = charset
	}

	return cfg, nil
}

func (p *SQLProvisioner) getDefaultColumns() []SQLColumn {
	return []SQLColumn{
		{Name: "id", Type: "VARCHAR(36)", PrimaryKey: true},
		{Name: "data", Type: "JSON", Nullable: true},
		{Name: "created_at", Type: "TIMESTAMP", Default: "CURRENT_TIMESTAMP"},
		{Name: "source", Type: "VARCHAR(255)", Nullable: true},
		{Name: "pipeline_id", Type: "VARCHAR(36)", Nullable: true},
	}
}

func (p *SQLProvisioner) buildDSN(config *SQLProvisioningConfig) string {
	switch config.Driver {
	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true",
			config.Username, config.Password, config.Host, config.Port, config.Database, config.Charset)
	case "postgres":
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			config.Host, config.Port, config.Username, config.Password, config.Database, config.SSLMode)
	default:
		return ""
	}
}

func (p *SQLProvisioner) createTable(ctx context.Context, config *SQLProvisioningConfig) error {
	dsn := p.buildDSN(config)
	if dsn == "" {
		return fmt.Errorf("unsupported driver: %s", config.Driver)
	}

	db, err := sql.Open(config.Driver, dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 연결 테스트
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// CREATE TABLE 쿼리 생성
	createSQL := p.buildCreateTableSQL(config)

	// 테이블 생성
	_, err = db.ExecContext(ctx, createSQL)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

func (p *SQLProvisioner) buildCreateTableSQL(config *SQLProvisioningConfig) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", config.TableName))

	var primaryKeys []string
	columnDefs := make([]string, 0, len(config.Columns))

	for _, col := range config.Columns {
		colDef := fmt.Sprintf("  %s %s", col.Name, col.Type)

		if !col.Nullable {
			colDef += " NOT NULL"
		}

		if col.Default != "" {
			if strings.ToUpper(col.Default) == "CURRENT_TIMESTAMP" {
				colDef += " DEFAULT CURRENT_TIMESTAMP"
			} else {
				colDef += fmt.Sprintf(" DEFAULT '%s'", col.Default)
			}
		}

		columnDefs = append(columnDefs, colDef)

		if col.PrimaryKey {
			primaryKeys = append(primaryKeys, col.Name)
		}
	}

	sb.WriteString(strings.Join(columnDefs, ",\n"))

	if len(primaryKeys) > 0 {
		sb.WriteString(fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(primaryKeys, ", ")))
	}

	sb.WriteString("\n)")

	// MySQL 엔진 및 문자셋
	if config.Driver == "mysql" {
		sb.WriteString(fmt.Sprintf(" ENGINE=InnoDB DEFAULT CHARSET=%s", config.Charset))
	}

	return sb.String()
}

func (p *SQLProvisioner) Validate(config map[string]any) error {
	_, err := p.parseConfig(config)
	return err
}

func (p *SQLProvisioner) RequiresExternalSetup() bool {
	return false // 자동 생성 가능
}

func (p *SQLProvisioner) GetExternalSetupURL(req *types.ProvisioningRequest) string {
	return req.ExternalURL
}

// CheckTableExists 테이블 존재 여부 확인
func (p *SQLProvisioner) CheckTableExists(config *SQLProvisioningConfig) (bool, error) {
	dsn := p.buildDSN(config)
	db, err := sql.Open(config.Driver, dsn)
	if err != nil {
		return false, err
	}
	defer db.Close()

	var query string
	switch config.Driver {
	case "mysql":
		query = fmt.Sprintf("SELECT 1 FROM information_schema.tables WHERE table_schema = '%s' AND table_name = '%s'",
			config.Database, config.TableName)
	case "postgres":
		query = fmt.Sprintf("SELECT 1 FROM information_schema.tables WHERE table_catalog = '%s' AND table_name = '%s'",
			config.Database, config.TableName)
	}

	var exists int
	err = db.QueryRow(query).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// DropTable 테이블 삭제 (롤백용)
func (p *SQLProvisioner) DropTable(config *SQLProvisioningConfig) error {
	dsn := p.buildDSN(config)
	db, err := sql.Open(config.Driver, dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", config.TableName))
	return err
}
