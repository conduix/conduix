// Package output Dead Letter Queue (DLQ) Output 구현
package output

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/source"
	"github.com/conduix/conduix/shared/types"
)

// DLQOutput Dead Letter Queue 출력 인터페이스
type DLQOutput interface {
	Output
	WriteViolation(ctx context.Context, record source.Record, violations []types.ContractViolation) error
}

// FileDLQOutput 파일 기반 DLQ
type FileDLQOutput struct {
	path    string
	format  string // json, jsonl
	file    *os.File
	mu      sync.Mutex
	stats   OutputStats
	encoder *json.Encoder

	// Rotation 설정
	maxSizeBytes int64
	maxAgeDays   int
	maxBackups   int
	currentSize  int64
	createdAt    time.Time
}

// NewFileDLQOutput 파일 DLQ 생성
func NewFileDLQOutput(cfg types.DLQConfig) (*FileDLQOutput, error) {
	path := cfg.Path
	if path == "" {
		path = "dlq.jsonl"
	}

	format := cfg.Format
	if format == "" {
		format = "jsonl"
	}

	// 디렉토리 생성
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create DLQ directory: %w", err)
		}
	}

	// Rotation 기본값
	maxSizeMB := cfg.MaxSizeMB
	if maxSizeMB <= 0 {
		maxSizeMB = 100 // 기본 100MB
	}

	maxAgeDays := cfg.MaxAgeDays
	if maxAgeDays <= 0 {
		maxAgeDays = 7 // 기본 7일
	}

	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 5 // 기본 5개
	}

	return &FileDLQOutput{
		path:         path,
		format:       format,
		maxSizeBytes: int64(maxSizeMB) * 1024 * 1024,
		maxAgeDays:   maxAgeDays,
		maxBackups:   maxBackups,
	}, nil
}

func (o *FileDLQOutput) Name() string { return "file_dlq" }

func (o *FileDLQOutput) Open(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// 오래된 백업 파일 정리
	o.cleanupOldBackups()

	file, err := os.OpenFile(o.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open DLQ file: %w", err)
	}

	// 현재 파일 크기 확인
	info, err := file.Stat()
	if err == nil {
		o.currentSize = info.Size()
		o.createdAt = info.ModTime()
	} else {
		o.currentSize = 0
		o.createdAt = time.Now()
	}

	o.file = file
	o.encoder = json.NewEncoder(file)

	log.Printf("[dlq] File DLQ opened: %s (size: %d bytes, max: %d MB, retention: %d days)",
		o.path, o.currentSize, o.maxSizeBytes/(1024*1024), o.maxAgeDays)
	return nil
}

func (o *FileDLQOutput) Write(ctx context.Context, record source.Record) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.file == nil {
		return fmt.Errorf("DLQ file not opened")
	}

	atomic.AddInt64(&o.stats.TotalRecords, 1)

	// Rotation 체크
	if o.needsRotation() {
		if err := o.rotateFile(); err != nil {
			log.Printf("[dlq] Failed to rotate file: %v", err)
		}
	}

	dlqEntry := map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
		"data":      record.Data,
		"metadata":  record.Metadata,
	}

	data, err := json.Marshal(dlqEntry)
	if err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, 1)
		return fmt.Errorf("failed to marshal DLQ entry: %w", err)
	}

	// 파일에 쓰기 (개행 포함)
	data = append(data, '\n')
	n, err := o.file.Write(data)
	if err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, 1)
		return fmt.Errorf("failed to write to DLQ: %w", err)
	}

	o.currentSize += int64(n)
	atomic.AddInt64(&o.stats.SuccessRecords, 1)
	o.stats.LastWriteTime = time.Now()
	return nil
}

func (o *FileDLQOutput) WriteViolation(ctx context.Context, record source.Record, violations []types.ContractViolation) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.file == nil {
		return fmt.Errorf("DLQ file not opened")
	}

	atomic.AddInt64(&o.stats.TotalRecords, 1)

	// Rotation 체크
	if o.needsRotation() {
		if err := o.rotateFile(); err != nil {
			log.Printf("[dlq] Failed to rotate file: %v", err)
		}
	}

	dlqEntry := types.DLQRecord{
		ID:           fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp:    time.Now(),
		Source:       record.Metadata.Source,
		Violations:   violations,
		OriginalData: record.Data,
		Metadata: map[string]string{
			"origin": record.Metadata.Origin,
		},
	}

	data, err := json.Marshal(dlqEntry)
	if err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, 1)
		return fmt.Errorf("failed to marshal DLQ entry: %w", err)
	}

	data = append(data, '\n')
	n, err := o.file.Write(data)
	if err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, 1)
		return fmt.Errorf("failed to write violation to DLQ: %w", err)
	}

	o.currentSize += int64(n)
	atomic.AddInt64(&o.stats.SuccessRecords, 1)
	o.stats.LastWriteTime = time.Now()
	return nil
}

func (o *FileDLQOutput) Flush(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.file != nil {
		return o.file.Sync()
	}
	return nil
}

func (o *FileDLQOutput) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.file != nil {
		if err := o.file.Close(); err != nil {
			return err
		}
		o.file = nil
		log.Printf("[dlq] File DLQ closed. Total: %d, Success: %d, Errors: %d",
			o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords)
	}
	return nil
}

func (o *FileDLQOutput) Stats() OutputStats { return o.stats }

// needsRotation 파일 rotation 필요 여부 확인
func (o *FileDLQOutput) needsRotation() bool {
	// 크기 기반 rotation
	if o.maxSizeBytes > 0 && o.currentSize >= o.maxSizeBytes {
		return true
	}

	// 시간 기반 rotation (1일 이상 지난 파일)
	if o.maxAgeDays > 0 && time.Since(o.createdAt) > 24*time.Hour {
		return true
	}

	return false
}

// rotateFile 파일 rotation 수행
func (o *FileDLQOutput) rotateFile() error {
	if o.file == nil {
		return nil
	}

	// 현재 파일 닫기
	if err := o.file.Close(); err != nil {
		return fmt.Errorf("failed to close current file: %w", err)
	}

	// 백업 파일명 생성 (타임스탬프 포함)
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.%s", o.path, timestamp)

	// 현재 파일을 백업으로 이동
	if err := os.Rename(o.path, backupPath); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	log.Printf("[dlq] Rotated DLQ file: %s -> %s", o.path, backupPath)

	// 새 파일 열기
	file, err := os.OpenFile(o.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open new file: %w", err)
	}

	o.file = file
	o.encoder = json.NewEncoder(file)
	o.currentSize = 0
	o.createdAt = time.Now()

	// 오래된 백업 정리
	o.cleanupOldBackups()

	return nil
}

// cleanupOldBackups 오래된 백업 파일 정리
func (o *FileDLQOutput) cleanupOldBackups() {
	dir := filepath.Dir(o.path)
	base := filepath.Base(o.path)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// 백업 파일 목록 수집
	var backups []string
	cutoff := time.Now().AddDate(0, 0, -o.maxAgeDays)

	for _, entry := range entries {
		name := entry.Name()
		// 백업 파일 패턴 매칭 (원본파일명.타임스탬프)
		if len(name) > len(base)+1 && name[:len(base)+1] == base+"." {
			fullPath := filepath.Join(dir, name)

			// 시간 기반 삭제
			info, err := entry.Info()
			if err == nil && info.ModTime().Before(cutoff) {
				if err := os.Remove(fullPath); err != nil {
					log.Printf("[dlq] Failed to remove old backup %s: %v", fullPath, err)
				} else {
					log.Printf("[dlq] Removed old backup: %s", fullPath)
				}
				continue
			}

			backups = append(backups, fullPath)
		}
	}

	// 개수 기반 삭제 (가장 오래된 것부터)
	if len(backups) > o.maxBackups {
		for i := 0; i < len(backups)-o.maxBackups; i++ {
			if err := os.Remove(backups[i]); err != nil {
				log.Printf("[dlq] Failed to remove excess backup %s: %v", backups[i], err)
			} else {
				log.Printf("[dlq] Removed excess backup: %s", backups[i])
			}
		}
	}
}

// NewDLQOutput DLQ 타입에 따라 적절한 출력 생성
func NewDLQOutput(cfg types.DLQConfig) (DLQOutput, error) {
	switch cfg.Type {
	case "file", "":
		return NewFileDLQOutput(cfg)
	default:
		return nil, fmt.Errorf("unsupported DLQ type: %s", cfg.Type)
	}
}
