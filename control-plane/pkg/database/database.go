package database

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// Config 데이터베이스 설정
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	Debug    bool
}

// DB 데이터베이스 인스턴스
type DB struct {
	*gorm.DB
}

// New 새 데이터베이스 연결 생성
func New(cfg *Config) (*DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName)

	logLevel := logger.Silent
	if cfg.Debug {
		logLevel = logger.Info
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logLevel),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 커넥션 풀 설정
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return &DB{db}, nil
}

// Migrate 데이터베이스 마이그레이션 (GORM AutoMigrate)
func (db *DB) Migrate() error {
	return db.AutoMigrate(
		// 기본 모델
		&models.Pipeline{},
		&models.PipelineRun{},
		&models.Schedule{},
		&models.User{},
		&models.Agent{},
		&models.Session{},
		&models.AuditLog{},
		&models.ProvisioningRequest{},
		&models.ProvisioningResult{},
		// 프로젝트 관련
		&models.Project{},
		&models.ProjectOwner{},
		&models.Workflow{},
		&models.WorkflowExecution{},
		&models.ResourcePermission{},
		// 파이프라인 통계
		&models.PipelineExecutionStats{},
		&models.PipelineHourlyStats{},
		// 데이터 유형 및 삭제 전략
		&models.DataType{},
		&models.DataTypePrework{},
		&models.DeleteStrategyPreset{},
		&models.Connection{},
	)
}

// Close 데이터베이스 연결 종료
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Health 헬스체크
func (db *DB) Health() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
