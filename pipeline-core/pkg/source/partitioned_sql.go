package source

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// PartitionedSQLSource 동적 파티션 기반 SQL 소스
// 1. 파티션 디스커버리 쿼리로 파티션 목록 조회 (예: MySQL INFORMATION_SCHEMA.PARTITIONS)
// 2. 각 파티션별로 쿼리 템플릿을 사용해 병렬 데이터 수집
type PartitionedSQLSource struct {
	cfg       config.SourceV2
	partition *config.PartitionConfig
	db        *sql.DB
}

// NewPartitionedSQLSource 파티션 기반 SQL 소스 생성
func NewPartitionedSQLSource(cfg config.SourceV2) (*PartitionedSQLSource, error) {
	if cfg.Partition == nil {
		return nil, fmt.Errorf("partition config is required for partitioned_sql source")
	}

	partition := cfg.Partition
	if partition.DiscoveryQuery == "" && len(partition.StaticPartitions) == 0 {
		return nil, fmt.Errorf("either discovery_query or static_partitions is required")
	}
	if partition.QueryTemplate == "" && cfg.Query == "" {
		return nil, fmt.Errorf("query_template or base query is required")
	}

	// 기본값 설정
	if partition.Parallelism <= 0 {
		partition.Parallelism = 4
	}

	return &PartitionedSQLSource{
		cfg:       cfg,
		partition: partition,
	}, nil
}

func (s *PartitionedSQLSource) Name() string {
	return "partitioned_sql"
}

func (s *PartitionedSQLSource) Open(ctx context.Context) error {
	db, err := sql.Open(s.cfg.Driver, s.cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// 커넥션 풀 설정 (파티션 병렬 처리용)
	db.SetMaxOpenConns(s.partition.Parallelism * 2)
	db.SetMaxIdleConns(s.partition.Parallelism)

	s.db = db
	return nil
}

func (s *PartitionedSQLSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		// 1. 파티션 목록 조회
		partitions, err := s.discoverPartitions(ctx)
		if err != nil {
			errs <- fmt.Errorf("partition discovery failed: %w", err)
			return
		}

		if len(partitions) == 0 {
			fmt.Println("[PartitionedSQL] No partitions found")
			return
		}
		fmt.Printf("[PartitionedSQL] Discovered %d partitions: %v\n", len(partitions), partitions)

		// 2. 파티션 병렬 처리
		s.fetchPartitions(ctx, partitions, records, errs)
	}()

	return records, errs
}

// discoverPartitions 파티션 목록 조회
func (s *PartitionedSQLSource) discoverPartitions(ctx context.Context) ([]string, error) {
	// 정적 파티션이 설정된 경우 바로 반환
	if len(s.partition.StaticPartitions) > 0 {
		return s.partition.StaticPartitions, nil
	}

	// SQL 쿼리로 파티션 목록 조회
	rows, err := s.db.QueryContext(ctx, s.partition.DiscoveryQuery)
	if err != nil {
		return nil, fmt.Errorf("discovery query failed: %w", err)
	}
	defer rows.Close()

	var partitions []string
	for rows.Next() {
		var partition string
		if err := rows.Scan(&partition); err != nil {
			return nil, fmt.Errorf("failed to scan partition: %w", err)
		}
		if partition != "" {
			partitions = append(partitions, partition)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return partitions, nil
}

// fetchPartitions 파티션별 데이터 병렬 수집
func (s *PartitionedSQLSource) fetchPartitions(ctx context.Context, partitions []string, records chan<- Record, errs chan<- error) {
	sem := make(chan struct{}, s.partition.Parallelism)
	var wg sync.WaitGroup

	for _, partition := range partitions {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(partitionID string) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := s.fetchPartition(ctx, partitionID, records); err != nil {
				fmt.Printf("[PartitionedSQL] Partition %s error: %v\n", partitionID, err)
			}
		}(partition)
	}

	wg.Wait()
}

// fetchPartition 단일 파티션 데이터 수집
func (s *PartitionedSQLSource) fetchPartition(ctx context.Context, partitionID string, records chan<- Record) error {
	// 쿼리 템플릿에서 ${partition} 치환
	query := s.partition.QueryTemplate
	if query == "" {
		query = s.cfg.Query
	}
	query = strings.ReplaceAll(query, "${partition}", partitionID)
	query = strings.ReplaceAll(query, "{partition}", partitionID)

	fmt.Printf("[PartitionedSQL] Fetching partition %s\n", partitionID)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("partition %s query failed: %w", partitionID, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
	}

	rowCount := 0
	for rows.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}

		data := make(map[string]any)
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				val = string(b)
			}
			data[col] = val
		}

		records <- Record{
			Data: data,
			Metadata: Metadata{
				Source:    "partitioned_sql",
				Origin:    s.cfg.DSN,
				Offset:    partitionID,
				Timestamp: time.Now().UnixMilli(),
			},
		}
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}

	fmt.Printf("[PartitionedSQL] Partition %s: %d rows\n", partitionID, rowCount)
	return nil
}

func (s *PartitionedSQLSource) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
