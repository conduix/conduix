package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

// ParquetWriter provides Parquet file writing capabilities
type ParquetWriter struct {
	path              string
	compression       string // snappy, gzip, zstd, none
	rowGroupSize      int64
	schema            []ParquetField
	schemaInferred    bool
	parquetWriter     *writer.ParquetWriter
	recordCount       int64
	maxRecordsPerFile int64
	fileIndex         int
	baseDir           string
	filePrefix        string
}

// ParquetField defines a field in the Parquet schema
type ParquetField struct {
	Name           string `json:"name"`
	Type           string `json:"type"`                     // string, int32, int64, float, double, boolean, bytes
	RepetitionType string `json:"repetition,omitempty"`     // required, optional, repeated
	ConvertedType  string `json:"converted_type,omitempty"` // utf8, timestamp_millis, etc.
}

// ParquetWriterConfig configuration for Parquet writer
type ParquetWriterConfig struct {
	Path              string         `json:"path"`
	Compression       string         `json:"compression"`          // snappy, gzip, zstd, none
	RowGroupSize      int64          `json:"row_group_size"`       // default: 128MB
	MaxRecordsPerFile int64          `json:"max_records_per_file"` // default: 1000000
	Schema            []ParquetField `json:"schema,omitempty"`     // optional, auto-infer if not provided
}

// NewParquetWriter creates a new Parquet writer
func NewParquetWriter(config ParquetWriterConfig) (*ParquetWriter, error) {
	if config.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Set defaults
	if config.Compression == "" {
		config.Compression = "snappy"
	}
	if config.RowGroupSize == 0 {
		config.RowGroupSize = 128 * 1024 * 1024 // 128MB
	}
	if config.MaxRecordsPerFile == 0 {
		config.MaxRecordsPerFile = 1000000
	}

	// Parse base directory and file prefix
	dir := filepath.Dir(config.Path)
	base := filepath.Base(config.Path)
	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext)

	return &ParquetWriter{
		path:              config.Path,
		compression:       strings.ToLower(config.Compression),
		rowGroupSize:      config.RowGroupSize,
		schema:            config.Schema,
		maxRecordsPerFile: config.MaxRecordsPerFile,
		baseDir:           dir,
		filePrefix:        prefix,
	}, nil
}

// Write writes a single record
func (w *ParquetWriter) Write(ctx context.Context, record *Record) error {
	// First record - infer schema if not provided
	if w.parquetWriter == nil {
		if len(w.schema) == 0 {
			w.schema = inferSchema(record.Data)
			w.schemaInferred = true
		}

		if err := w.createNewFile(); err != nil {
			return fmt.Errorf("failed to create parquet file: %w", err)
		}
	}

	// Check if we need to rotate file
	if w.maxRecordsPerFile > 0 && w.recordCount >= w.maxRecordsPerFile {
		if err := w.rotateFile(); err != nil {
			return fmt.Errorf("failed to rotate parquet file: %w", err)
		}
	}

	// Convert record to Parquet row
	row, err := w.recordToRow(record.Data)
	if err != nil {
		return fmt.Errorf("failed to convert record to row: %w", err)
	}

	// Write row
	if err := w.parquetWriter.Write(row); err != nil {
		return fmt.Errorf("failed to write row: %w", err)
	}

	w.recordCount++
	return nil
}

// WriteBatch writes multiple records
func (w *ParquetWriter) WriteBatch(ctx context.Context, records []*Record) error {
	for _, record := range records {
		if err := w.Write(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

// createNewFile creates a new Parquet file
func (w *ParquetWriter) createNewFile() error {
	// Ensure directory exists
	if err := os.MkdirAll(w.baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate filename
	filename := w.generateFilename()

	// Create file writer
	fw, err := local.NewLocalFileWriter(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	// Build schema string
	schemaStr := w.buildSchemaString()

	// Create Parquet writer
	pw, err := writer.NewParquetWriter(fw, schemaStr, 4)
	if err != nil {
		fw.Close()
		return fmt.Errorf("failed to create parquet writer: %w", err)
	}

	// Set compression
	pw.CompressionType = w.getCompressionType()
	pw.RowGroupSize = w.rowGroupSize

	w.parquetWriter = pw
	w.recordCount = 0

	fmt.Printf("[parquet] Created new file: %s\n", filename)
	return nil
}

// generateFilename generates a unique filename
func (w *ParquetWriter) generateFilename() string {
	timestamp := time.Now().Format("20060102-150405")
	if w.maxRecordsPerFile > 0 && w.fileIndex > 0 {
		return filepath.Join(w.baseDir, fmt.Sprintf("%s-%s-part%d.parquet",
			w.filePrefix, timestamp, w.fileIndex))
	}
	return filepath.Join(w.baseDir, fmt.Sprintf("%s-%s.parquet", w.filePrefix, timestamp))
}

// rotateFile closes current file and creates a new one
func (w *ParquetWriter) rotateFile() error {
	if err := w.closeCurrentFile(); err != nil {
		return err
	}

	w.fileIndex++
	return w.createNewFile()
}

// closeCurrentFile closes the current Parquet file
func (w *ParquetWriter) closeCurrentFile() error {
	if w.parquetWriter != nil {
		if err := w.parquetWriter.WriteStop(); err != nil {
			return fmt.Errorf("failed to stop parquet writer: %w", err)
		}
		w.parquetWriter = nil
	}
	return nil
}

// Close closes the writer
func (w *ParquetWriter) Close() error {
	return w.closeCurrentFile()
}

// buildSchemaString builds Parquet schema string in JSON format for parquet-go
func (w *ParquetWriter) buildSchemaString() string {
	var fields []map[string]any

	for _, f := range w.schema {
		rep := f.RepetitionType
		if rep == "" {
			rep = "OPTIONAL"
		}
		repCode := "1" // OPTIONAL
		switch strings.ToUpper(rep) {
		case "REQUIRED":
			repCode = "0"
		case "REPEATED":
			repCode = "2"
		}

		tag := fmt.Sprintf("name=%s, type=%s, repetitiontype=%s",
			f.Name, w.goTypeToParquetType(f.Type), strings.ToUpper(rep))

		if f.ConvertedType != "" {
			tag += fmt.Sprintf(", convertedtype=%s", strings.ToUpper(f.ConvertedType))
		}

		field := map[string]any{
			"Tag":           tag,
			"RepetitionTag": repCode,
		}
		fields = append(fields, field)
	}

	// Build JSON schema
	schema := map[string]any{
		"Tag":    "name=parquet_schema",
		"Fields": fields,
	}

	jsonBytes, _ := json.Marshal(schema)
	return string(jsonBytes)
}

// goTypeToParquetType converts Go type name to Parquet type
func (w *ParquetWriter) goTypeToParquetType(goType string) string {
	switch strings.ToLower(goType) {
	case "string":
		return "BYTE_ARRAY"
	case "int", "int32":
		return "INT32"
	case "int64":
		return "INT64"
	case "float", "float32":
		return "FLOAT"
	case "float64", "double":
		return "DOUBLE"
	case "bool", "boolean":
		return "BOOLEAN"
	case "bytes":
		return "BYTE_ARRAY"
	default:
		return "BYTE_ARRAY" // Default to bytes
	}
}

// getCompressionType returns Parquet compression type
func (w *ParquetWriter) getCompressionType() parquet.CompressionCodec {
	switch w.compression {
	case "snappy":
		return parquet.CompressionCodec_SNAPPY
	case "gzip":
		return parquet.CompressionCodec_GZIP
	case "zstd":
		return parquet.CompressionCodec_ZSTD
	case "lz4":
		return parquet.CompressionCodec_LZ4
	case "none", "uncompressed":
		return parquet.CompressionCodec_UNCOMPRESSED
	default:
		return parquet.CompressionCodec_SNAPPY
	}
}

// recordToRow converts a record to a Parquet row (map)
func (w *ParquetWriter) recordToRow(data map[string]any) (map[string]any, error) {
	row := make(map[string]any)

	for _, field := range w.schema {
		value, ok := data[field.Name]
		if !ok {
			row[field.Name] = nil
			continue
		}

		// Convert value to appropriate type
		converted, err := w.convertValue(value, field.Type)
		if err != nil {
			return nil, fmt.Errorf("failed to convert field %s: %w", field.Name, err)
		}
		row[field.Name] = converted
	}

	return row, nil
}

// convertValue converts a value to the specified type
func (w *ParquetWriter) convertValue(value any, targetType string) (any, error) {
	if value == nil {
		return nil, nil
	}

	switch strings.ToLower(targetType) {
	case "string":
		switch v := value.(type) {
		case string:
			return v, nil
		default:
			// JSON encode complex types
			if reflect.TypeOf(value).Kind() == reflect.Map ||
				reflect.TypeOf(value).Kind() == reflect.Slice {
				b, _ := json.Marshal(value)
				return string(b), nil
			}
			return fmt.Sprintf("%v", value), nil
		}

	case "int", "int32":
		switch v := value.(type) {
		case int:
			return int32(v), nil
		case int32:
			return v, nil
		case int64:
			return int32(v), nil
		case float64:
			return int32(v), nil
		}

	case "int64":
		switch v := value.(type) {
		case int:
			return int64(v), nil
		case int32:
			return int64(v), nil
		case int64:
			return v, nil
		case float64:
			return int64(v), nil
		}

	case "float", "float32":
		switch v := value.(type) {
		case float32:
			return v, nil
		case float64:
			return float32(v), nil
		case int:
			return float32(v), nil
		}

	case "float64", "double":
		switch v := value.(type) {
		case float32:
			return float64(v), nil
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		}

	case "bool", "boolean":
		switch v := value.(type) {
		case bool:
			return v, nil
		}

	case "bytes":
		switch v := value.(type) {
		case []byte:
			return v, nil
		case string:
			return []byte(v), nil
		}
	}

	return value, nil
}

// inferSchema infers Parquet schema from a record
func inferSchema(data map[string]any) []ParquetField {
	var fields []ParquetField

	for name, value := range data {
		field := ParquetField{
			Name:           name,
			Type:           inferType(value),
			RepetitionType: "OPTIONAL",
		}

		// Add converted type for strings
		if field.Type == "string" {
			field.ConvertedType = "UTF8"
		}

		fields = append(fields, field)
	}

	return fields
}

// inferType infers Parquet type from a Go value
func inferType(value any) string {
	if value == nil {
		return "string"
	}

	switch value.(type) {
	case string:
		return "string"
	case int, int32:
		return "int32"
	case int64:
		return "int64"
	case float32:
		return "float32"
	case float64:
		return "float64"
	case bool:
		return "boolean"
	case []byte:
		return "bytes"
	case map[string]any, []any:
		return "string" // JSON encode
	default:
		return "string"
	}
}

// ParquetFileSink wraps ParquetWriter as a Sink
type ParquetFileSink struct {
	BufferedSink
	writer *ParquetWriter
	config ParquetWriterConfig
}

// NewParquetFileSink creates a new Parquet file sink
func NewParquetFileSink(name string, config map[string]any) *ParquetFileSink {
	cfg := ParquetWriterConfig{
		Compression:       "snappy",
		RowGroupSize:      128 * 1024 * 1024,
		MaxRecordsPerFile: 1000000,
	}

	if v, ok := config["path"].(string); ok {
		cfg.Path = v
	}
	if v, ok := config["compression"].(string); ok {
		cfg.Compression = v
	}
	if v, ok := config["row_group_size"].(int); ok {
		cfg.RowGroupSize = int64(v)
	}
	if v, ok := config["max_records_per_file"].(int); ok {
		cfg.MaxRecordsPerFile = int64(v)
	}

	// Parse schema if provided
	if schema, ok := config["schema"].([]any); ok {
		for _, s := range schema {
			if sf, ok := s.(map[string]any); ok {
				field := ParquetField{}
				if v, ok := sf["name"].(string); ok {
					field.Name = v
				}
				if v, ok := sf["type"].(string); ok {
					field.Type = v
				}
				if v, ok := sf["repetition"].(string); ok {
					field.RepetitionType = v
				}
				if v, ok := sf["converted_type"].(string); ok {
					field.ConvertedType = v
				}
				cfg.Schema = append(cfg.Schema, field)
			}
		}
	}

	batchSize := 10000
	if buf, ok := config["buffer"].(map[string]any); ok {
		if me, ok := buf["max_events"].(int); ok {
			batchSize = me
		}
	}

	s := &ParquetFileSink{
		BufferedSink: BufferedSink{
			BaseSink: BaseSink{
				name:         name,
				typ:          "parquet",
				config:       config,
				batchSize:    batchSize,
				flushTimeout: 30 * time.Second,
			},
		},
		config: cfg,
	}

	s.init()
	s.writeFunc = s.writeBatch

	return s
}

// writeBatch writes records to Parquet
func (s *ParquetFileSink) writeBatch(ctx context.Context, records []*Record) error {
	// Lazy initialize writer
	if s.writer == nil {
		w, err := NewParquetWriter(s.config)
		if err != nil {
			return fmt.Errorf("failed to create parquet writer: %w", err)
		}
		s.writer = w
	}

	return s.writer.WriteBatch(ctx, records)
}

// Close closes the sink
func (s *ParquetFileSink) Close() error {
	if err := s.BufferedSink.Close(); err != nil {
		return err
	}
	if s.writer != nil {
		return s.writer.Close()
	}
	return nil
}

// ParquetStreamWriter writes Parquet data to an io.Writer (for S3, GCS, etc.)
type ParquetStreamWriter struct {
	writer      io.WriteCloser
	compression string
	schema      []ParquetField
}

// NewParquetStreamWriter creates a Parquet writer for streaming to cloud storage
func NewParquetStreamWriter(w io.WriteCloser, compression string, schema []ParquetField) *ParquetStreamWriter {
	return &ParquetStreamWriter{
		writer:      w,
		compression: compression,
		schema:      schema,
	}
}
