package source

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// FileSource 파일 소스
type FileSource struct {
	paths     []string
	format    string // json, csv, lines
	csvHeader bool   //nolint:unused
}

// NewFileSource 파일 소스 생성
func NewFileSource(cfg config.SourceV2) (*FileSource, error) {
	var paths []string

	if cfg.Path != "" {
		paths = append(paths, cfg.Path)
	}
	paths = append(paths, cfg.Paths...)

	// Glob 패턴 확장
	var expandedPaths []string
	for _, p := range paths {
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %s: %w", p, err)
		}
		if len(matches) == 0 {
			// 패턴이 아닌 경우 그대로 사용
			expandedPaths = append(expandedPaths, p)
		} else {
			expandedPaths = append(expandedPaths, matches...)
		}
	}

	format := cfg.Format
	if format == "" {
		format = "json"
	}

	return &FileSource{
		paths:  expandedPaths,
		format: format,
	}, nil
}

func (s *FileSource) Name() string {
	return "file"
}

func (s *FileSource) Open(ctx context.Context) error {
	// 파일 존재 여부 확인
	for _, path := range s.paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", path)
		}
	}
	return nil
}

func (s *FileSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		for _, path := range s.paths {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if err := s.readFile(ctx, path, records); err != nil {
				errs <- fmt.Errorf("error reading %s: %w", path, err)
				return
			}
		}
	}()

	return records, errs
}

func (s *FileSource) readFile(ctx context.Context, path string, records chan<- Record) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	switch s.format {
	case "json":
		return s.readJSON(ctx, file, path, records)
	case "csv":
		return s.readCSV(ctx, file, path, records)
	case "lines":
		return s.readLines(ctx, file, path, records)
	default:
		return fmt.Errorf("unsupported format: %s", s.format)
	}
}

func (s *FileSource) readJSON(ctx context.Context, file *os.File, path string, records chan<- Record) error {
	decoder := json.NewDecoder(file)

	// 배열인지 확인
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	if delim, ok := token.(json.Delim); ok && delim == '[' {
		// JSON 배열
		for decoder.More() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			var data map[string]any
			if err := decoder.Decode(&data); err != nil {
				return err
			}

			records <- Record{
				Data: data,
				Metadata: Metadata{
					Source:    "file",
					Origin:    path,
					Timestamp: time.Now().UnixMilli(),
				},
			}
		}
	} else {
		// NDJSON (줄별 JSON)
		_, _ = file.Seek(0, 0)
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			var data map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &data); err != nil {
				continue // 잘못된 줄 스킵
			}

			records <- Record{
				Data: data,
				Metadata: Metadata{
					Source:    "file",
					Origin:    path,
					Timestamp: time.Now().UnixMilli(),
				},
			}
		}
	}

	return nil
}

func (s *FileSource) readCSV(ctx context.Context, file *os.File, path string, records chan<- Record) error {
	reader := csv.NewReader(file)

	// 헤더 읽기
	headers, err := reader.Read()
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		data := make(map[string]any)
		for i, header := range headers {
			if i < len(row) {
				data[header] = row[i]
			}
		}

		records <- Record{
			Data: data,
			Metadata: Metadata{
				Source:    "file",
				Origin:    path,
				Timestamp: time.Now().UnixMilli(),
			},
		}
	}

	return nil
}

func (s *FileSource) readLines(ctx context.Context, file *os.File, path string, records chan<- Record) error {
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lineNum++
		records <- Record{
			Data: map[string]any{
				"line":    scanner.Text(),
				"line_no": lineNum,
			},
			Metadata: Metadata{
				Source:    "file",
				Origin:    path,
				Timestamp: time.Now().UnixMilli(),
			},
		}
	}

	return scanner.Err()
}

func (s *FileSource) Close() error {
	return nil
}
