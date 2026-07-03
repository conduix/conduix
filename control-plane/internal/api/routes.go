package api

import (
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/conduix/conduix/control-plane/internal/api/handlers"
	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/config"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/shared/types"
)

// Server API 서버
type Server struct {
	router              *gin.Engine
	db                  *database.DB
	redisService        *services.RedisService
	schedulerService    *services.SchedulerService
	jwtSecret           []byte
	pipelineHandler     *handlers.PipelineHandler
	authHandler         *handlers.AuthHandler
	workflowHandler     *handlers.WorkflowHandler
	statsHandler        *handlers.StatsHandler
	scheduleHandler     *handlers.ScheduleHandler
	graphHandler        *handlers.GraphHandler
	dataTypeHandler     *handlers.DataTypeHandler
	userHandler         *handlers.UserHandler
	projectHandler      *handlers.ProjectHandler
	agentHandler        *handlers.AgentHandler
	clusterHandler      *handlers.ClusterHandler
	utilsHandler        *handlers.UtilsHandler
	checkpointHandler   *handlers.CheckpointHandler
	pipelineLinkHandler *handlers.PipelineLinkHandler
	stageHandler        *handlers.StageHandler
	previewHandler      *handlers.PreviewHandler
	pluginHandler       *handlers.PluginHandler
	runnerHandler       *handlers.RunnerHandler
	lspHandler          *handlers.LSPHandler
	startTime           time.Time
	version             string
	allowedOrigins      []string
}

// Version 버전 정보 (빌드 시 설정)
var Version = "dev"

// NewServer 새 서버 생성
func NewServer(db *database.DB, redisService *services.RedisService, schedulerService *services.SchedulerService, jwtSecret string, usersConfig *config.UsersConfig, frontendURL string) *Server {
	gin.SetMode(gin.ReleaseMode)

	// Kafka 서비스 초기화
	kafkaService := services.NewKafkaService(nil)
	linkService := services.NewPipelineLinkService(db, kafkaService, nil)

	// control-plane은 K8s 크레덴셜을 갖지 않는다(위임 구조). K8s Job 생성·deployment 스케일은
	// 각 cluster의 worker/배포 차트가 담당한다. (docs/EXECUTION_TOPOLOGY_INTENT.md)

	s := &Server{
		router:              gin.New(),
		db:                  db,
		redisService:        redisService,
		schedulerService:    schedulerService,
		jwtSecret:           []byte(jwtSecret),
		pipelineHandler:     handlers.NewPipelineHandler(db, redisService),
		authHandler:         handlers.NewAuthHandler(db, jwtSecret, usersConfig, frontendURL),
		workflowHandler:     handlers.NewWorkflowHandler(db, redisService),
		statsHandler:        handlers.NewStatsHandler(db),
		scheduleHandler:     handlers.NewScheduleHandler(db, schedulerService),
		graphHandler:        handlers.NewGraphHandler(db, redisService),
		dataTypeHandler:     handlers.NewDataTypeHandler(db),
		userHandler:         handlers.NewUserHandler(db),
		projectHandler:      handlers.NewProjectHandler(db),
		agentHandler:        handlers.NewAgentHandler(db, redisService),
		clusterHandler:      handlers.NewClusterHandler(db, redisService),
		utilsHandler:        handlers.NewUtilsHandler(),
		checkpointHandler:   handlers.NewCheckpointHandler(db),
		pipelineLinkHandler: handlers.NewPipelineLinkHandler(db, linkService),
		stageHandler:        handlers.NewStageHandler(db),
		previewHandler:      handlers.NewPreviewHandler(),
		pluginHandler:       handlers.NewPluginHandler(db),
		runnerHandler:       handlers.NewRunnerHandler(db),
		lspHandler:          handlers.NewLSPHandler(os.Getenv("CONDUIX_SDK_PATH")),
		startTime:           time.Now(),
		version:             Version,
		allowedOrigins:      buildAllowedOrigins(frontendURL),
	}

	s.setupRoutes()
	return s
}

// buildAllowedOrigins CORS allowlist를 구성한다.
// frontendURL(스킴+호스트)과 CORS_ALLOWED_ORIGINS(콤마 구분) 환경변수를 합친다.
func buildAllowedOrigins(frontendURL string) []string {
	origins := make([]string, 0, 2)
	if o := originFromURL(frontendURL); o != "" {
		origins = append(origins, o)
	}
	for _, extra := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		if trimmed := strings.TrimSpace(extra); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

// originFromURL은 URL에서 CORS Origin(scheme://host[:port])만 추출한다.
func originFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// setupRoutes 라우트 설정
func (s *Server) setupRoutes() {
	// 미들웨어
	s.router.Use(gin.Recovery())
	s.router.Use(middleware.CORSMiddleware(s.allowedOrigins))
	s.router.Use(middleware.RequestIDMiddleware())

	// Index (서비스 정보)
	s.router.GET("/", s.index)

	// 헬스체크 (인증 불필요)
	s.router.GET("/health", s.health)
	s.router.GET("/ready", s.ready)

	// Prometheus 메트릭 (스크레이핑용, 인증 불필요)
	s.router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API v1
	v1 := s.router.Group("/api/v1")
	{
		// 인증 (인증 불필요)
		auth := v1.Group("/auth")
		{
			auth.GET("/providers", s.authHandler.GetProviders)
			auth.POST("/login", s.authHandler.Login)
			auth.GET("/callback", s.authHandler.Callback)
		}

		// Agent 내부 API (인증 불필요 - 클러스터 내부 통신)
		internal := v1.Group("/workflows")
		{
			internal.POST("/:id/executions/:executionId/result", s.workflowHandler.ReceiveExecutionResult)
		}

		// Job 결과 콜백 API (Kubernetes Job Pod에서 호출)
		internalJob := v1.Group("/internal")
		{
			internalJob.POST("/job-result", s.workflowHandler.HandleJobResultCallback)
		}

		// 파이프라인 체크포인트 내부 API (Agent에서 호출)
		internalPipelines := v1.Group("/pipelines")
		{
			internalPipelines.POST("/:id/checkpoints", s.checkpointHandler.UpdateCheckpoint)
		}

		// 에이전트 내부 API (인증 불필요 - 클러스터 내부 통신)
		internalAgents := v1.Group("/agents")
		{
			internalAgents.POST("/register", s.agentHandler.RegisterAgent)
			internalAgents.POST("/:id/heartbeat", s.agentHandler.Heartbeat)
		}

		// 플러그인 등록 내부 API (Helm hook에서 호출 - 클러스터 내부 통신)
		internalPlugins := v1.Group("/plugins")
		{
			internalPlugins.POST("", s.pluginHandler.CreatePlugin)
		}

		// 인증 필요한 라우트
		authenticated := v1.Group("")
		authenticated.Use(middleware.AuthMiddleware(s.jwtSecret))
		{
			// 사용자
			authenticated.GET("/auth/me", s.authHandler.GetCurrentUser)
			authenticated.GET("/auth/profile", s.authHandler.GetUserProfile)
			authenticated.POST("/auth/logout", s.authHandler.Logout)

			// 파이프라인 (개별 실행 제어 없음 - 워크플로우 단위로만 제어)
			pipelines := authenticated.Group("/pipelines")
			{
				pipelines.GET("", s.pipelineHandler.List)
				pipelines.POST("", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.pipelineHandler.Create)
				pipelines.GET("/:id", s.pipelineHandler.Get)
				pipelines.PUT("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.pipelineHandler.Update)
				pipelines.DELETE("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.pipelineHandler.Delete)
				pipelines.GET("/:id/status", s.pipelineHandler.GetStatus)
				pipelines.GET("/:id/history", s.pipelineHandler.GetHistory)
				pipelines.GET("/:id/metrics", s.pipelineHandler.GetMetrics)
				// 그래프 (시각화)
				pipelines.GET("/:id/graph", s.graphHandler.GetPipelineGraph)
				pipelines.PUT("/:id/graph", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.graphHandler.UpdatePipelineGraph)
				pipelines.GET("/:id/actor-metrics", s.graphHandler.GetActorMetrics)
				// 체크포인트 (소스 오프셋 관리)
				pipelines.GET("/:id/checkpoints", s.checkpointHandler.ListCheckpoints)
				pipelines.PUT("/:id/checkpoints/reset", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.checkpointHandler.ResetCheckpoints)
				pipelines.DELETE("/:id/checkpoints", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.checkpointHandler.DeleteCheckpoints)
			}

			// 워크플로우
			workflows := authenticated.Group("/workflows")
			{
				workflows.GET("", s.workflowHandler.ListWorkflows)
				workflows.POST("", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.CreateWorkflow)
				// YAML import/export (템플릿·GitOps·AI 제어용). import는 :id 라우트보다 먼저 등록.
				workflows.POST("/import", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.ImportWorkflowYAML)
				workflows.GET("/:id", s.workflowHandler.GetWorkflow)
				workflows.GET("/:id/yaml", s.workflowHandler.ExportWorkflowYAML)
				workflows.PUT("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.UpdateWorkflow)
				workflows.DELETE("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.workflowHandler.DeleteWorkflow)
				workflows.POST("/:id/start", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.StartWorkflow)
				workflows.POST("/:id/stop", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.StopWorkflow)
				workflows.POST("/:id/pause", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.PauseWorkflow)
				workflows.POST("/:id/resume", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.ResumeWorkflow)
				workflows.GET("/:id/executions", s.workflowHandler.GetWorkflowExecutions)
				workflows.GET("/:id/executions/:execId", s.workflowHandler.GetWorkflowExecution)
				workflows.GET("/:id/executions/:execId/monitoring", s.workflowHandler.GetExecutionMonitoring)
				workflows.POST("/:id/pipelines", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.AddPipelineToWorkflow)
				workflows.DELETE("/:id/pipelines/:pipelineId", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.workflowHandler.RemovePipelineFromWorkflow)
				// 스케줄 관련 (워크플로우 하위)
				workflows.GET("/:id/schedule", s.scheduleHandler.GetSchedule)
				workflows.PUT("/:id/schedule", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.scheduleHandler.UpdateSchedule)
				workflows.POST("/:id/schedule/enable", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.scheduleHandler.EnableSchedule)
				workflows.POST("/:id/schedule/disable", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.scheduleHandler.DisableSchedule)
				workflows.POST("/:id/trigger", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.scheduleHandler.TriggerNow)
				// 체크포인트 (워크플로우 내 모든 파이프라인)
				workflows.GET("/:id/checkpoints", s.checkpointHandler.GetCheckpointsByWorkflow)
			}

			// 통계
			stats := authenticated.Group("/stats")
			{
				stats.GET("/pipelines/:id", s.statsHandler.GetPipelineStats)
				stats.GET("/workflows/:id", s.statsHandler.GetWorkflowStats)
				stats.GET("/executions/:id", s.statsHandler.GetExecutionStats)
			}

			// 스케줄 목록 (전체)
			schedules := authenticated.Group("/schedules")
			{
				schedules.GET("", s.scheduleHandler.ListSchedules)
			}

			// 데이터 유형
			dataTypes := authenticated.Group("/data-types")
			{
				dataTypes.GET("", s.dataTypeHandler.ListDataTypes)
				dataTypes.GET("/categories", s.dataTypeHandler.GetCategories)
				dataTypes.POST("", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.dataTypeHandler.CreateDataType)
				dataTypes.GET("/:id", s.dataTypeHandler.GetDataType)
				dataTypes.PUT("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.dataTypeHandler.UpdateDataType)
				dataTypes.DELETE("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.dataTypeHandler.DeleteDataType)
				// 사전작업
				dataTypes.POST("/:id/preworks", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.dataTypeHandler.AddPrework)
				dataTypes.DELETE("/:id/preworks/:preworkId", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.dataTypeHandler.DeletePrework)
				dataTypes.POST("/:id/preworks/:preworkId/execute", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.dataTypeHandler.ExecutePrework)
			}

			// 삭제 전략 프리셋
			authenticated.GET("/delete-strategy-presets", s.dataTypeHandler.ListDeleteStrategyPresets)

			// 사용자 관리 (관리자 전용)
			users := authenticated.Group("/users")
			{
				users.GET("", s.userHandler.ListUsers)
				users.GET("/search", s.userHandler.SearchUsers) // 자동완성용 검색 (모든 인증된 사용자 접근 가능)
				users.GET("/:id", s.userHandler.GetUser)
				users.PUT("/:id/role", s.userHandler.UpdateUserRole)
			}

			// 권한 관리 (관리자 전용)
			permissions := authenticated.Group("/permissions")
			{
				permissions.GET("", s.userHandler.ListPermissions)
				permissions.POST("", s.userHandler.CreatePermission)
				permissions.DELETE("/:id", s.userHandler.DeletePermission)
			}

			// 역할 목록
			authenticated.GET("/roles", s.userHandler.GetRoles)

			// 프로젝트
			projects := authenticated.Group("/projects")
			{
				projects.GET("", s.projectHandler.ListProjects)
				projects.POST("", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.projectHandler.CreateProject)
				projects.GET("/:id", s.projectHandler.GetProject)
				projects.PUT("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.projectHandler.UpdateProject)
				projects.DELETE("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.projectHandler.DeleteProject)
				projects.GET("/:id/workflows", s.projectHandler.GetProjectWorkflows)
				projects.GET("/:id/hierarchy", s.projectHandler.GetProjectHierarchy)
				projects.GET("/:id/data-types", s.projectHandler.GetProjectDataTypes)
			}

			// 에이전트
			agents := authenticated.Group("/agents")
			{
				agents.GET("", s.agentHandler.ListAgents)
				agents.GET("/:id", s.agentHandler.GetAgent)
			}

			// 클러스터
			clusters := authenticated.Group("/clusters")
			{
				clusters.GET("", s.clusterHandler.ListClusters)
				clusters.POST("", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.clusterHandler.CreateCluster)
				clusters.GET("/:id", s.clusterHandler.GetCluster)
				clusters.PUT("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.clusterHandler.UpdateCluster)
				clusters.DELETE("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.clusterHandler.DeleteCluster)
				clusters.GET("/:id/agents", s.clusterHandler.GetClusterAgents)
				// Agent 스케일링 및 배포 설정
				clusters.POST("/:id/scale", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.clusterHandler.ScaleAgents)
				clusters.GET("/:id/agent-config", s.clusterHandler.GetAgentConfig)
				clusters.PUT("/:id/agent-config", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.clusterHandler.UpdateAgentConfig)
			}

			// 유틸리티 (연결 테스트)
			utils := authenticated.Group("/utils")
			{
				utils.POST("/test-db-connection", s.utilsHandler.TestDBConnection)
				utils.POST("/test-elasticsearch", s.utilsHandler.TestElasticsearch)
				utils.POST("/test-kafka", s.utilsHandler.TestKafka)
				utils.POST("/test-mongodb", s.utilsHandler.TestMongoDB)
				utils.POST("/test-s3", s.utilsHandler.TestS3)
				utils.POST("/test-rest-api", s.utilsHandler.TestRESTAPI)
			}

			// Stage 스키마 (GUI 자동 생성용)
			stages := authenticated.Group("/stages")
			{
				stages.GET("", s.stageHandler.ListAllStages)               // 빌트인 + 플러그인 Stage 목록
				stages.GET("/:type/schema", s.stageHandler.GetStageSchema) // 개별 Stage 스키마 (빌트인 + 플러그인)
				stages.GET("/schemas", s.stageHandler.GetAllSchemas)       // 빌트인만 (하위 호환성)
				stages.GET("/schemas/:type", s.stageHandler.GetSchema)     // 빌트인만 (하위 호환성)
				stages.GET("/categories", s.stageHandler.GetCategories)
				stages.GET("/categories/:category/schemas", s.stageHandler.GetSchemasByCategory)
				stages.GET("/field-types", s.stageHandler.GetFieldTypes)
			}

			// 플러그인 (커스텀 Stage 관리)
			plugins := authenticated.Group("/plugins")
			{
				plugins.GET("", s.pluginHandler.ListPlugins)
				plugins.POST("/test-script", s.pluginHandler.TestScript)
				plugins.POST("/test-native", s.pluginHandler.TestNativePlugin)
				plugins.GET("/revisions/:revisionId", s.pluginHandler.GetRevision)
				plugins.GET("/:name", s.pluginHandler.GetPlugin)
				plugins.GET("/:name/revisions", s.pluginHandler.ListRevisions)
				plugins.PUT("/:name", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.pluginHandler.UpdatePlugin)
				plugins.DELETE("/:name", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.pluginHandler.DeletePlugin)
			}

			// Runner (Native Plugin 빌드/버전 관리)
			runner := authenticated.Group("/runner")
			{
				runner.GET("/versions", s.runnerHandler.ListVersions)
				runner.GET("/versions/:id", s.runnerHandler.GetVersion)
				runner.GET("/status", s.runnerHandler.CheckStatus)
				runner.POST("/build", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.runnerHandler.StartBuild)
				runner.POST("/resolve", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.runnerHandler.ResolveImage)
				runner.POST("/rebuild/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.runnerHandler.RebuildVersion)
			}

			// LSP (gopls 자동완성)
			lspGroup := authenticated.Group("/lsp")
			{
				lspGroup.GET("/go", s.lspHandler.HandleLSP)
				lspGroup.POST("/sync", s.lspHandler.SyncSource)
			}

			// 미리보기
			previewGroup := authenticated.Group("/preview")
			{
				previewGroup.POST("/stage", s.previewHandler.PreviewStage)
				previewGroup.POST("/pipeline", s.previewHandler.PreviewPipeline)
			}

			// 파이프라인 링크 (부모-자식 연결)
			pipelineLinks := authenticated.Group("/pipeline-links")
			{
				pipelineLinks.GET("", s.pipelineLinkHandler.ListAllLinks)
				pipelineLinks.POST("", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.pipelineLinkHandler.CreateLink)
				pipelineLinks.GET("/:parent_id/:child_id", s.pipelineLinkHandler.GetLink)
				pipelineLinks.DELETE("/:parent_id/:child_id", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.pipelineLinkHandler.DeleteLink)
				pipelineLinks.GET("/parent/:parent_id", s.pipelineLinkHandler.GetLinksByParent)
				pipelineLinks.GET("/child/:child_id", s.pipelineLinkHandler.GetLinksByChild)
				pipelineLinks.GET("/workflow/:workflow_id", s.pipelineLinkHandler.GetLinksByWorkflow)
			}
		}
	}
}

// IndexInfo 인덱스 페이지 정보
type IndexInfo struct {
	Service      string            `json:"service"`
	Version      string            `json:"version"`
	Uptime       string            `json:"uptime"`
	UptimeSec    float64           `json:"uptime_seconds"`
	StartTime    string            `json:"start_time"`
	GoVersion    string            `json:"go_version"`
	NumGoroutine int               `json:"num_goroutine"`
	MemoryMB     float64           `json:"memory_mb"`
	Endpoints    map[string]string `json:"endpoints"`
}

// index 서비스 정보 페이지
func (s *Server) index(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Since(s.startTime)

	info := IndexInfo{
		Service:      "Conduix Control Plane",
		Version:      s.version,
		Uptime:       uptime.Round(time.Second).String(),
		UptimeSec:    uptime.Seconds(),
		StartTime:    s.startTime.Format(time.RFC3339),
		GoVersion:    runtime.Version(),
		NumGoroutine: runtime.NumGoroutine(),
		MemoryMB:     float64(m.Alloc) / 1024 / 1024,
		Endpoints: map[string]string{
			"health":     "/health",
			"ready":      "/ready",
			"api":        "/api/v1",
			"auth":       "/api/v1/auth/providers",
			"pipelines":  "/api/v1/pipelines",
			"workflows":  "/api/v1/workflows",
			"agents":     "/api/v1/agents",
			"clusters":   "/api/v1/clusters",
			"data-types": "/api/v1/data-types",
			"projects":   "/api/v1/projects",
			"plugins":    "/api/v1/plugins",
			"stages":     "/api/v1/stages",
		},
	}

	c.JSON(200, info)
}

// health 헬스체크
func (s *Server) health(c *gin.Context) {
	c.JSON(200, types.HealthStatus{
		Status: "healthy",
	})
}

// ready 준비 상태 확인
func (s *Server) ready(c *gin.Context) {
	if err := s.db.Health(); err != nil {
		c.JSON(503, types.HealthStatus{
			Status: "not ready",
			Checks: map[string]string{
				"database": err.Error(),
			},
		})
		return
	}

	c.JSON(200, types.HealthStatus{
		Status: "ready",
		Checks: map[string]string{
			"database": "ok",
		},
	})
}

// Run 서버 실행
func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

// Router 라우터 반환
func (s *Server) Router() *gin.Engine {
	return s.router
}

// RegisterOAuthProviders OAuth2 프로바이더들 일괄 등록
func (s *Server) RegisterOAuthProviders(oauthConfig *config.OAuthConfig) {
	s.authHandler.RegisterProvidersFromConfig(oauthConfig)
}
