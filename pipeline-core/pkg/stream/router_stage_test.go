package stream

import (
	"context"
	"testing"
)

func TestRouterStageConditionMode(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "high_priority", Condition: ".priority >= 8", Outputs: []string{"alert", "kafka"}},
			{Name: "medium", Condition: ".priority >= 5", Outputs: []string{"es"}},
			{Name: "low", Condition: ".priority < 5", Outputs: []string{"s3"}},
		},
		DefaultRoute: &Route{Name: "_default", Outputs: []string{"default_output"}},
	}

	stage, err := NewRouterStage("test_router", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	tests := []struct {
		name           string
		data           map[string]any
		expectedRoute  string
		expectedOutputs []string
	}{
		{
			name:           "high priority",
			data:           map[string]any{"priority": 9.0},
			expectedRoute:  "high_priority",
			expectedOutputs: []string{"alert", "kafka"},
		},
		{
			name:           "medium priority",
			data:           map[string]any{"priority": 6.0},
			expectedRoute:  "medium",
			expectedOutputs: []string{"es"},
		},
		{
			name:           "low priority",
			data:           map[string]any{"priority": 3.0},
			expectedRoute:  "low",
			expectedOutputs: []string{"s3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := &Record{Data: tt.data}
			results := stage.Route(record)

			if len(results) != 1 {
				t.Fatalf("Expected 1 route result, got %d", len(results))
			}

			if results[0].RouteName != tt.expectedRoute {
				t.Errorf("Expected route %s, got %s", tt.expectedRoute, results[0].RouteName)
			}

			if len(results[0].Outputs) != len(tt.expectedOutputs) {
				t.Errorf("Expected %d outputs, got %d", len(tt.expectedOutputs), len(results[0].Outputs))
			}
		})
	}
}

func TestRouterStageFanOutMode(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeFanOut,
		Routes: []Route{
			{Name: "route1", Outputs: []string{"output1"}},
			{Name: "route2", Outputs: []string{"output2"}},
			{Name: "route3", Outputs: []string{"output3"}},
		},
	}

	stage, err := NewRouterStage("test_fanout", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	record := &Record{Data: map[string]any{"message": "test"}}
	results := stage.Route(record)

	// Fan-out should return all routes
	if len(results) != 3 {
		t.Errorf("Expected 3 route results for fan-out, got %d", len(results))
	}
}

func TestRouterStageFilterMode(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeFilter,
		Routes: []Route{
			{Name: "has_name", Condition: ".name exists", Outputs: []string{"output1"}},
			{Name: "has_email", Condition: ".email exists", Outputs: []string{"output2"}},
			{Name: "active", Condition: ".active == true", Outputs: []string{"output3"}},
		},
	}

	stage, err := NewRouterStage("test_filter", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// Record matches all conditions
	record := &Record{
		Data: map[string]any{
			"name":   "John",
			"email":  "john@example.com",
			"active": true,
		},
	}
	results := stage.Route(record)

	if len(results) != 3 {
		t.Errorf("Expected 3 matching routes, got %d", len(results))
	}

	// Record matches only some conditions
	record2 := &Record{
		Data: map[string]any{
			"name": "Jane",
		},
	}
	results2 := stage.Route(record2)

	if len(results2) != 1 {
		t.Errorf("Expected 1 matching route, got %d", len(results2))
	}
}

func TestRouterStageDefaultRoute(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "match", Condition: ".type == \"special\"", Outputs: []string{"special_output"}},
		},
		DefaultRoute: &Route{Name: "_default", Outputs: []string{"default_output"}},
	}

	stage, err := NewRouterStage("test_default", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// Record doesn't match any condition
	record := &Record{Data: map[string]any{"type": "normal"}}
	results := stage.Route(record)

	if len(results) != 1 {
		t.Fatalf("Expected 1 route result (default), got %d", len(results))
	}

	if results[0].RouteName != "_default" {
		t.Errorf("Expected default route, got %s", results[0].RouteName)
	}

	if results[0].Outputs[0] != "default_output" {
		t.Errorf("Expected default_output, got %s", results[0].Outputs[0])
	}
}

func TestRouterStageStringConditions(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "error", Condition: ".level == \"error\"", Outputs: []string{"alert"}},
			{Name: "not_debug", Condition: ".level != \"debug\"", Outputs: []string{"logs"}},
		},
	}

	stage, err := NewRouterStage("test_string", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// Test error level
	record1 := &Record{Data: map[string]any{"level": "error"}}
	results1 := stage.Route(record1)

	if len(results1) != 1 || results1[0].RouteName != "error" {
		t.Errorf("Expected 'error' route for error level")
	}

	// Test info level (should match not_debug)
	record2 := &Record{Data: map[string]any{"level": "info"}}
	results2 := stage.Route(record2)

	if len(results2) != 1 || results2[0].RouteName != "not_debug" {
		t.Errorf("Expected 'not_debug' route for info level")
	}
}

func TestRouterStageAndConditions(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "match", Condition: ".status == \"active\" && .priority >= 5", Outputs: []string{"high"}},
		},
	}

	stage, err := NewRouterStage("test_and", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// Both conditions true
	record1 := &Record{Data: map[string]any{"status": "active", "priority": 8.0}}
	results1 := stage.Route(record1)

	if len(results1) != 1 {
		t.Errorf("Expected match for AND condition")
	}

	// Only one condition true
	record2 := &Record{Data: map[string]any{"status": "active", "priority": 3.0}}
	results2 := stage.Route(record2)

	if len(results2) != 0 {
		t.Errorf("Expected no match when one AND condition is false")
	}
}

func TestRouterStageOrConditions(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "match", Condition: ".type == \"urgent\" || .priority >= 9", Outputs: []string{"alert"}},
		},
	}

	stage, err := NewRouterStage("test_or", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// First condition true
	record1 := &Record{Data: map[string]any{"type": "urgent", "priority": 1.0}}
	results1 := stage.Route(record1)

	if len(results1) != 1 {
		t.Errorf("Expected match for OR condition (first true)")
	}

	// Second condition true
	record2 := &Record{Data: map[string]any{"type": "normal", "priority": 10.0}}
	results2 := stage.Route(record2)

	if len(results2) != 1 {
		t.Errorf("Expected match for OR condition (second true)")
	}

	// Neither condition true
	record3 := &Record{Data: map[string]any{"type": "normal", "priority": 5.0}}
	results3 := stage.Route(record3)

	if len(results3) != 0 {
		t.Errorf("Expected no match when both OR conditions are false")
	}
}

func TestRouterStageExistsCondition(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "has_error", Condition: ".error exists", Outputs: []string{"error_handler"}},
			{Name: "no_error", Condition: ".error not exists", Outputs: []string{"normal"}},
		},
	}

	stage, err := NewRouterStage("test_exists", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// With error field
	record1 := &Record{Data: map[string]any{"error": "something went wrong"}}
	results1 := stage.Route(record1)

	if len(results1) != 1 || results1[0].RouteName != "has_error" {
		t.Errorf("Expected 'has_error' route")
	}

	// Without error field
	record2 := &Record{Data: map[string]any{"message": "ok"}}
	results2 := stage.Route(record2)

	if len(results2) != 1 || results2[0].RouteName != "no_error" {
		t.Errorf("Expected 'no_error' route")
	}
}

func TestRouterStageRegexCondition(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "email", Condition: ".contact =~ \".*@.*\\.com\"", Outputs: []string{"email_handler"}},
			{Name: "phone", Condition: ".contact =~ \"[0-9]{3}-[0-9]{4}\"", Outputs: []string{"phone_handler"}},
		},
	}

	stage, err := NewRouterStage("test_regex", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// Email format
	record1 := &Record{Data: map[string]any{"contact": "user@example.com"}}
	results1 := stage.Route(record1)

	if len(results1) != 1 || results1[0].RouteName != "email" {
		t.Errorf("Expected 'email' route for email format")
	}

	// Phone format
	record2 := &Record{Data: map[string]any{"contact": "123-4567"}}
	results2 := stage.Route(record2)

	if len(results2) != 1 || results2[0].RouteName != "phone" {
		t.Errorf("Expected 'phone' route for phone format")
	}
}

func TestRouterStageNestedField(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "admin", Condition: ".user.role == \"admin\"", Outputs: []string{"admin_handler"}},
		},
	}

	stage, err := NewRouterStage("test_nested", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// Nested field match
	record := &Record{
		Data: map[string]any{
			"user": map[string]any{
				"name": "John",
				"role": "admin",
			},
		},
	}
	results := stage.Route(record)

	if len(results) != 1 || results[0].RouteName != "admin" {
		t.Errorf("Expected 'admin' route for nested field match")
	}
}

func TestRouterStageProcess(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "high", Condition: ".priority >= 8", Outputs: []string{"alert", "kafka"}},
		},
		DefaultRoute: &Route{Name: "_default", Outputs: []string{"default"}},
	}

	stage, err := NewRouterStage("test_process", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	ctx := context.Background()

	// Process record
	record := &Record{Data: map[string]any{"priority": 9.0}}
	result, err := stage.Process(ctx, record)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Check routing metadata was added
	routes, ok := result.Data["_routes"].([]string)
	if !ok || len(routes) == 0 {
		t.Error("Expected _routes in result data")
	}

	outputs, ok := result.Data["_target_outputs"].([]string)
	if !ok || len(outputs) != 2 {
		t.Error("Expected _target_outputs with 2 outputs")
	}
}

func TestRouterStageFromConfig(t *testing.T) {
	config := map[string]any{
		"mode": "condition",
		"routes": []any{
			map[string]any{
				"name":      "urgent",
				"condition": ".priority >= 9",
				"outputs":   []any{"alert"},
			},
			map[string]any{
				"name":      "normal",
				"condition": ".priority < 9",
				"outputs":   []any{"default"},
			},
		},
		"default": map[string]any{
			"outputs": []any{"fallback"},
		},
	}

	stage, err := NewRouterStageFromConfig("test_from_config", config)
	if err != nil {
		t.Fatalf("Failed to create router stage from config: %v", err)
	}

	if stage.Name() != "test_from_config" {
		t.Errorf("Expected name 'test_from_config', got '%s'", stage.Name())
	}

	if stage.Type() != "router" {
		t.Errorf("Expected type 'router', got '%s'", stage.Type())
	}

	// Test routing
	record := &Record{Data: map[string]any{"priority": 10.0}}
	results := stage.Route(record)

	if len(results) != 1 || results[0].RouteName != "urgent" {
		t.Errorf("Expected 'urgent' route")
	}
}

func TestRouterStageMetrics(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "route_a", Condition: ".type == \"a\"", Outputs: []string{"out_a"}},
			{Name: "route_b", Condition: ".type == \"b\"", Outputs: []string{"out_b"}},
		},
	}

	stage, err := NewRouterStage("test_metrics", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// Route some records
	_ = stage.Route(&Record{Data: map[string]any{"type": "a"}})
	_ = stage.Route(&Record{Data: map[string]any{"type": "a"}})
	_ = stage.Route(&Record{Data: map[string]any{"type": "b"}})

	metrics := stage.GetRouteMetrics()

	if metrics["route_a"]["matched"] != 2 {
		t.Errorf("Expected route_a matched=2, got %d", metrics["route_a"]["matched"])
	}

	if metrics["route_b"]["matched"] != 1 {
		t.Errorf("Expected route_b matched=1, got %d", metrics["route_b"]["matched"])
	}
}

func TestRouterStageBooleanCondition(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "active", Condition: ".is_active == true", Outputs: []string{"active_handler"}},
			{Name: "inactive", Condition: ".is_active == false", Outputs: []string{"inactive_handler"}},
		},
	}

	stage, err := NewRouterStage("test_boolean", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// Active record
	record1 := &Record{Data: map[string]any{"is_active": true}}
	results1 := stage.Route(record1)

	if len(results1) != 1 || results1[0].RouteName != "active" {
		t.Errorf("Expected 'active' route for is_active=true")
	}

	// Inactive record
	record2 := &Record{Data: map[string]any{"is_active": false}}
	results2 := stage.Route(record2)

	if len(results2) != 1 || results2[0].RouteName != "inactive" {
		t.Errorf("Expected 'inactive' route for is_active=false")
	}
}

func TestRouterStageNullCondition(t *testing.T) {
	cfg := &RouterStageConfig{
		Mode: RouterModeCondition,
		Routes: []Route{
			{Name: "has_value", Condition: ".value != null", Outputs: []string{"process"}},
			{Name: "no_value", Condition: ".value == null", Outputs: []string{"skip"}},
		},
	}

	stage, err := NewRouterStage("test_null", cfg)
	if err != nil {
		t.Fatalf("Failed to create router stage: %v", err)
	}

	// With value
	record1 := &Record{Data: map[string]any{"value": "something"}}
	results1 := stage.Route(record1)

	if len(results1) != 1 || results1[0].RouteName != "has_value" {
		t.Errorf("Expected 'has_value' route")
	}

	// Without value (null)
	record2 := &Record{Data: map[string]any{"other": "field"}}
	results2 := stage.Route(record2)

	if len(results2) != 1 || results2[0].RouteName != "no_value" {
		t.Errorf("Expected 'no_value' route")
	}
}
