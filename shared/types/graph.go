package types

import "time"

// GraphNode React Flow 호환 노드 구조
type GraphNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"` // source, transform, sink, router, supervisor
	Label    string         `json:"label"`
	Position GraphPosition  `json:"position"`
	Data     *GraphNodeData `json:"data"`
}

// GraphPosition 노드 위치 좌표
type GraphPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// GraphNodeData 노드 상세 데이터
type GraphNodeData struct {
	ActorName   string         `json:"actor_name"`
	ActorType   string         `json:"actor_type"`
	ActorConfig map[string]any `json:"actor_config,omitempty"`
	Parallelism int            `json:"parallelism,omitempty"`
	Metrics     *ActorMetrics  `json:"metrics,omitempty"`
}

// ActorMetrics Actor별 메트릭
type ActorMetrics struct {
	InputCount       int64      `json:"input_count"`
	OutputCount      int64      `json:"output_count"`
	ErrorCount       int64      `json:"error_count"`
	ThroughputPerSec float64    `json:"throughput_per_sec"`
	State            ActorState `json:"state"`
	LastUpdated      time.Time  `json:"last_updated"`
}

// GraphEdge 노드 간 연결
type GraphEdge struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	Label    string `json:"label,omitempty"`
	Animated bool   `json:"animated,omitempty"`
}

// PipelineGraph 전체 파이프라인 그래프 구조
type PipelineGraph struct {
	PipelineID string      `json:"pipeline_id"`
	Nodes      []GraphNode `json:"nodes"`
	Edges      []GraphEdge `json:"edges"`
	Layout     string      `json:"layout,omitempty"` // "dagre" or "manual"
}

// GraphUpdateRequest 그래프 연결 업데이트 요청
type GraphUpdateRequest struct {
	AddEdges    []GraphEdge       `json:"add_edges,omitempty"`
	RemoveEdges []string          `json:"remove_edges,omitempty"`
	UpdateNodes []GraphNodeUpdate `json:"update_nodes,omitempty"`
}

// GraphNodeUpdate 노드 위치 업데이트
type GraphNodeUpdate struct {
	ID       string        `json:"id"`
	Position GraphPosition `json:"position"`
}

// ActorMetricsMap Actor별 메트릭 맵 (polling 응답용)
type ActorMetricsMap map[string]*ActorMetrics
