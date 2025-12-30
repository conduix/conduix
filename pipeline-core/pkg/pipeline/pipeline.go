// Package pipeline 파이프라인 실행 엔진
package pipeline

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/dedup"
	"github.com/conduix/conduix/pipeline-core/pkg/processor"
	"github.com/conduix/conduix/pipeline-core/pkg/sink"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// Pipeline 파이프라인 실행기
type Pipeline struct {
	config     *config.PipelineConfigV2
	source     source.Source
	processors []processor.Processor
	sink       sink.Sink
	dedup      dedup.DedupService

	// 실시간 설정
	idField        string
	eventTypeField string
	entityIDField  string

	// 통계
	stats Stats
}

// Stats 파이프라인 실행 통계
type Stats struct {
	StartTime      time.Time
	EndTime        time.Time
	TotalRecords   int64
	ProcessedCount int64
	FilteredCount  int64
	ErrorCount     int64
	DuplicateCount int64
}

// New 새 파이프라인 생성
func New(cfg *config.PipelineConfigV2) (*Pipeline, error) {
	// 소스 생성
	src, err := source.NewSource(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("failed to create source: %w", err)
	}

	// 프로세서 생성
	processors := make([]processor.Processor, 0, len(cfg.Steps))
	for _, step := range cfg.Steps {
		p, err := processor.NewProcessor(step)
		if err != nil {
			return nil, fmt.Errorf("failed to create processor %s: %w", step.Name, err)
		}
		processors = append(processors, p)
	}

	// 싱크 생성
	snk, err := sink.NewSink(cfg.Output)
	if err != nil {
		return nil, fmt.Errorf("failed to create sink: %w", err)
	}

	p := &Pipeline{
		config:     cfg,
		source:     src,
		processors: processors,
		sink:       snk,
	}

	// 실시간 모드 설정
	if cfg.IsRealtime() && cfg.Realtime != nil {
		ttl, _ := time.ParseDuration(cfg.Realtime.DedupTTL)
		dedupSvc, err := dedup.NewDedupService(cfg.Realtime.DedupStorage, ttl)
		if err != nil {
			return nil, fmt.Errorf("failed to create dedup service: %w", err)
		}
		p.dedup = dedupSvc
		p.idField = cfg.Realtime.IDField
		p.eventTypeField = cfg.Realtime.EventTypeField
		p.entityIDField = cfg.Realtime.EntityIDField
	}

	return p, nil
}

// Run 파이프라인 실행
func (p *Pipeline) Run(ctx context.Context) error {
	p.stats.StartTime = time.Now()
	defer func() {
		p.stats.EndTime = time.Now()
	}()

	log.Printf("[pipeline] Starting %s (mode=%s)", p.config.Name, p.config.Mode)

	// 소스 열기
	if err := p.source.Open(ctx); err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() { _ = p.source.Close() }()

	// 싱크 열기
	if err := p.sink.Open(ctx); err != nil {
		return fmt.Errorf("failed to open sink: %w", err)
	}
	defer func() { _ = p.sink.Close() }()

	// 데이터 읽기
	records, errs := p.source.Read(ctx)

	// 처리 루프
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				log.Printf("[pipeline] Source error: %v", err)
				p.stats.ErrorCount++
			}

		case record, ok := <-records:
			if !ok {
				// 소스 완료
				log.Printf("[pipeline] Source completed")
				return p.sink.Flush(ctx)
			}

			p.stats.TotalRecords++

			// 레코드 처리
			if err := p.processRecord(ctx, record); err != nil {
				log.Printf("[pipeline] Process error: %v", err)
				p.stats.ErrorCount++
			}
		}

		// 채널 모두 닫힘
		if records == nil && errs == nil {
			break
		}
	}

	return p.sink.Flush(ctx)
}

func (p *Pipeline) processRecord(ctx context.Context, record source.Record) error {
	// 실시간 모드: 중복 체크
	if p.config.IsRealtime() && p.dedup != nil {
		eventID := p.getField(record, p.idField)
		if eventID != "" {
			isDup, err := p.dedup.IsDuplicate(ctx, eventID)
			if err != nil {
				return fmt.Errorf("dedup check failed: %w", err)
			}
			if isDup {
				p.stats.DuplicateCount++
				return nil // 중복 스킵
			}
		}
	}

	// 실시간 모드: Upsert 로직
	if p.config.IsRealtime() && p.dedup != nil && p.eventTypeField != "" {
		record = p.applyUpsertLogic(ctx, record)
	}

	// 프로세서 체인 실행
	current := &record
	for _, proc := range p.processors {
		result, err := proc.Process(ctx, *current)
		if err != nil {
			return fmt.Errorf("processor %s failed: %w", proc.Name(), err)
		}
		if result == nil {
			p.stats.FilteredCount++
			return nil // 필터링됨
		}
		current = result
	}

	// 싱크에 기록
	if err := p.sink.Write(ctx, *current); err != nil {
		return fmt.Errorf("sink write failed: %w", err)
	}

	p.stats.ProcessedCount++

	// 실시간 모드: 처리 완료 표시
	if p.config.IsRealtime() && p.dedup != nil {
		eventID := p.getField(record, p.idField)
		if eventID != "" {
			if err := p.dedup.MarkProcessed(ctx, eventID); err != nil {
				log.Printf("[pipeline] Warning: failed to mark processed: %v", err)
			}
		}

		// 엔티티 상태 업데이트
		entityID := p.getField(record, p.entityIDField)
		eventType := p.getField(record, p.eventTypeField)
		if entityID != "" {
			switch dedup.EventType(eventType) {
			case dedup.EventCreate, dedup.EventUpdate:
				_ = p.dedup.SetEntityExists(ctx, entityID)
			case dedup.EventDelete:
				_ = p.dedup.DeleteEntity(ctx, entityID)
			}
		}
	}

	return nil
}

// applyUpsertLogic UPDATE 이벤트인데 엔티티가 없으면 CREATE로 변환
func (p *Pipeline) applyUpsertLogic(ctx context.Context, record source.Record) source.Record {
	eventType := p.getField(record, p.eventTypeField)
	entityID := p.getField(record, p.entityIDField)

	if eventType == string(dedup.EventUpdate) && entityID != "" {
		exists, err := p.dedup.EntityExists(ctx, entityID)
		if err != nil {
			log.Printf("[pipeline] Warning: entity check failed: %v", err)
			return record
		}

		if !exists {
			// UPDATE → CREATE 변환
			log.Printf("[pipeline] Upsert: converting UPDATE to CREATE for entity %s", entityID)
			newData := make(map[string]any)
			for k, v := range record.Data {
				newData[k] = v
			}
			newData[p.eventTypeField] = string(dedup.EventCreate)
			record.Data = newData
		}
	}

	return record
}

func (p *Pipeline) getField(record source.Record, field string) string {
	if field == "" {
		return ""
	}
	if val, ok := record.Data[field]; ok {
		return fmt.Sprintf("%v", val)
	}
	return ""
}

// Stats 현재 통계 반환
func (p *Pipeline) Stats() Stats {
	return p.stats
}

// Close 파이프라인 리소스 정리
func (p *Pipeline) Close() error {
	if p.dedup != nil {
		_ = p.dedup.Close()
	}
	return nil
}
