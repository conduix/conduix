package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// GraphHandler 파이프라인 그래프 핸들러
type GraphHandler struct {
	db           *database.DB
	redisService *services.RedisService
}

// NewGraphHandler 새 핸들러 생성
func NewGraphHandler(db *database.DB, redisService *services.RedisService) *GraphHandler {
	return &GraphHandler{
		db:           db,
		redisService: redisService,
	}
}

// PipelineConfig 파이프라인 설정 (YAML 파싱용)
type PipelineConfig struct {
	Version     string                     `yaml:"version"`
	Name        string                     `yaml:"name"`
	Type        string                     `yaml:"type,omitempty"`
	Description string                     `yaml:"description,omitempty"`
	ActorSystem *types.ActorSystemConfig   `yaml:"actor_system,omitempty"`
	Pipeline    *types.ActorDefinition     `yaml:"pipeline,omitempty"`
	Sources     map[string]SourceConfig    `yaml:"sources,omitempty"`
	Transforms  map[string]TransformConfig `yaml:"transforms,omitempty"`
	Sinks       map[string]SinkConfig      `yaml:"sinks,omitempty"`
}

// SourceConfig 소스 설정
type SourceConfig struct {
	Type string `yaml:"type"`
}

// TransformConfig 변환 설정
type TransformConfig struct {
	Type   string   `yaml:"type"`
	Inputs []string `yaml:"inputs"`
}

// SinkConfig 싱크 설정
type SinkConfig struct {
	Type   string   `yaml:"type"`
	Inputs []string `yaml:"inputs"`
}

// GetPipelineGraph GET /api/v1/pipelines/:id/graph
// 파이프라인 구조를 그래프로 반환
func (h *GraphHandler) GetPipelineGraph(c *gin.Context) {
	pipelineID := c.Param("id")

	var pipeline models.Pipeline
	if err := h.db.First(&pipeline, "id = ?", pipelineID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Pipeline not found")
		return
	}

	// YAML 파싱
	var config PipelineConfig
	if err := yaml.Unmarshal([]byte(pipeline.ConfigYAML), &config); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, fmt.Sprintf("Invalid pipeline config: %v", err))
		return
	}

	// 타입 결정
	pipelineType := config.Type
	if pipelineType == "" {
		if config.Pipeline != nil {
			pipelineType = "actor"
		} else {
			pipelineType = "flat"
		}
	}

	// 그래프 변환
	var graph *types.PipelineGraph
	if pipelineType == "actor" && config.Pipeline != nil {
		graph = convertActorToGraph(pipelineID, config.Pipeline)
	} else {
		graph = convertFlatToGraph(pipelineID, &config)
	}

	c.JSON(http.StatusOK, types.APIResponse[types.PipelineGraph]{
		Success: true,
		Data:    *graph,
	})
}

// convertActorToGraph Actor 모드 설정을 그래프로 변환
func convertActorToGraph(pipelineID string, root *types.ActorDefinition) *types.PipelineGraph {
	graph := &types.PipelineGraph{
		PipelineID: pipelineID,
		Nodes:      []types.GraphNode{},
		Edges:      []types.GraphEdge{},
		Layout:     "dagre",
	}

	// 재귀적으로 노드와 엣지 생성
	nodeIndex := 0
	convertActorNodeRecursive(root, graph, nil, 0, &nodeIndex)

	return graph
}

// convertActorNodeRecursive Actor 노드를 재귀적으로 변환
func convertActorNodeRecursive(def *types.ActorDefinition, graph *types.PipelineGraph, parentID *string, depth int, index *int) {
	nodeID := def.Name

	// 노드 생성
	node := types.GraphNode{
		ID:    nodeID,
		Type:  string(def.Type),
		Label: def.Name,
		Position: types.GraphPosition{
			X: float64(depth * 300),
			Y: float64(*index * 120),
		},
		Data: &types.GraphNodeData{
			ActorName:   def.Name,
			ActorType:   string(def.Type),
			ActorConfig: def.Config,
			Parallelism: def.Parallelism,
		},
	}
	graph.Nodes = append(graph.Nodes, node)
	*index++

	// 부모-자식 관계 엣지: supervisor의 직접 자식이 아닌 경우에만 부모 엣지 생성
	// supervisor 자체는 계층 구조를 나타내므로 데이터 흐름 엣지는 outputs에서 처리
	// TODO: 필요시 parentID와 def.Type을 사용한 엣지 생성 로직 구현

	// Outputs 연결 (데이터 흐름 엣지)
	for _, output := range def.Outputs {
		edge := types.GraphEdge{
			ID:       fmt.Sprintf("%s-%s", nodeID, output),
			Source:   nodeID,
			Target:   output,
			Animated: true,
		}
		graph.Edges = append(graph.Edges, edge)
	}

	// Children 재귀 처리
	for i := range def.Children {
		child := &def.Children[i]
		convertActorNodeRecursive(child, graph, &nodeID, depth+1, index)
	}
}

// convertFlatToGraph Flat 모드 설정을 그래프로 변환
func convertFlatToGraph(pipelineID string, config *PipelineConfig) *types.PipelineGraph {
	graph := &types.PipelineGraph{
		PipelineID: pipelineID,
		Nodes:      []types.GraphNode{},
		Edges:      []types.GraphEdge{},
		Layout:     "dagre",
	}

	yOffset := 0

	// Sources 노드 생성
	for name, src := range config.Sources {
		node := types.GraphNode{
			ID:    name,
			Type:  "source",
			Label: name,
			Position: types.GraphPosition{
				X: 0,
				Y: float64(yOffset),
			},
			Data: &types.GraphNodeData{
				ActorName: name,
				ActorType: "source",
				ActorConfig: map[string]any{
					"source_type": src.Type,
				},
			},
		}
		graph.Nodes = append(graph.Nodes, node)
		yOffset += 120
	}

	// Transforms 노드 생성
	transformYOffset := 0
	for name, transform := range config.Transforms {
		node := types.GraphNode{
			ID:    name,
			Type:  "transform",
			Label: name,
			Position: types.GraphPosition{
				X: 300,
				Y: float64(transformYOffset),
			},
			Data: &types.GraphNodeData{
				ActorName: name,
				ActorType: "transform",
				ActorConfig: map[string]any{
					"transform_type": transform.Type,
				},
			},
		}
		graph.Nodes = append(graph.Nodes, node)

		// 입력 엣지 생성
		for _, input := range transform.Inputs {
			edge := types.GraphEdge{
				ID:       fmt.Sprintf("%s-%s", input, name),
				Source:   input,
				Target:   name,
				Animated: true,
			}
			graph.Edges = append(graph.Edges, edge)
		}

		transformYOffset += 120
	}

	// Sinks 노드 생성
	sinkYOffset := 0
	for name, sink := range config.Sinks {
		node := types.GraphNode{
			ID:    name,
			Type:  "sink",
			Label: name,
			Position: types.GraphPosition{
				X: 600,
				Y: float64(sinkYOffset),
			},
			Data: &types.GraphNodeData{
				ActorName: name,
				ActorType: "sink",
				ActorConfig: map[string]any{
					"sink_type": sink.Type,
				},
			},
		}
		graph.Nodes = append(graph.Nodes, node)

		// 입력 엣지 생성
		for _, input := range sink.Inputs {
			edge := types.GraphEdge{
				ID:       fmt.Sprintf("%s-%s", input, name),
				Source:   input,
				Target:   name,
				Animated: true,
			}
			graph.Edges = append(graph.Edges, edge)
		}

		sinkYOffset += 120
	}

	return graph
}

// UpdatePipelineGraph PUT /api/v1/pipelines/:id/graph
// 파이프라인 그래프 연결 업데이트
func (h *GraphHandler) UpdatePipelineGraph(c *gin.Context) {
	pipelineID := c.Param("id")

	var req types.GraphUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	var pipeline models.Pipeline
	if err := h.db.First(&pipeline, "id = ?", pipelineID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Pipeline not found")
		return
	}

	// YAML 파싱
	var config PipelineConfig
	if err := yaml.Unmarshal([]byte(pipeline.ConfigYAML), &config); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, fmt.Sprintf("Invalid pipeline config: %v", err))
		return
	}

	// 연결 변경 적용
	pipelineType := config.Type
	if pipelineType == "" {
		if config.Pipeline != nil {
			pipelineType = "actor"
		} else {
			pipelineType = "flat"
		}
	}

	var updateErr error
	if pipelineType == "actor" && config.Pipeline != nil {
		updateErr = applyActorEdgeChanges(config.Pipeline, &req)
	} else {
		updateErr = applyFlatEdgeChanges(&config, &req)
	}

	if updateErr != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, updateErr.Error())
		return
	}

	// YAML 재생성
	newYAML, err := yaml.Marshal(&config)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, "Failed to generate config")
		return
	}

	// 저장
	pipeline.ConfigYAML = string(newYAML)
	pipeline.UpdatedAt = time.Now()
	if err := h.db.Save(&pipeline).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to save pipeline")
		return
	}

	// 업데이트된 그래프 반환
	var graph *types.PipelineGraph
	if pipelineType == "actor" && config.Pipeline != nil {
		graph = convertActorToGraph(pipelineID, config.Pipeline)
	} else {
		graph = convertFlatToGraph(pipelineID, &config)
	}

	c.JSON(http.StatusOK, types.APIResponse[types.PipelineGraph]{
		Success: true,
		Data:    *graph,
	})
}

// applyActorEdgeChanges Actor 모드 연결 변경 적용
func applyActorEdgeChanges(root *types.ActorDefinition, req *types.GraphUpdateRequest) error {
	actorMap := buildActorMap(root)

	// 엣지 추가
	for _, edge := range req.AddEdges {
		sourceActor, ok := actorMap[edge.Source]
		if !ok {
			return fmt.Errorf("source actor not found: %s", edge.Source)
		}
		if _, ok := actorMap[edge.Target]; !ok {
			return fmt.Errorf("target actor not found: %s", edge.Target)
		}

		// 중복 확인
		found := false
		for _, output := range sourceActor.Outputs {
			if output == edge.Target {
				found = true
				break
			}
		}
		if !found {
			sourceActor.Outputs = append(sourceActor.Outputs, edge.Target)
		}
	}

	// 엣지 제거
	for _, edgeID := range req.RemoveEdges {
		// edgeID 형식: "source-target"
		for _, actor := range actorMap {
			newOutputs := []string{}
			for _, output := range actor.Outputs {
				expectedID := fmt.Sprintf("%s-%s", actor.Name, output)
				if expectedID != edgeID {
					newOutputs = append(newOutputs, output)
				}
			}
			actor.Outputs = newOutputs
		}
	}

	return nil
}

// buildActorMap Actor 맵 생성 (이름 → Actor 포인터)
func buildActorMap(root *types.ActorDefinition) map[string]*types.ActorDefinition {
	actorMap := make(map[string]*types.ActorDefinition)
	buildActorMapRecursive(root, actorMap)
	return actorMap
}

func buildActorMapRecursive(def *types.ActorDefinition, actorMap map[string]*types.ActorDefinition) {
	actorMap[def.Name] = def
	for i := range def.Children {
		buildActorMapRecursive(&def.Children[i], actorMap)
	}
}

// applyFlatEdgeChanges Flat 모드 연결 변경 적용
func applyFlatEdgeChanges(config *PipelineConfig, req *types.GraphUpdateRequest) error {
	// 모든 노드 이름 수집
	nodeNames := make(map[string]string) // name → type (source/transform/sink)
	for name := range config.Sources {
		nodeNames[name] = "source"
	}
	for name := range config.Transforms {
		nodeNames[name] = "transform"
	}
	for name := range config.Sinks {
		nodeNames[name] = "sink"
	}

	// 엣지 추가
	for _, edge := range req.AddEdges {
		sourceType, sourceOk := nodeNames[edge.Source]
		targetType, targetOk := nodeNames[edge.Target]
		if !sourceOk {
			return fmt.Errorf("source not found: %s", edge.Source)
		}
		if !targetOk {
			return fmt.Errorf("target not found: %s", edge.Target)
		}

		// 연결 유효성 검증
		if !isValidConnection(sourceType, targetType) {
			return fmt.Errorf("invalid connection: %s (%s) → %s (%s)", edge.Source, sourceType, edge.Target, targetType)
		}

		// Target의 inputs에 추가
		switch targetType {
		case "transform":
			transform := config.Transforms[edge.Target]
			if !containsString(transform.Inputs, edge.Source) {
				transform.Inputs = append(transform.Inputs, edge.Source)
				config.Transforms[edge.Target] = transform
			}
		case "sink":
			sink := config.Sinks[edge.Target]
			if !containsString(sink.Inputs, edge.Source) {
				sink.Inputs = append(sink.Inputs, edge.Source)
				config.Sinks[edge.Target] = sink
			}
		}
	}

	// 엣지 제거
	for _, edgeID := range req.RemoveEdges {
		// edgeID 형식: "source-target"
		for name, transform := range config.Transforms {
			newInputs := []string{}
			for _, input := range transform.Inputs {
				expectedID := fmt.Sprintf("%s-%s", input, name)
				if expectedID != edgeID {
					newInputs = append(newInputs, input)
				}
			}
			transform.Inputs = newInputs
			config.Transforms[name] = transform
		}

		for name, sink := range config.Sinks {
			newInputs := []string{}
			for _, input := range sink.Inputs {
				expectedID := fmt.Sprintf("%s-%s", input, name)
				if expectedID != edgeID {
					newInputs = append(newInputs, input)
				}
			}
			sink.Inputs = newInputs
			config.Sinks[name] = sink
		}
	}

	return nil
}

// isValidConnection 연결 유효성 검증
func isValidConnection(sourceType, targetType string) bool {
	// source → transform, sink
	// transform → transform, sink
	// sink → (nothing)
	switch sourceType {
	case "source":
		return targetType == "transform" || targetType == "sink"
	case "transform":
		return targetType == "transform" || targetType == "sink"
	default:
		return false
	}
}

func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// GetActorMetrics GET /api/v1/pipelines/:id/actor-metrics
// Actor별 메트릭 조회 (폴링용)
func (h *GraphHandler) GetActorMetrics(c *gin.Context) {
	pipelineID := c.Param("id")

	// 1. 파이프라인 존재 확인
	var pipeline models.Pipeline
	if err := h.db.First(&pipeline, "id = ?", pipelineID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Pipeline not found")
		return
	}

	// 2. 최근 실행 통계 조회
	var stats models.PipelineExecutionStats
	h.db.Where("pipeline_id = ?", pipelineID).
		Order("started_at DESC").
		First(&stats)

	// 3. per_stage_counts 파싱
	actorMetrics := make(types.ActorMetricsMap)

	if stats.PerStageCounts != "" {
		var stageCounts map[string]int64
		if err := json.Unmarshal([]byte(stats.PerStageCounts), &stageCounts); err == nil {
			for name, count := range stageCounts {
				actorMetrics[name] = &types.ActorMetrics{
					InputCount:  count,
					OutputCount: count, // 근사값
					State:       types.ActorStateRunning,
					LastUpdated: stats.StartedAt,
				}
			}
		}
	}

	// 4. 소스/싱크 메트릭 추가
	if stats.RecordsCollected > 0 {
		// YAML 파싱하여 소스/싱크 이름 가져오기
		var config PipelineConfig
		if err := yaml.Unmarshal([]byte(pipeline.ConfigYAML), &config); err == nil {
			// 소스 메트릭
			for name := range config.Sources {
				if _, exists := actorMetrics[name]; !exists {
					actorMetrics[name] = &types.ActorMetrics{
						InputCount:  0,
						OutputCount: stats.RecordsCollected,
						State:       types.ActorStateRunning,
						LastUpdated: stats.StartedAt,
					}
				}
			}
			// 싱크 메트릭
			for name := range config.Sinks {
				if _, exists := actorMetrics[name]; !exists {
					actorMetrics[name] = &types.ActorMetrics{
						InputCount:  stats.RecordsProcessed,
						OutputCount: stats.RecordsProcessed,
						ErrorCount:  stats.ProcessingErrors,
						State:       types.ActorStateRunning,
						LastUpdated: stats.StartedAt,
					}
				}
			}
		}
	}

	// 5. Redis에서 실시간 메트릭 조회 (가능한 경우)
	// TODO: h.redisService를 사용한 실시간 메트릭 조회 구현

	c.JSON(http.StatusOK, types.APIResponse[types.ActorMetricsMap]{
		Success: true,
		Data:    actorMetrics,
	})
}
