package api

import (
	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/handlers"
	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/config"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/shared/types"
)

// Server API 서버
type Server struct {
	router           *gin.Engine
	db               *database.DB
	redisService     *services.RedisService
	schedulerService *services.SchedulerService
	jwtSecret        []byte
	pipelineHandler  *handlers.PipelineHandler
	authHandler      *handlers.AuthHandler
	workflowHandler  *handlers.WorkflowHandler
	statsHandler     *handlers.StatsHandler
	scheduleHandler  *handlers.ScheduleHandler
	graphHandler     *handlers.GraphHandler
	dataTypeHandler  *handlers.DataTypeHandler
	userHandler      *handlers.UserHandler
	projectHandler   *handlers.ProjectHandler
}

// NewServer 새 서버 생성
func NewServer(db *database.DB, redisService *services.RedisService, schedulerService *services.SchedulerService, jwtSecret string, usersConfig *config.UsersConfig, frontendURL string) *Server {
	gin.SetMode(gin.ReleaseMode)

	s := &Server{
		router:           gin.New(),
		db:               db,
		redisService:     redisService,
		schedulerService: schedulerService,
		jwtSecret:        []byte(jwtSecret),
		pipelineHandler:  handlers.NewPipelineHandler(db, redisService),
		authHandler:      handlers.NewAuthHandler(db, jwtSecret, usersConfig, frontendURL),
		workflowHandler:  handlers.NewWorkflowHandler(db),
		statsHandler:     handlers.NewStatsHandler(db),
		scheduleHandler:  handlers.NewScheduleHandler(db, schedulerService),
		graphHandler:     handlers.NewGraphHandler(db, redisService),
		dataTypeHandler:  handlers.NewDataTypeHandler(db),
		userHandler:      handlers.NewUserHandler(db),
		projectHandler:   handlers.NewProjectHandler(db),
	}

	s.setupRoutes()
	return s
}

// setupRoutes 라우트 설정
func (s *Server) setupRoutes() {
	// 미들웨어
	s.router.Use(gin.Recovery())
	s.router.Use(middleware.CORSMiddleware())
	s.router.Use(middleware.RequestIDMiddleware())

	// 헬스체크 (인증 불필요)
	s.router.GET("/health", s.health)
	s.router.GET("/ready", s.ready)

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
			}

			// 워크플로우
			workflows := authenticated.Group("/workflows")
			{
				workflows.GET("", s.workflowHandler.ListWorkflows)
				workflows.POST("", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.CreateWorkflow)
				workflows.GET("/:id", s.workflowHandler.GetWorkflow)
				workflows.PUT("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.UpdateWorkflow)
				workflows.DELETE("/:id", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.workflowHandler.DeleteWorkflow)
				workflows.POST("/:id/start", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.StartWorkflow)
				workflows.POST("/:id/stop", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.StopWorkflow)
				workflows.POST("/:id/pause", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.PauseWorkflow)
				workflows.POST("/:id/resume", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.ResumeWorkflow)
				workflows.GET("/:id/executions", s.workflowHandler.GetWorkflowExecutions)
				workflows.POST("/:id/pipelines", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.workflowHandler.AddPipelineToWorkflow)
				workflows.DELETE("/:id/pipelines/:pipelineId", middleware.RoleMiddleware(string(types.UserRoleAdmin)), s.workflowHandler.RemovePipelineFromWorkflow)
				// 스케줄 관련 (워크플로우 하위)
				workflows.GET("/:id/schedule", s.scheduleHandler.GetSchedule)
				workflows.PUT("/:id/schedule", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.scheduleHandler.UpdateSchedule)
				workflows.POST("/:id/schedule/enable", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.scheduleHandler.EnableSchedule)
				workflows.POST("/:id/schedule/disable", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.scheduleHandler.DisableSchedule)
				workflows.POST("/:id/trigger", middleware.RoleMiddleware(string(types.UserRoleAdmin), string(types.UserRoleOperator)), s.scheduleHandler.TriggerNow)
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

			// 에이전트 (TODO: 구현)
			// agents := authenticated.Group("/agents")
		}
	}
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

// RegisterGitHubOAuth2 GitHub OAuth2 프로바이더 등록
func (s *Server) RegisterGitHubOAuth2(clientID, clientSecret, redirectURL string) {
	s.authHandler.RegisterGitHubProvider(clientID, clientSecret, redirectURL)
}

// RegisterGoogleOAuth2 Google OAuth2 프로바이더 등록
func (s *Server) RegisterGoogleOAuth2(clientID, clientSecret, redirectURL string) {
	s.authHandler.RegisterGoogleProvider(clientID, clientSecret, redirectURL)
}
