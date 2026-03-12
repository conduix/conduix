package lsp

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// goplsBinaryName gopls 바이너리 이름 (PATH에서 찾음)
const goplsBinaryName = "gopls"

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // CORS는 미들웨어에서 처리
	},
}

// LSPProxy WebSocket ↔ gopls stdio 중계 프록시
type LSPProxy struct {
	workspaceManager *WorkspaceManager
	logger           *slog.Logger
}

// NewLSPProxy LSPProxy 생성
func NewLSPProxy(wm *WorkspaceManager) *LSPProxy {
	return &LSPProxy{
		workspaceManager: wm,
		logger:           slog.Default().With("component", "lsp-proxy"),
	}
}

// HandleWebSocket WebSocket 연결 처리 (gin HandlerFunc용)
// Query parameter: session_id (필수)
func (p *LSPProxy) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}

	// workspace 가져오기/생성
	ws, err := p.workspaceManager.GetOrCreate(sessionID)
	if err != nil {
		p.logger.Error("Failed to create workspace", "session_id", sessionID, "error", err)
		http.Error(w, "workspace creation failed", http.StatusInternalServerError)
		return
	}

	// WebSocket upgrade
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		p.logger.Error("WebSocket upgrade failed", "session_id", sessionID, "error", err)
		return
	}
	defer func() { _ = conn.Close() }()

	p.logger.Info("LSP session started", "session_id", sessionID, "workspace", ws.Dir)

	// gopls 프로세스 시작
	cmd := exec.Command(goplsBinaryName, "-rpc.trace")
	cmd.Dir = ws.Dir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.logger.Error("Failed to get gopls stdin", "error", err)
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.logger.Error("Failed to get gopls stdout", "error", err)
		return
	}

	if err := cmd.Start(); err != nil {
		p.logger.Error("Failed to start gopls", "error", err)
		p.sendError(conn, fmt.Sprintf("gopls not found or failed to start: %v", err))
		return
	}

	p.logger.Info("gopls started", "session_id", sessionID, "pid", cmd.Process.Pid)

	var wg sync.WaitGroup
	done := make(chan struct{})

	// WebSocket → gopls stdin (LSP request)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stdin.Close()
		p.wsToStdin(conn, stdin, done)
	}()

	// gopls stdout → WebSocket (LSP response)
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.stdoutToWs(stdout, conn, done)
	}()

	wg.Wait()

	// gopls 프로세스 종료
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()

	p.logger.Info("LSP session ended", "session_id", sessionID)
}

// wsToStdin WebSocket에서 읽은 LSP 메시지를 gopls stdin에 전달
func (p *LSPProxy) wsToStdin(conn *websocket.Conn, stdin io.WriteCloser, done chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				p.logger.Debug("WebSocket read error", "error", err)
			}
			close(done)
			return
		}

		// LSP Content-Length 헤더 추가 후 전송
		header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(message))
		if _, err := stdin.Write([]byte(header)); err != nil {
			p.logger.Debug("stdin write header error", "error", err)
			return
		}
		if _, err := stdin.Write(message); err != nil {
			p.logger.Debug("stdin write body error", "error", err)
			return
		}
	}
}

// stdoutToWs gopls stdout에서 LSP 메시지를 읽어 WebSocket으로 전달
func (p *LSPProxy) stdoutToWs(stdout io.Reader, conn *websocket.Conn, done chan struct{}) {
	reader := bufio.NewReader(stdout)

	for {
		select {
		case <-done:
			return
		default:
		}

		// Content-Length 헤더 읽기
		contentLength, err := readContentLength(reader)
		if err != nil {
			if err != io.EOF {
				p.logger.Debug("stdout read header error", "error", err)
			}
			return
		}

		// 본문 읽기
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, body); err != nil {
			p.logger.Debug("stdout read body error", "error", err)
			return
		}

		// WebSocket으로 전송
		if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
			p.logger.Debug("WebSocket write error", "error", err)
			return
		}
	}
}

// readContentLength LSP 헤더에서 Content-Length 파싱
func readContentLength(reader *bufio.Reader) (int, error) {
	var contentLength int

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0, err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			// 헤더 끝
			break
		}

		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, err = strconv.Atoi(val)
			if err != nil {
				return 0, fmt.Errorf("invalid Content-Length: %s", val)
			}
		}
	}

	if contentLength == 0 {
		return 0, fmt.Errorf("missing Content-Length header")
	}

	return contentLength, nil
}

// sendError WebSocket으로 에러 메시지 전송
func (p *LSPProxy) sendError(conn *websocket.Conn, msg string) {
	errJSON := fmt.Sprintf(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"%s"}}`, msg)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(errJSON))
}
