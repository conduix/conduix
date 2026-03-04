// Package stream provides conditional routing stage implementation
package stream

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// RouterMode defines how routing works
type RouterMode string

const (
	// RouterModeFanOut copies record to all routes (like multicast)
	RouterModeFanOut RouterMode = "fan_out"
	// RouterModeCondition routes to first matching condition
	RouterModeCondition RouterMode = "condition"
	// RouterModeFilter routes to all matching conditions
	RouterModeFilter RouterMode = "filter"
)

// RouteResult represents the result of routing
type RouteResult struct {
	RouteName string   // matched route name
	Outputs   []string // target output names
	Record    *Record  // potentially modified record
}

// Route represents a single routing rule
type Route struct {
	Name      string   `json:"name"`
	Condition string   `json:"condition"` // VRL-style condition
	Outputs   []string `json:"outputs"`   // target output names
}

// RouterStageConfig is the configuration for RouterStage
type RouterStageConfig struct {
	Mode         RouterMode `json:"mode"`          // fan_out, condition, filter
	Field        string     `json:"field"`         // for simple field-based routing
	Routes       []Route    `json:"routes"`        // routing rules
	DefaultRoute *Route     `json:"default_route"` // fallback route
}

// RouterStage routes records to different outputs based on conditions
type RouterStage struct {
	BaseStage
	mode         RouterMode
	field        string // for simple field-based routing
	routes       []Route
	defaultRoute *Route
	evaluators   []conditionEvaluator

	// Metrics per route
	routeMetrics map[string]*routeMetrics
	metricsMu    sync.RWMutex
}

type routeMetrics struct {
	matched   int64
	processed int64
}

// conditionEvaluator evaluates a condition against record data
type conditionEvaluator struct {
	routeName string
	condition string
	evalFunc  func(data map[string]any) bool
}

// NewRouterStage creates a new router stage
func NewRouterStage(name string, cfg *RouterStageConfig) (*RouterStage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("router stage config is required")
	}

	mode := cfg.Mode
	if mode == "" {
		mode = RouterModeCondition
	}

	s := &RouterStage{
		BaseStage:    BaseStage{name: name, typ: "router", config: map[string]any{}},
		mode:         mode,
		field:        cfg.Field,
		routes:       cfg.Routes,
		defaultRoute: cfg.DefaultRoute,
		routeMetrics: make(map[string]*routeMetrics),
	}

	// Build evaluators for each route
	for _, route := range cfg.Routes {
		eval, err := buildConditionEvaluator(route.Name, route.Condition)
		if err != nil {
			return nil, fmt.Errorf("invalid condition for route %s: %w", route.Name, err)
		}
		s.evaluators = append(s.evaluators, eval)
		s.routeMetrics[route.Name] = &routeMetrics{}
	}

	if cfg.DefaultRoute != nil {
		s.routeMetrics["_default"] = &routeMetrics{}
	}

	return s, nil
}

// NewRouterStageFromConfig creates a router stage from config map
func NewRouterStageFromConfig(name string, config map[string]any) (*RouterStage, error) {
	cfg := &RouterStageConfig{}

	// Parse mode
	if modeStr, ok := config["mode"].(string); ok {
		cfg.Mode = RouterMode(modeStr)
	}

	// Parse field (for simple routing)
	if field, ok := config["field"].(string); ok {
		cfg.Field = field
	}

	// Parse routes
	if routesRaw, ok := config["routes"].([]any); ok {
		for _, r := range routesRaw {
			if routeMap, ok := r.(map[string]any); ok {
				route := Route{}
				if n, ok := routeMap["name"].(string); ok {
					route.Name = n
				}
				if c, ok := routeMap["condition"].(string); ok {
					route.Condition = c
				}
				if outputs, ok := routeMap["outputs"].([]any); ok {
					for _, o := range outputs {
						if os, ok := o.(string); ok {
							route.Outputs = append(route.Outputs, os)
						}
					}
				}
				cfg.Routes = append(cfg.Routes, route)
			}
		}
	}

	// Parse default route
	if defaultRaw, ok := config["default"].(map[string]any); ok {
		cfg.DefaultRoute = &Route{Name: "_default"}
		if outputs, ok := defaultRaw["outputs"].([]any); ok {
			for _, o := range outputs {
				if os, ok := o.(string); ok {
					cfg.DefaultRoute.Outputs = append(cfg.DefaultRoute.Outputs, os)
				}
			}
		}
	}

	return NewRouterStage(name, cfg)
}

// Process routes the record and returns routing results
// Note: Router stage is special - it doesn't transform data but determines routing
// The actual routing is handled by the executor
func (s *RouterStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	// Mark routing metadata in record
	routes := s.Route(record)
	if len(routes) == 0 {
		// No matching route
		return nil, nil
	}

	// Store route info in record metadata
	routeNames := make([]string, 0, len(routes))
	outputNames := make([]string, 0)
	for _, r := range routes {
		routeNames = append(routeNames, r.RouteName)
		outputNames = append(outputNames, r.Outputs...)
	}

	// Add routing info to data (can be used by executor)
	if record.Data == nil {
		record.Data = make(map[string]any)
	}
	record.Data["_routes"] = routeNames
	record.Data["_target_outputs"] = outputNames

	s.incrementOutput()
	return record, nil
}

// Route evaluates all routing conditions and returns matching routes
func (s *RouterStage) Route(record *Record) []RouteResult {
	var results []RouteResult

	switch s.mode {
	case RouterModeFanOut:
		// Copy to all routes
		for _, route := range s.routes {
			results = append(results, RouteResult{
				RouteName: route.Name,
				Outputs:   route.Outputs,
				Record:    record,
			})
			s.incrementRouteMetrics(route.Name)
		}

	case RouterModeCondition:
		// First matching condition only
		for i, eval := range s.evaluators {
			if eval.evalFunc(record.Data) {
				route := s.routes[i]
				results = append(results, RouteResult{
					RouteName: route.Name,
					Outputs:   route.Outputs,
					Record:    record,
				})
				s.incrementRouteMetrics(route.Name)
				return results // Return on first match
			}
		}
		// Use default if no match
		if s.defaultRoute != nil {
			results = append(results, RouteResult{
				RouteName: "_default",
				Outputs:   s.defaultRoute.Outputs,
				Record:    record,
			})
			s.incrementRouteMetrics("_default")
		}

	case RouterModeFilter:
		// All matching conditions
		for i, eval := range s.evaluators {
			if eval.evalFunc(record.Data) {
				route := s.routes[i]
				results = append(results, RouteResult{
					RouteName: route.Name,
					Outputs:   route.Outputs,
					Record:    record,
				})
				s.incrementRouteMetrics(route.Name)
			}
		}
		// If no match, use default
		if len(results) == 0 && s.defaultRoute != nil {
			results = append(results, RouteResult{
				RouteName: "_default",
				Outputs:   s.defaultRoute.Outputs,
				Record:    record,
			})
			s.incrementRouteMetrics("_default")
		}
	}

	return results
}

// GetRouteMetrics returns metrics for all routes
func (s *RouterStage) GetRouteMetrics() map[string]map[string]int64 {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	result := make(map[string]map[string]int64)
	for name, m := range s.routeMetrics {
		result[name] = map[string]int64{
			"matched":   atomic.LoadInt64(&m.matched),
			"processed": atomic.LoadInt64(&m.processed),
		}
	}
	return result
}

func (s *RouterStage) incrementRouteMetrics(routeName string) {
	s.metricsMu.RLock()
	m, ok := s.routeMetrics[routeName]
	s.metricsMu.RUnlock()

	if ok {
		atomic.AddInt64(&m.matched, 1)
		atomic.AddInt64(&m.processed, 1)
	}
}

// buildConditionEvaluator creates an evaluator for a condition string
// Supports VRL-style conditions:
// - .field == "value"
// - .field != "value"
// - .field > 10
// - .field >= 10
// - .field < 10
// - .field <= 10
// - .field exists
// - .field == true/false
// - .field =~ "regex"
// - .field1 == "a" && .field2 > 5
// - .field1 == "a" || .field2 == "b"
func buildConditionEvaluator(routeName, condition string) (conditionEvaluator, error) {
	eval := conditionEvaluator{
		routeName: routeName,
		condition: condition,
	}

	if condition == "" || condition == "true" {
		// Always match
		eval.evalFunc = func(data map[string]any) bool { return true }
		return eval, nil
	}

	// Handle AND conditions
	if strings.Contains(condition, "&&") {
		parts := strings.Split(condition, "&&")
		var subEvals []func(map[string]any) bool

		for _, part := range parts {
			subEval, err := buildSimpleConditionFunc(strings.TrimSpace(part))
			if err != nil {
				return eval, err
			}
			subEvals = append(subEvals, subEval)
		}

		eval.evalFunc = func(data map[string]any) bool {
			for _, subEval := range subEvals {
				if !subEval(data) {
					return false
				}
			}
			return true
		}
		return eval, nil
	}

	// Handle OR conditions
	if strings.Contains(condition, "||") {
		parts := strings.Split(condition, "||")
		var subEvals []func(map[string]any) bool

		for _, part := range parts {
			subEval, err := buildSimpleConditionFunc(strings.TrimSpace(part))
			if err != nil {
				return eval, err
			}
			subEvals = append(subEvals, subEval)
		}

		eval.evalFunc = func(data map[string]any) bool {
			for _, subEval := range subEvals {
				if subEval(data) {
					return true
				}
			}
			return false
		}
		return eval, nil
	}

	// Simple condition
	simpleFunc, err := buildSimpleConditionFunc(condition)
	if err != nil {
		return eval, err
	}
	eval.evalFunc = simpleFunc
	return eval, nil
}

// buildSimpleConditionFunc creates evaluator for a simple condition (no && or ||)
func buildSimpleConditionFunc(condition string) (func(map[string]any) bool, error) {
	condition = strings.TrimSpace(condition)

	// Handle "not exists" operator (must check before "exists")
	if strings.HasSuffix(condition, " not exists") {
		field := strings.TrimSuffix(condition, " not exists")
		field = extractFieldName(field)
		return func(data map[string]any) bool {
			_, exists := getNestedValue(data, field)
			return !exists
		}, nil
	}

	// Handle "exists" operator
	if strings.HasSuffix(condition, " exists") {
		field := strings.TrimSuffix(condition, " exists")
		field = extractFieldName(field)
		return func(data map[string]any) bool {
			_, exists := getNestedValue(data, field)
			return exists
		}, nil
	}

	// Handle regex match
	if strings.Contains(condition, " =~ ") {
		parts := strings.SplitN(condition, " =~ ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid regex condition: %s", condition)
		}
		field := extractFieldName(parts[0])
		pattern := strings.Trim(parts[1], "\"'")

		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %w", err)
		}

		return func(data map[string]any) bool {
			val, ok := getNestedValue(data, field)
			if !ok {
				return false
			}
			if str, ok := val.(string); ok {
				return re.MatchString(str)
			}
			return false
		}, nil
	}

	// Handle comparison operators
	operators := []string{"!=", ">=", "<=", "==", ">", "<"}
	for _, op := range operators {
		if strings.Contains(condition, op) {
			parts := strings.SplitN(condition, op, 2)
			if len(parts) != 2 {
				continue
			}

			field := extractFieldName(strings.TrimSpace(parts[0]))
			valueStr := strings.TrimSpace(parts[1])

			return buildComparisonFunc(field, op, valueStr)
		}
	}

	return nil, fmt.Errorf("unsupported condition format: %s", condition)
}

// buildComparisonFunc creates a comparison function
func buildComparisonFunc(field, op, valueStr string) (func(map[string]any) bool, error) {
	// Handle boolean values
	if valueStr == "true" || valueStr == "false" {
		boolVal := valueStr == "true"
		switch op {
		case "==":
			return func(data map[string]any) bool {
				val, ok := getNestedValue(data, field)
				if !ok {
					return false
				}
				if b, ok := val.(bool); ok {
					return b == boolVal
				}
				return false
			}, nil
		case "!=":
			return func(data map[string]any) bool {
				val, ok := getNestedValue(data, field)
				if !ok {
					return true
				}
				if b, ok := val.(bool); ok {
					return b != boolVal
				}
				return true
			}, nil
		}
	}

	// Handle null value
	if valueStr == "null" || valueStr == "nil" {
		switch op {
		case "==":
			return func(data map[string]any) bool {
				val, ok := getNestedValue(data, field)
				return !ok || val == nil
			}, nil
		case "!=":
			return func(data map[string]any) bool {
				val, ok := getNestedValue(data, field)
				return ok && val != nil
			}, nil
		}
	}

	// Handle string values (quoted)
	if (strings.HasPrefix(valueStr, "\"") && strings.HasSuffix(valueStr, "\"")) ||
		(strings.HasPrefix(valueStr, "'") && strings.HasSuffix(valueStr, "'")) {
		strVal := valueStr[1 : len(valueStr)-1]

		switch op {
		case "==":
			return func(data map[string]any) bool {
				val, ok := getNestedValue(data, field)
				if !ok {
					return false
				}
				if str, ok := val.(string); ok {
					return str == strVal
				}
				return false
			}, nil
		case "!=":
			return func(data map[string]any) bool {
				val, ok := getNestedValue(data, field)
				if !ok {
					return true
				}
				if str, ok := val.(string); ok {
					return str != strVal
				}
				return true
			}, nil
		default:
			return nil, fmt.Errorf("operator %s not supported for string comparison", op)
		}
	}

	// Handle numeric values
	numVal, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse value as number: %s", valueStr)
	}

	return func(data map[string]any) bool {
		val, ok := getNestedValue(data, field)
		if !ok {
			return false
		}

		var fieldVal float64
		switch v := val.(type) {
		case float64:
			fieldVal = v
		case float32:
			fieldVal = float64(v)
		case int:
			fieldVal = float64(v)
		case int64:
			fieldVal = float64(v)
		case int32:
			fieldVal = float64(v)
		default:
			return false
		}

		switch op {
		case "==":
			return fieldVal == numVal
		case "!=":
			return fieldVal != numVal
		case ">":
			return fieldVal > numVal
		case ">=":
			return fieldVal >= numVal
		case "<":
			return fieldVal < numVal
		case "<=":
			return fieldVal <= numVal
		default:
			return false
		}
	}, nil
}

// extractFieldName extracts field name from VRL-style notation (.field or .field.nested)
func extractFieldName(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimPrefix(s, ".")
}

// getNestedValue retrieves a value from nested map using dot notation
func getNestedValue(data map[string]any, field string) (any, bool) {
	parts := strings.Split(field, ".")
	current := any(data)

	for _, part := range parts {
		if current == nil {
			return nil, false
		}

		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		val, exists := m[part]
		if !exists {
			return nil, false
		}
		current = val
	}

	return current, true
}
