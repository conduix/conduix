// Package output provides dynamic output selection implementation
package output

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// DynamicOutputConfig is the configuration for DynamicOutput
type DynamicOutputConfig struct {
	// OutputField is the record field that determines the target output
	OutputField string `json:"output_field"`

	// Fallback is the default output name when mapping not found
	Fallback string `json:"fallback"`

	// Mapping maps field values to output names
	// e.g., {"es": "elasticsearch_output", "s3": "s3_output"}
	Mapping map[string]string `json:"mapping"`

	// Outputs is a map of output name to output instance
	// This is set programmatically, not from config
	Outputs map[string]Output `json:"-"`

	// ConditionRouting enables condition-based routing instead of field-based
	// When enabled, uses conditions to select outputs
	ConditionRouting bool                     `json:"condition_routing"`
	Conditions       []DynamicOutputCondition `json:"conditions"`
}

// DynamicOutputCondition defines a condition for dynamic routing
type DynamicOutputCondition struct {
	Name       string `json:"name"`
	Condition  string `json:"condition"` // VRL-style condition
	OutputName string `json:"output_name"`
}

// DynamicOutput routes records to different outputs based on record data
type DynamicOutput struct {
	name           string
	outputField    string
	fallback       string
	mapping        map[string]string
	outputs        map[string]Output
	stats          OutputStats
	routeStats     map[string]*int64 // stats per output
	mu             sync.RWMutex
	condRouting    bool
	conditions     []DynamicOutputCondition
	condEvaluators []conditionEval
}

type conditionEval struct {
	condition  string
	outputName string
	evalFunc   func(map[string]any) bool
}

// NewDynamicOutput creates a new dynamic output
func NewDynamicOutput(name string, cfg config.OutputConfig) (*DynamicOutput, error) {
	outputField := "_target_output"
	if field, ok := cfg.Config["output_field"].(string); ok && field != "" {
		outputField = field
	}

	fallback := ""
	if fb, ok := cfg.Config["fallback"].(string); ok {
		fallback = fb
	}

	mapping := make(map[string]string)
	if m, ok := cfg.Config["mapping"].(map[string]any); ok {
		for k, v := range m {
			if vs, ok := v.(string); ok {
				mapping[k] = vs
			}
		}
	}

	d := &DynamicOutput{
		name:        name,
		outputField: outputField,
		fallback:    fallback,
		mapping:     mapping,
		outputs:     make(map[string]Output),
		routeStats:  make(map[string]*int64),
	}

	// Parse condition-based routing if configured
	if condRouting, ok := cfg.Config["condition_routing"].(bool); ok && condRouting {
		d.condRouting = true

		if conditions, ok := cfg.Config["conditions"].([]any); ok {
			for _, c := range conditions {
				if condMap, ok := c.(map[string]any); ok {
					cond := DynamicOutputCondition{}
					if name, ok := condMap["name"].(string); ok {
						cond.Name = name
					}
					if condition, ok := condMap["condition"].(string); ok {
						cond.Condition = condition
					}
					if output, ok := condMap["output_name"].(string); ok {
						cond.OutputName = output
					}
					d.conditions = append(d.conditions, cond)
				}
			}
		}

		// Build condition evaluators
		for _, cond := range d.conditions {
			eval, err := buildConditionEval(cond.Condition, cond.OutputName)
			if err != nil {
				return nil, fmt.Errorf("invalid condition for %s: %w", cond.Name, err)
			}
			d.condEvaluators = append(d.condEvaluators, eval)
		}
	}

	return d, nil
}

// SetOutputs sets the output instances for routing
func (d *DynamicOutput) SetOutputs(outputs map[string]Output) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.outputs = outputs

	// Initialize route stats
	for name := range outputs {
		count := int64(0)
		d.routeStats[name] = &count
	}
}

// AddOutput adds an output instance
func (d *DynamicOutput) AddOutput(name string, output Output) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.outputs[name] = output
	count := int64(0)
	d.routeStats[name] = &count
}

func (d *DynamicOutput) Name() string { return d.name }

func (d *DynamicOutput) Open(ctx context.Context) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Open all registered outputs
	for name, output := range d.outputs {
		if err := output.Open(ctx); err != nil {
			return fmt.Errorf("failed to open output %s: %w", name, err)
		}
	}

	log.Printf("[dynamic_output] Opened with %d outputs, field=%s, fallback=%s",
		len(d.outputs), d.outputField, d.fallback)
	return nil
}

func (d *DynamicOutput) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&d.stats.TotalRecords, 1)

	// Determine target output
	targetOutput := d.resolveOutput(record.Data)
	if targetOutput == "" {
		atomic.AddInt64(&d.stats.ErrorRecords, 1)
		return fmt.Errorf("no target output found for record")
	}

	d.mu.RLock()
	output, ok := d.outputs[targetOutput]
	d.mu.RUnlock()

	if !ok {
		atomic.AddInt64(&d.stats.ErrorRecords, 1)
		return fmt.Errorf("output %s not found", targetOutput)
	}

	// Write to target output
	if err := output.Write(ctx, record); err != nil {
		atomic.AddInt64(&d.stats.ErrorRecords, 1)
		return fmt.Errorf("failed to write to %s: %w", targetOutput, err)
	}

	atomic.AddInt64(&d.stats.SuccessRecords, 1)
	d.stats.LastWriteTime = time.Now()

	// Update route stats
	if stat, ok := d.routeStats[targetOutput]; ok {
		atomic.AddInt64(stat, 1)
	}

	return nil
}

// resolveOutput determines the target output for a record
func (d *DynamicOutput) resolveOutput(data map[string]any) string {
	// Condition-based routing
	if d.condRouting {
		for _, eval := range d.condEvaluators {
			if eval.evalFunc(data) {
				return eval.outputName
			}
		}
		return d.fallback
	}

	// Field-based routing
	fieldValue := d.getFieldValue(data, d.outputField)
	if fieldValue == "" {
		return d.fallback
	}

	// Check mapping
	if mapped, ok := d.mapping[fieldValue]; ok {
		return mapped
	}

	// Use field value directly as output name
	d.mu.RLock()
	_, exists := d.outputs[fieldValue]
	d.mu.RUnlock()

	if exists {
		return fieldValue
	}

	return d.fallback
}

// getFieldValue extracts a field value from record data, supporting nested fields
func (d *DynamicOutput) getFieldValue(data map[string]any, field string) string {
	// Handle _target_outputs array (from router stage)
	if field == "_target_output" || field == "_target_outputs" {
		if outputs, ok := data["_target_outputs"].([]string); ok && len(outputs) > 0 {
			return outputs[0]
		}
		if output, ok := data["_target_output"].(string); ok {
			return output
		}
	}

	parts := strings.Split(field, ".")
	current := any(data)

	for _, part := range parts {
		if current == nil {
			return ""
		}
		if m, ok := current.(map[string]any); ok {
			current = m[part]
		} else {
			return ""
		}
	}

	if str, ok := current.(string); ok {
		return str
	}
	return ""
}

func (d *DynamicOutput) Flush(ctx context.Context) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var lastErr error
	for name, output := range d.outputs {
		if err := output.Flush(ctx); err != nil {
			log.Printf("[dynamic_output] Failed to flush %s: %v", name, err)
			lastErr = err
		}
	}
	return lastErr
}

func (d *DynamicOutput) Close() error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var lastErr error
	for name, output := range d.outputs {
		if err := output.Close(); err != nil {
			log.Printf("[dynamic_output] Failed to close %s: %v", name, err)
			lastErr = err
		}
	}

	// Log route stats
	log.Printf("[dynamic_output] Closed. Total: %d, Success: %d, Errors: %d",
		d.stats.TotalRecords, d.stats.SuccessRecords, d.stats.ErrorRecords)
	for name, count := range d.routeStats {
		log.Printf("[dynamic_output] Route %s: %d records", name, atomic.LoadInt64(count))
	}

	return lastErr
}

func (d *DynamicOutput) Stats() OutputStats {
	return d.stats
}

// GetRouteStats returns statistics per output
func (d *DynamicOutput) GetRouteStats() map[string]int64 {
	result := make(map[string]int64)
	for name, count := range d.routeStats {
		result[name] = atomic.LoadInt64(count)
	}
	return result
}

// buildConditionEval creates a condition evaluator
// Reuses the logic from router_stage for consistency
func buildConditionEval(condition, outputName string) (conditionEval, error) {
	eval := conditionEval{
		condition:  condition,
		outputName: outputName,
	}

	if condition == "" || condition == "true" {
		eval.evalFunc = func(data map[string]any) bool { return true }
		return eval, nil
	}

	// Simple condition parser (similar to router_stage)
	evalFunc, err := buildDynamicConditionFunc(condition)
	if err != nil {
		return eval, err
	}
	eval.evalFunc = evalFunc
	return eval, nil
}

// buildDynamicConditionFunc creates a simple condition evaluator
func buildDynamicConditionFunc(condition string) (func(map[string]any) bool, error) {
	condition = strings.TrimSpace(condition)

	// Handle comparison operators
	operators := []string{"!=", ">=", "<=", "==", ">", "<"}
	for _, op := range operators {
		if strings.Contains(condition, op) {
			parts := strings.SplitN(condition, op, 2)
			if len(parts) != 2 {
				continue
			}

			field := extractDynamicFieldName(strings.TrimSpace(parts[0]))
			valueStr := strings.TrimSpace(parts[1])

			return buildDynamicComparisonFunc(field, op, valueStr)
		}
	}

	// Handle exists
	if strings.HasSuffix(condition, " exists") {
		field := strings.TrimSuffix(condition, " exists")
		field = extractDynamicFieldName(field)
		return func(data map[string]any) bool {
			_, exists := getDynamicNestedValue(data, field)
			return exists
		}, nil
	}

	return nil, fmt.Errorf("unsupported condition: %s", condition)
}

func buildDynamicComparisonFunc(field, op, valueStr string) (func(map[string]any) bool, error) {
	// Handle string values (quoted)
	if (strings.HasPrefix(valueStr, "\"") && strings.HasSuffix(valueStr, "\"")) ||
		(strings.HasPrefix(valueStr, "'") && strings.HasSuffix(valueStr, "'")) {
		strVal := valueStr[1 : len(valueStr)-1]

		switch op {
		case "==":
			return func(data map[string]any) bool {
				val, ok := getDynamicNestedValue(data, field)
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
				val, ok := getDynamicNestedValue(data, field)
				if !ok {
					return true
				}
				if str, ok := val.(string); ok {
					return str != strVal
				}
				return true
			}, nil
		}
	}

	return nil, fmt.Errorf("unsupported comparison: %s %s %s", field, op, valueStr)
}

func extractDynamicFieldName(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimPrefix(s, ".")
}

func getDynamicNestedValue(data map[string]any, field string) (any, bool) {
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
