package models

import (
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// CompressZstd zstd 압축
func CompressZstd(data []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}
	result := encoder.EncodeAll(data, make([]byte, 0, len(data)/2))
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zstd encoder: %w", err)
	}
	return result, nil
}

// DecompressZstd zstd 압축 해제
func DecompressZstd(data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	defer decoder.Close()

	return decoder.DecodeAll(data, nil)
}
