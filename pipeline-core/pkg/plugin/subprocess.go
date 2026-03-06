package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// SubprocessStage 외부 프로세스로 실행되는 Stage
// stdin/stdout JSON lines 프로토콜로 통신
type SubprocessStage struct {
	BaseStage
	command string
	args    []string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
}

// SubprocessConfig subprocess Stage 설정
type SubprocessConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// NewSubprocessStage subprocess Stage 생성
func NewSubprocessStage(stageType, command string, args ...string) *SubprocessStage {
	return &SubprocessStage{
		BaseStage: BaseStage{StageType: stageType},
		command:   command,
		args:      args,
	}
}

// Init 외부 프로세스 시작
func (s *SubprocessStage) Init(configData json.RawMessage) error {
	// config에서 command/args 오버라이드 가능
	if len(configData) > 0 {
		var cfg SubprocessConfig
		if err := json.Unmarshal(configData, &cfg); err == nil {
			if cfg.Command != "" {
				s.command = cfg.Command
			}
			if len(cfg.Args) > 0 {
				s.args = cfg.Args
			}
		}
	}

	cmd := exec.Command(s.command, s.args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start subprocess: %w", err)
	}

	s.cmd = cmd
	s.stdin = stdin
	s.stdout = stdout
	s.scanner = bufio.NewScanner(stdout)
	s.scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	return nil
}

// Process 레코드를 subprocess로 전달하고 결과 수신
func (s *SubprocessStage) Process(_ context.Context, record *Record) ([]*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// JSON line으로 전송
	data, err := json.Marshal(record.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal record: %w", err)
	}

	if _, err := s.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write to subprocess: %w", err)
	}

	// 결과 수신 (1 line)
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to read from subprocess: %w", err)
		}
		return nil, fmt.Errorf("subprocess closed stdout")
	}

	var resultData map[string]any
	if err := json.Unmarshal(s.scanner.Bytes(), &resultData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal subprocess output: %w", err)
	}

	return []*Record{{Data: resultData, Metadata: record.Metadata}}, nil
}

// Close 외부 프로세스 종료
func (s *SubprocessStage) Close() error {
	if s.stdin != nil {
		s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Wait()
	}
	return nil
}
