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
	// maxAllowedPacket=0 → 서버의 max_allowed_packet 을 자동 사용(드라이버 기본 64MB 한도 제거).
	// RunnerVersion.Binary(native runner gzip, 수십MB longblob) write 가 클라이언트 측
	// 기본 한도에 걸려 조용히 실패하는 것을 막는다.
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&maxAllowedPacket=0",
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
	if err := db.AutoMigrate(
		// 기본 모델
		&models.Pipeline{},
		&models.PipelineRun{},
		&models.Schedule{},
		&models.User{},
		&models.Agent{},
		&models.Cluster{},
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
		// 플러그인 시스템
		&models.RunnerVersion{},
		&models.Plugin{},
		&models.PluginBuild{},
		&models.StageRevision{},
		&models.AllowedModule{},
		&models.AllowedModuleVersion{},
		// 파이프라인 링크
		&models.PipelineLink{},
		&models.InputCheckpoint{},
	); err != nil {
		return err
	}
	return BackfillModuleVersions(db.DB)
}

// BackfillModuleVersions 는 allowed_modules 의 기본 버전을 allowed_module_versions 에 채운다.
// 다중 버전 테이블 도입 전에 등록된 모듈은 버전 행이 없어 "보유 버전 목록" 이 비어 보이므로,
// 기본 버전 하나를 보유 버전으로 옮긴다. 멱등 — 이미 있으면 건너뛴다.
func BackfillModuleVersions(gdb *gorm.DB) error {
	var mods []models.AllowedModule
	if err := gdb.Find(&mods).Error; err != nil {
		return fmt.Errorf("backfill module versions: list modules: %w", err)
	}
	for _, m := range mods {
		if m.Version == "" {
			continue
		}
		row := models.AllowedModuleVersion{ModulePath: m.ModulePath, Version: m.Version}
		if err := gdb.Where(&models.AllowedModuleVersion{ModulePath: m.ModulePath, Version: m.Version}).
			Attrs(models.AllowedModuleVersion{Status: "active", AddedBy: m.AddedBy}).
			FirstOrCreate(&row).Error; err != nil {
			return fmt.Errorf("backfill module versions: %s@%s: %w", m.ModulePath, m.Version, err)
		}
	}
	return nil
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
