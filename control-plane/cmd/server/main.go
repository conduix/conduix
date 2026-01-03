package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/caarlos0/env/v10"

	"github.com/conduix/conduix/control-plane/internal/api"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/config"
	"github.com/conduix/conduix/control-plane/pkg/database"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

// Config 서버 설정
type Config struct {
	// Database
	DBHost     string `env:"DB_HOST" envDefault:"localhost"`
	DBPort     int    `env:"DB_PORT" envDefault:"3306"`
	DBUser     string `env:"DB_USER" envDefault:"conduixuser"`
	DBPassword string `env:"DB_PASSWORD" envDefault:"conduixpassword"`
	DBName     string `env:"DB_NAME" envDefault:"conduix"`

	// Redis
	RedisAddr     string `env:"REDIS_ADDR" envDefault:"localhost:6379"`
	RedisPassword string `env:"REDIS_PASSWORD" envDefault:""`
	RedisDB       int    `env:"REDIS_DB" envDefault:"0"`

	// Server
	Port      int    `env:"PORT" envDefault:"8080"`
	JWTSecret string `env:"JWT_SECRET" envDefault:"your-secret-key"`

	// OAuth2 Config
	OAuthConfigPath      string `env:"OAUTH_CONFIG_PATH" envDefault:""`
	OAuthDefaultRedirect string `env:"OAUTH_REDIRECT_URL" envDefault:"http://localhost:8080/api/v1/auth/callback"`

	// Users Config
	UsersConfigPath string `env:"USERS_CONFIG_PATH" envDefault:""`

	// Frontend
	FrontendURL string `env:"FRONTEND_URL" envDefault:"http://localhost:3000"`

	// Database Migration
	AutoMigrate bool `env:"AUTO_MIGRATE" envDefault:"false"`

	// Flags (not from env)
	Migrate     bool
	ShowVersion bool
}

func main() {
	cfg := Config{}

	// 환경변수에서 설정 로드
	if err := env.Parse(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing environment variables: %v\n", err)
		os.Exit(1)
	}

	// 명령행 인자 파싱 (환경변수보다 우선)
	flag.IntVar(&cfg.DBPort, "db-port", cfg.DBPort, "Database port")
	flag.StringVar(&cfg.DBHost, "db-host", cfg.DBHost, "Database host")
	flag.StringVar(&cfg.DBUser, "db-user", cfg.DBUser, "Database user")
	flag.StringVar(&cfg.DBPassword, "db-password", cfg.DBPassword, "Database password")
	flag.StringVar(&cfg.DBName, "db-name", cfg.DBName, "Database name")
	flag.StringVar(&cfg.RedisAddr, "redis-addr", cfg.RedisAddr, "Redis address")
	flag.StringVar(&cfg.RedisPassword, "redis-password", cfg.RedisPassword, "Redis password")
	flag.IntVar(&cfg.RedisDB, "redis-db", cfg.RedisDB, "Redis database number")
	flag.StringVar(&cfg.JWTSecret, "jwt-secret", cfg.JWTSecret, "JWT secret key")
	flag.IntVar(&cfg.Port, "port", cfg.Port, "API server port")
	flag.StringVar(&cfg.OAuthConfigPath, "oauth-config", cfg.OAuthConfigPath, "OAuth providers config file path (YAML)")
	flag.StringVar(&cfg.OAuthDefaultRedirect, "oauth-redirect-url", cfg.OAuthDefaultRedirect, "Default OAuth redirect URL")
	flag.StringVar(&cfg.UsersConfigPath, "users-config", cfg.UsersConfigPath, "Users config file path (YAML)")
	flag.StringVar(&cfg.FrontendURL, "frontend-url", cfg.FrontendURL, "Frontend URL for OAuth callback redirect")
	flag.BoolVar(&cfg.Migrate, "migrate", false, "Run database migrations")
	flag.BoolVar(&cfg.ShowVersion, "version", false, "Show version")

	flag.Parse()

	if cfg.ShowVersion {
		fmt.Printf("Conduix Control Plane %s (built: %s)\n", version, buildTime)
		os.Exit(0)
	}

	// 사용자 설정 로드 (파일 + 환경변수)
	usersConfig := config.LoadUsersConfigFromEnv()
	if cfg.UsersConfigPath != "" {
		fileCfg, err := config.LoadUsersConfig(cfg.UsersConfigPath)
		if err != nil {
			fmt.Printf("Warning: Failed to load users config from %s: %v\n", cfg.UsersConfigPath, err)
		} else {
			usersConfig.Merge(fileCfg)
		}
	}

	// 관리자 목록 출력
	if len(usersConfig.AdminEmails) > 0 {
		fmt.Printf("Admin users configured: %v\n", usersConfig.AdminEmails)
	}
	if len(usersConfig.OperatorEmails) > 0 {
		fmt.Printf("Operator users configured: %v\n", usersConfig.OperatorEmails)
	}

	// 데이터베이스 연결
	dbConfig := &database.Config{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		Debug:    false,
	}

	db, err := database.New(dbConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	// 마이그레이션 (환경변수 또는 플래그로 활성화)
	if cfg.AutoMigrate || cfg.Migrate {
		fmt.Println("Running database migrations...")
		if err := db.Migrate(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running migrations: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Migrations completed")
	}

	// Redis 서비스 생성 (선택적 - 실패해도 서버 시작)
	var redisService *services.RedisService
	redisConfig := &services.RedisServiceConfig{
		Addr:             cfg.RedisAddr,
		Password:         cfg.RedisPassword,
		DB:               cfg.RedisDB,
		EnableRetryQueue: true,
	}
	redisService, err = services.NewRedisService(redisConfig)
	if err != nil {
		fmt.Printf("Warning: Redis connection failed: %v (continuing without Redis)\n", err)
		redisService = nil
	} else {
		defer func() { _ = redisService.Close() }()
	}

	// 스케줄러 서비스 생성 및 시작
	schedulerService := services.NewSchedulerService(db, redisService, nil)
	if err := schedulerService.Start(); err != nil {
		fmt.Printf("Warning: Scheduler service failed to start: %v\n", err)
	} else {
		defer func() { _ = schedulerService.Stop() }()
	}

	// API 서버 생성
	server := api.NewServer(db, redisService, schedulerService, cfg.JWTSecret, usersConfig, cfg.FrontendURL)

	// OAuth2 프로바이더 설정 로드
	var oauthConfig *config.OAuthConfig
	if cfg.OAuthConfigPath != "" {
		var err error
		oauthConfig, err = config.LoadOAuthConfigWithEnv(cfg.OAuthConfigPath, cfg.OAuthDefaultRedirect)
		if err != nil {
			fmt.Printf("Warning: Failed to load OAuth config from %s: %v\n", cfg.OAuthConfigPath, err)
			oauthConfig = config.GetDefaultOAuthConfig()
		}
	} else {
		oauthConfig = config.GetDefaultOAuthConfig()
	}

	// 환경변수에서 credentials 주입 (LoadOAuthConfigWithEnv에서 처리됨)
	// GetDefaultOAuthConfig 사용 시에도 환경변수 적용 필요
	if cfg.OAuthConfigPath == "" {
		for id, provider := range oauthConfig.Providers {
			upperID := strings.ToUpper(id)
			provider.ClientID = os.Getenv(fmt.Sprintf("%s_CLIENT_ID", upperID))
			provider.ClientSecret = os.Getenv(fmt.Sprintf("%s_CLIENT_SECRET", upperID))
			provider.RedirectURL = os.Getenv(fmt.Sprintf("%s_REDIRECT_URL", upperID))
			if provider.RedirectURL == "" {
				provider.RedirectURL = cfg.OAuthDefaultRedirect
			}
		}
	}

	// 프로바이더 등록
	server.RegisterOAuthProviders(oauthConfig)
	enabledProviders := oauthConfig.GetEnabledProviders()
	if len(enabledProviders) > 0 {
		fmt.Printf("OAuth2 providers registered: ")
		names := make([]string, 0, len(enabledProviders))
		for id := range enabledProviders {
			names = append(names, id)
		}
		fmt.Println(strings.Join(names, ", "))
	} else {
		fmt.Println("Warning: No OAuth2 providers configured")
	}

	// 서버 시작
	go func() {
		addr := fmt.Sprintf(":%d", cfg.Port)
		fmt.Printf("Control plane server listening on %s\n", addr)
		if err := server.Run(addr); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// 시그널 대기
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	fmt.Printf("\nReceived signal: %v\n", sig)
	fmt.Println("Shutting down...")
}
