package models

import (
	"strings"
	"testing"
)

func TestCompressDecompressZstd_Basic(t *testing.T) {
	original := []byte("hello world, this is a test string for zstd compression")
	compressed, err := CompressZstd(original)
	if err != nil {
		t.Fatalf("CompressZstd failed: %v", err)
	}

	decompressed, err := DecompressZstd(compressed)
	if err != nil {
		t.Fatalf("DecompressZstd failed: %v", err)
	}

	if string(decompressed) != string(original) {
		t.Errorf("decompressed data mismatch: got %q, want %q", decompressed, original)
	}
}

func TestCompressZstd_Empty(t *testing.T) {
	compressed, err := CompressZstd([]byte{})
	if err != nil {
		t.Fatalf("CompressZstd failed for empty: %v", err)
	}

	decompressed, err := DecompressZstd(compressed)
	if err != nil {
		t.Fatalf("DecompressZstd failed for empty: %v", err)
	}

	if len(decompressed) != 0 {
		t.Errorf("expected empty, got %d bytes", len(decompressed))
	}
}

func TestCompressZstd_LargeData(t *testing.T) {
	// 10KB 반복 텍스트 (높은 압축률 기대)
	original := []byte(strings.Repeat("package main\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n", 200))
	compressed, err := CompressZstd(original)
	if err != nil {
		t.Fatalf("CompressZstd failed: %v", err)
	}

	// 반복 데이터는 크게 압축되어야 함
	ratio := float64(len(compressed)) / float64(len(original))
	if ratio > 0.5 {
		t.Errorf("poor compression ratio: %.2f (compressed %d, original %d)", ratio, len(compressed), len(original))
	}

	decompressed, err := DecompressZstd(compressed)
	if err != nil {
		t.Fatalf("DecompressZstd failed: %v", err)
	}

	if string(decompressed) != string(original) {
		t.Error("decompressed data does not match original")
	}
}

func TestDecompressZstd_InvalidData(t *testing.T) {
	_, err := DecompressZstd([]byte("not valid zstd data"))
	if err == nil {
		t.Error("expected error for invalid zstd data")
	}
}

func TestCompressDecompressZstd_GoSourceCode(t *testing.T) {
	// 실제 Go 소스코드 시뮬레이션
	source := `package main

import (
	"fmt"
	"github.com/conduix/conduix/plugin-sdk"
)

type MyStage struct {
	sdk.BaseNativeStage
	threshold float64
}

func (s *MyStage) Init(config map[string]any) error {
	if v, ok := config["threshold"].(float64); ok {
		s.threshold = v
	}
	return nil
}

func (s *MyStage) Process(record map[string]any) (map[string]any, error) {
	if val, ok := record["score"].(float64); ok && val < s.threshold {
		return nil, nil // drop
	}
	record["processed"] = true
	return record, nil
}

func init() {
	fmt.Println("stage registered")
}
`
	compressed, err := CompressZstd([]byte(source))
	if err != nil {
		t.Fatalf("CompressZstd failed: %v", err)
	}

	decompressed, err := DecompressZstd(compressed)
	if err != nil {
		t.Fatalf("DecompressZstd failed: %v", err)
	}

	if string(decompressed) != source {
		t.Error("decompressed source does not match original")
	}
}
