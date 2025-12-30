// Package handlers API 핸들러
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/conduix/conduix/pipeline-core/pkg/filter"
)

// FilterHandlers 필터 관련 API 핸들러
type FilterHandlers struct {
	registry *filter.FilterRegistry
}

// NewFilterHandlers 핸들러 생성
func NewFilterHandlers() *FilterHandlers {
	return &FilterHandlers{
		registry: filter.Global(),
	}
}

// GetOperators GET /api/v1/filters/operators
// 사용 가능한 모든 필터 연산자 목록 반환
func (h *FilterHandlers) GetOperators(w http.ResponseWriter, r *http.Request) {
	operators := h.registry.List()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"operators":  operators,
		"categories": h.registry.Categories(),
	})
}

// GetOperatorsByCategory GET /api/v1/filters/operators/:category
// 특정 카테고리의 연산자 목록 반환
func (h *FilterHandlers) GetOperatorsByCategory(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	if category == "" {
		http.Error(w, "category is required", http.StatusBadRequest)
		return
	}

	operators := h.registry.ListByCategory(category)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"operators": operators,
		"category":  category,
	})
}

// ValidateFilter POST /api/v1/filters/validate
// 필터 표현식 유효성 검증
func (h *FilterHandlers) ValidateFilter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filter *filter.Filter `json:"filter"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Filter == nil {
		http.Error(w, "filter is required", http.StatusBadRequest)
		return
	}

	// 필터 유효성 검증
	err := req.Filter.Validate()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid":   false,
			"message": err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"valid":   true,
		"message": "필터가 유효합니다",
	})
}

// ConvertFilter POST /api/v1/filters/convert
// 필터 형식 변환 (표현식 ↔ 구조화)
func (h *FilterHandlers) ConvertFilter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Expression string         `json:"expression,omitempty"`
		Filter     *filter.Filter `json:"filter,omitempty"`
		To         string         `json:"to"` // "expression" 또는 "structured"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	converter := filter.NewConverter()
	w.Header().Set("Content-Type", "application/json")

	switch req.To {
	case "expression":
		// 구조화 → 표현식
		if req.Filter == nil {
			http.Error(w, "filter is required for conversion to expression", http.StatusBadRequest)
			return
		}
		expr, err := converter.StructuredToExpression(req.Filter)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":    true,
			"expression": expr,
		})

	case "structured":
		// 표현식 → 구조화
		if req.Expression == "" {
			http.Error(w, "expression is required for conversion to structured", http.StatusBadRequest)
			return
		}
		f, err := converter.ExpressionToStructured(req.Expression)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"filter":  f,
		})

	default:
		http.Error(w, "to must be 'expression' or 'structured'", http.StatusBadRequest)
	}
}

// TestFilter POST /api/v1/filters/test
// 필터를 샘플 데이터에 대해 테스트
func (h *FilterHandlers) TestFilter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filter *filter.Filter   `json:"filter"`
		Data   []map[string]any `json:"data"` // 테스트 데이터
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Filter == nil {
		http.Error(w, "filter is required", http.StatusBadRequest)
		return
	}

	if len(req.Data) == 0 {
		http.Error(w, "data is required", http.StatusBadRequest)
		return
	}

	// 평가기 생성
	evaluator, err := filter.NewEvaluator(req.Filter)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// 각 데이터에 대해 필터 테스트
	results := make([]map[string]any, len(req.Data))
	passCount := 0

	for i, data := range req.Data {
		passed, err := evaluator.Evaluate(data)
		results[i] = map[string]any{
			"index":  i,
			"data":   data,
			"passed": passed,
		}
		if err != nil {
			results[i]["error"] = err.Error()
		}
		if passed {
			passCount++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"results": results,
		"summary": map[string]any{
			"total":  len(req.Data),
			"passed": passCount,
			"failed": len(req.Data) - passCount,
		},
	})
}
