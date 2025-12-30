package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/filter"
)

func TestNewFilterHandlers(t *testing.T) {
	h := NewFilterHandlers()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.registry == nil {
		t.Error("expected registry to be set")
	}
}

func TestGetOperators(t *testing.T) {
	h := NewFilterHandlers()

	req := httptest.NewRequest("GET", "/api/v1/filters/operators", http.NoBody)
	w := httptest.NewRecorder()

	h.GetOperators(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["operators"] == nil {
		t.Error("expected operators in response")
	}
	if response["categories"] == nil {
		t.Error("expected categories in response")
	}
}

func TestGetOperatorsByCategory(t *testing.T) {
	h := NewFilterHandlers()

	tests := []struct {
		name         string
		category     string
		expectStatus int
	}{
		{
			name:         "with category",
			category:     "comparison",
			expectStatus: http.StatusOK,
		},
		{
			name:         "missing category",
			category:     "",
			expectStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/filters/operators"
			if tt.category != "" {
				url += "?category=" + tt.category
			}

			req := httptest.NewRequest("GET", url, http.NoBody)
			w := httptest.NewRecorder()

			h.GetOperatorsByCategory(w, req)

			if w.Code != tt.expectStatus {
				t.Errorf("expected status %d, got %d", tt.expectStatus, w.Code)
			}
		})
	}
}

func TestValidateFilter_ValidExpression(t *testing.T) {
	h := NewFilterHandlers()

	f := filter.Filter{
		Expression: ".status == 'active'",
	}
	body, _ := json.Marshal(map[string]any{"filter": f})

	req := httptest.NewRequest("POST", "/api/v1/filters/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ValidateFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["valid"] != true {
		t.Errorf("expected valid=true, got %v", response["valid"])
	}
}

func TestValidateFilter_ValidRoot(t *testing.T) {
	h := NewFilterHandlers()

	f := filter.Filter{
		Root: filter.NewCondition("status", filter.OpEqual, "active"),
	}
	body, _ := json.Marshal(map[string]any{"filter": f})

	req := httptest.NewRequest("POST", "/api/v1/filters/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ValidateFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["valid"] != true {
		t.Errorf("expected valid=true, got %v", response["valid"])
	}
}

func TestValidateFilter_InvalidFilter(t *testing.T) {
	h := NewFilterHandlers()

	// Empty filter - neither expression nor root
	f := filter.Filter{}
	body, _ := json.Marshal(map[string]any{"filter": f})

	req := httptest.NewRequest("POST", "/api/v1/filters/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ValidateFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["valid"] != false {
		t.Errorf("expected valid=false, got %v", response["valid"])
	}
}

func TestValidateFilter_InvalidJSON(t *testing.T) {
	h := NewFilterHandlers()

	req := httptest.NewRequest("POST", "/api/v1/filters/validate", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ValidateFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestValidateFilter_MissingFilter(t *testing.T) {
	h := NewFilterHandlers()

	body, _ := json.Marshal(map[string]any{})

	req := httptest.NewRequest("POST", "/api/v1/filters/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ValidateFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestConvertFilter_ToExpression(t *testing.T) {
	h := NewFilterHandlers()

	f := filter.Filter{
		Root: filter.NewCondition("status", filter.OpEqual, "active"),
	}
	body, _ := json.Marshal(map[string]any{
		"filter": f,
		"to":     "expression",
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/convert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ConvertFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["success"] != true {
		t.Errorf("expected success=true, got %v", response["success"])
	}
	if response["expression"] == nil {
		t.Error("expected expression in response")
	}
}

func TestConvertFilter_ToStructured(t *testing.T) {
	h := NewFilterHandlers()

	body, _ := json.Marshal(map[string]any{
		"expression": ".status == 'active'",
		"to":         "structured",
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/convert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ConvertFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["success"] != true {
		t.Errorf("expected success=true, got %v", response["success"])
	}
	if response["filter"] == nil {
		t.Error("expected filter in response")
	}
}

func TestConvertFilter_InvalidTo(t *testing.T) {
	h := NewFilterHandlers()

	body, _ := json.Marshal(map[string]any{
		"expression": ".status == 'active'",
		"to":         "invalid",
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/convert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ConvertFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestConvertFilter_MissingFilter(t *testing.T) {
	h := NewFilterHandlers()

	body, _ := json.Marshal(map[string]any{
		"to": "expression",
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/convert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ConvertFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestConvertFilter_MissingExpression(t *testing.T) {
	h := NewFilterHandlers()

	body, _ := json.Marshal(map[string]any{
		"to": "structured",
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/convert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ConvertFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestTestFilter_Success(t *testing.T) {
	h := NewFilterHandlers()

	f := filter.Filter{
		Expression: ".status == 'active'",
	}
	data := []map[string]any{
		{"status": "active", "name": "item1"},
		{"status": "inactive", "name": "item2"},
		{"status": "active", "name": "item3"},
	}

	body, _ := json.Marshal(map[string]any{
		"filter": f,
		"data":   data,
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.TestFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["success"] != true {
		t.Errorf("expected success=true, got %v", response["success"])
	}

	summary, ok := response["summary"].(map[string]any)
	if !ok {
		t.Fatal("expected summary in response")
	}

	if summary["total"] != float64(3) {
		t.Errorf("expected total=3, got %v", summary["total"])
	}
	if summary["passed"] != float64(2) {
		t.Errorf("expected passed=2, got %v", summary["passed"])
	}
	if summary["failed"] != float64(1) {
		t.Errorf("expected failed=1, got %v", summary["failed"])
	}
}

func TestTestFilter_MissingFilter(t *testing.T) {
	h := NewFilterHandlers()

	body, _ := json.Marshal(map[string]any{
		"data": []map[string]any{{"status": "active"}},
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.TestFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestTestFilter_MissingData(t *testing.T) {
	h := NewFilterHandlers()

	f := filter.Filter{
		Expression: ".status == 'active'",
	}
	body, _ := json.Marshal(map[string]any{
		"filter": f,
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.TestFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestTestFilter_EmptyData(t *testing.T) {
	h := NewFilterHandlers()

	f := filter.Filter{
		Expression: ".status == 'active'",
	}
	body, _ := json.Marshal(map[string]any{
		"filter": f,
		"data":   []map[string]any{},
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.TestFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestTestFilter_WithRootCondition(t *testing.T) {
	h := NewFilterHandlers()

	// Test with a proper root condition
	f := filter.Filter{
		Root: filter.NewCondition("name", filter.OpContains, "test"),
	}
	body, _ := json.Marshal(map[string]any{
		"filter": f,
		"data":   []map[string]any{{"name": "test-item"}, {"name": "other"}},
	})

	req := httptest.NewRequest("POST", "/api/v1/filters/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.TestFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["success"] != true {
		t.Errorf("expected success=true, got %v", response["success"])
	}

	summary, ok := response["summary"].(map[string]any)
	if !ok {
		t.Fatal("expected summary in response")
	}
	if summary["passed"] != float64(1) {
		t.Errorf("expected passed=1, got %v", summary["passed"])
	}
}

func BenchmarkValidateFilter(b *testing.B) {
	h := NewFilterHandlers()

	f := filter.Filter{
		Expression: ".status == 'active' && .level != 'debug'",
	}
	body, _ := json.Marshal(map[string]any{"filter": f})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/api/v1/filters/validate", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ValidateFilter(w, req)
	}
}

func BenchmarkTestFilter(b *testing.B) {
	h := NewFilterHandlers()

	f := filter.Filter{
		Expression: ".status == 'active'",
	}
	data := []map[string]any{
		{"status": "active"},
		{"status": "inactive"},
		{"status": "active"},
	}
	body, _ := json.Marshal(map[string]any{
		"filter": f,
		"data":   data,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/api/v1/filters/test", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.TestFilter(w, req)
	}
}
