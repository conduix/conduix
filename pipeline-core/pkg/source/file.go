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
	"strings"
	"time"

	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// FileSource 파일 소스
type FileSource struct {
	paths     []string
	format    string // json, csv, lines
	encoding  string // utf-8(기본), cp949, euc-kr
	csvHeader bool   //nolint:unused
}

// decodedReader 는 파일 인코딩을 UTF-8 로 바꿔 준다.
//
// 왜 필요한가: 국내 공공데이터 CSV 는 대부분 cp949 다. 그대로 읽으면 행 파싱은
// 되는데 한글만 깨져 들어간다(실측: 공중화장실정보.csv 53,582행 전량이 무효 UTF-8).
// 그 상태로 적재하면 DB 에 깨진 문자가 그대로 쌓인다.
//
// euc-kr 이 아니라 cp949 를 기본으로 권하는 이유: euc-kr 로 디코딩하면 확장 한글
// (똠, 뷁 등 8,822자)에서 변환이 끊긴다 — 같은 파일이 53,775행에서 4,722행으로
// 잘리는 것을 실측했다. cp949 는 euc-kr 의 상위 호환이라 둘 다 안전하게 읽는다.
func decodedReader(r io.Reader, encoding string) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "utf-8", "utf8":
		return r, nil
	case "cp949", "euc-kr", "euckr", "ks_c_5601-1987", "windows-949":
		return transform.NewReader(r, korean.EUCKR.NewDecoder()), nil
	default:
		return nil, fmt.Errorf("unsupported encoding: %s (supported: utf-8, cp949, euc-kr)", encoding)
	}
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

	// 인코딩은 여기서 한 번 검증한다 — 읽는 중에 실패하면 어느 파일 몇 행에서
	// 깨졌는지 알기 어렵고, 이미 일부가 적재된 뒤일 수 있다.
	if _, err := decodedReader(strings.NewReader(""), cfg.Encoding); err != nil {
		return nil, err
	}

	return &FileSource{
		paths:    expandedPaths,
		format:   format,
		encoding: cfg.Encoding,
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
	src, err := decodedReader(file, s.encoding)
	if err != nil {
		return err
	}
	reader := csv.NewReader(src)

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
