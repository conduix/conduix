package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/lsp"
)

// LSPHandler gopls LSP 프록시 핸들러
type LSPHandler struct {
	proxy            *lsp.LSPProxy
	workspaceManager *lsp.WorkspaceManager
}

// NewLSPHandler LSPHandler 생성
// sdkModPath: plugin-sdk 모듈의 절대 경로 (gopls가 참조할 replace 경로)
func NewLSPHandler(sdkModPath string) *LSPHandler {
	wm := lsp.NewWorkspaceManager(sdkModPath)
	return &LSPHandler{
		proxy:            lsp.NewLSPProxy(wm),
		workspaceManager: wm,
	}
}

// HandleLSP WebSocket LSP 프록시 엔드포인트
// GET /api/v1/lsp/go?session_id=xxx → WebSocket upgrade → gopls 연결
func (h *LSPHandler) HandleLSP(c *gin.Context) {
	h.proxy.HandleWebSocket(c.Writer, c.Request)
}

// SyncSourceRequest 소스 동기화 요청
type SyncSourceRequest struct {
	SessionID  string `json:"session_id" binding:"required"`
	SourceCode string `json:"source_code"`
	GoMod      string `json:"go_mod"`
}

// SyncSource POST /api/v1/lsp/sync — 사용자 코드를 gopls workspace에 동기화
// Monaco Editor에서 코드 변경 시 호출하여 gopls가 최신 코드를 인식하도록 함
func (h *LSPHandler) SyncSource(c *gin.Context) {
	var req SyncSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.workspaceManager.SyncSource(req.SessionID, req.SourceCode, req.GoMod); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
