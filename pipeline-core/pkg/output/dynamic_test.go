package output

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// mockOutput is a simple mock output for testing
type mockOutput struct {
	name        string
	records     []source.Record
	openCalled  bool
	closeCalled bool
	mu          sync.Mutex
}

func newMockOutput(name string) *mockOutput {
	return &mockOutput{name: name}
}

func (m *mockOutput) Name() string { return m.name }

func (m *mockOutput) Open(ctx context.Context) error {
	m.openCalled = true
	return nil
}

func (m *mockOutput) Write(ctx context.Context, record source.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, record)
	return nil
}

func (m *mockOutput) Flush(ctx context.Context) error {
	return nil
}

func (m *mockOutput) Close() error {
	m.closeCalled = true
	return nil
}

func (m *mockOutput) Stats() OutputStats {
	return OutputStats{TotalRecords: int64(len(m.records))}
}

func TestNewDynamicOutput(t *testing.T) {
	tests := []struct {
		name    string
		outName string
		cfg     config.OutputConfig
		wantErr bool
	}{
		{
			name:    "valid config",
			outName: "dynamic_output",
			cfg: config.OutputConfig{
				Type: "dynamic",
				Config: map[string]interface{}{
					"output_field": "_target",
					"fallback":     "default_output",
					"mapping": map[string]interface{}{
						"es":    "elasticsearch_output",
						"kafka": "kafka_output",
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "default output_field",
			outName: "dynamic_output",
			cfg: config.OutputConfig{
				Type:   "dynamic",
				Config: map[string]interface{}{},
			},
			wantErr: false,
		},
		{
			name:    "with condition routing",
			outName: "dynamic_output",
			cfg: config.OutputConfig{
				Type: "dynamic",
				Config: map[string]interface{}{
					"condition_routing": true,
					"fallback":          "default",
					"conditions": []interface{}{
						map[string]interface{}{
							"name":        "urgent",
							"condition":   ".priority == \"high\"",
							"output_name": "alert_output",
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := NewDynamicOutput(tt.outName, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDynamicOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && out == nil {
				t.Error("NewDynamicOutput() returned nil output")
			}
		})
	}
}

func TestDynamicOutput_FieldBasedRouting(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "dynamic",
		Config: map[string]interface{}{
			"output_field": "target",
			"fallback":     "default_output",
			"mapping": map[string]interface{}{
				"es":    "elasticsearch_output",
				"kafka": "kafka_output",
			},
		},
	}

	dyn, err := NewDynamicOutput("dynamic_output", cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic output: %v", err)
	}

	// Create mock outputs
	esOutput := newMockOutput("elasticsearch_output")
	kafkaOutput := newMockOutput("kafka_output")
	defaultOutput := newMockOutput("default_output")

	dyn.SetOutputs(map[string]Output{
		"elasticsearch_output": esOutput,
		"kafka_output":         kafkaOutput,
		"default_output":       defaultOutput,
	})

	ctx := context.Background()
	if err := dyn.Open(ctx); err != nil {
		t.Fatalf("Failed to open: %v", err)
	}

	// Write records with different targets
	records := []source.Record{
		{Data: map[string]interface{}{"target": "es", "msg": "to es"}},
		{Data: map[string]interface{}{"target": "kafka", "msg": "to kafka"}},
		{Data: map[string]interface{}{"target": "unknown", "msg": "to default"}},
		{Data: map[string]interface{}{"msg": "no target, to default"}},
	}

	for _, r := range records {
		if err := dyn.Write(ctx, r); err != nil {
			t.Errorf("Write failed: %v", err)
		}
	}

	// Verify routing
	if len(esOutput.records) != 1 {
		t.Errorf("Expected 1 record to ES, got %d", len(esOutput.records))
	}
	if len(kafkaOutput.records) != 1 {
		t.Errorf("Expected 1 record to Kafka, got %d", len(kafkaOutput.records))
	}
	if len(defaultOutput.records) != 2 {
		t.Errorf("Expected 2 records to default, got %d", len(defaultOutput.records))
	}

	// Verify stats
	stats := dyn.GetRouteStats()
	if stats["elasticsearch_output"] != 1 {
		t.Errorf("Expected ES route count 1, got %d", stats["elasticsearch_output"])
	}

	if err := dyn.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestDynamicOutput_DirectOutputName(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "dynamic",
		Config: map[string]interface{}{
			"output_field": "target",
			"fallback":     "default_output",
		},
	}

	dyn, err := NewDynamicOutput("dynamic_output", cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic output: %v", err)
	}

	// Create mock outputs
	esOutput := newMockOutput("es_output")
	defaultOutput := newMockOutput("default_output")

	dyn.SetOutputs(map[string]Output{
		"es_output":      esOutput,
		"default_output": defaultOutput,
	})

	ctx := context.Background()
	_ = dyn.Open(ctx)

	// Record with direct output name (not mapped)
	record := source.Record{Data: map[string]interface{}{"target": "es_output", "msg": "direct"}}
	if err := dyn.Write(ctx, record); err != nil {
		t.Errorf("Write failed: %v", err)
	}

	if len(esOutput.records) != 1 {
		t.Errorf("Expected 1 record to ES output, got %d", len(esOutput.records))
	}

	_ = dyn.Close()
}

func TestDynamicOutput_RouterStageIntegration(t *testing.T) {
	// Test integration with router stage output (_target_outputs field)
	cfg := config.OutputConfig{
		Type: "dynamic",
		Config: map[string]interface{}{
			"output_field": "_target_outputs",
			"fallback":     "default_output",
		},
	}

	dyn, err := NewDynamicOutput("dynamic_output", cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic output: %v", err)
	}

	alertOutput := newMockOutput("alert_output")
	defaultOutput := newMockOutput("default_output")

	dyn.SetOutputs(map[string]Output{
		"alert_output":   alertOutput,
		"default_output": defaultOutput,
	})

	ctx := context.Background()
	_ = dyn.Open(ctx)

	// Simulating router stage output
	record := source.Record{
		Data: map[string]interface{}{
			"_routes":         []string{"high_priority"},
			"_target_outputs": []string{"alert_output", "kafka_output"},
			"message":         "important event",
		},
	}

	if err := dyn.Write(ctx, record); err != nil {
		t.Errorf("Write failed: %v", err)
	}

	// Should route to first target output (alert_output)
	if len(alertOutput.records) != 1 {
		t.Errorf("Expected 1 record to alert output, got %d", len(alertOutput.records))
	}

	_ = dyn.Close()
}

func TestDynamicOutput_ConditionBasedRouting(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "dynamic",
		Config: map[string]interface{}{
			"condition_routing": true,
			"fallback":          "default_output",
			"conditions": []interface{}{
				map[string]interface{}{
					"name":        "urgent",
					"condition":   ".priority == \"high\"",
					"output_name": "alert_output",
				},
				map[string]interface{}{
					"name":        "normal",
					"condition":   ".priority == \"normal\"",
					"output_name": "log_output",
				},
			},
		},
	}

	dyn, err := NewDynamicOutput("dynamic_output", cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic output: %v", err)
	}

	alertOutput := newMockOutput("alert_output")
	logOutput := newMockOutput("log_output")
	defaultOutput := newMockOutput("default_output")

	dyn.SetOutputs(map[string]Output{
		"alert_output":   alertOutput,
		"log_output":     logOutput,
		"default_output": defaultOutput,
	})

	ctx := context.Background()
	_ = dyn.Open(ctx)

	// Write records
	records := []source.Record{
		{Data: map[string]interface{}{"priority": "high", "msg": "urgent"}},
		{Data: map[string]interface{}{"priority": "normal", "msg": "normal"}},
		{Data: map[string]interface{}{"priority": "low", "msg": "low priority"}},
	}

	for _, r := range records {
		if err := dyn.Write(ctx, r); err != nil {
			t.Errorf("Write failed: %v", err)
		}
	}

	// Verify routing
	if len(alertOutput.records) != 1 {
		t.Errorf("Expected 1 record to alert, got %d", len(alertOutput.records))
	}
	if len(logOutput.records) != 1 {
		t.Errorf("Expected 1 record to log, got %d", len(logOutput.records))
	}
	if len(defaultOutput.records) != 1 {
		t.Errorf("Expected 1 record to default, got %d", len(defaultOutput.records))
	}

	_ = dyn.Close()
}

func TestDynamicOutput_NestedField(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "dynamic",
		Config: map[string]interface{}{
			"output_field": "meta.destination",
			"fallback":     "default_output",
		},
	}

	dyn, err := NewDynamicOutput("dynamic_output", cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic output: %v", err)
	}

	s3Output := newMockOutput("s3_output")
	defaultOutput := newMockOutput("default_output")

	dyn.SetOutputs(map[string]Output{
		"s3_output":      s3Output,
		"default_output": defaultOutput,
	})

	ctx := context.Background()
	_ = dyn.Open(ctx)

	// Record with nested field
	record := source.Record{
		Data: map[string]interface{}{
			"meta": map[string]interface{}{
				"destination": "s3_output",
			},
			"msg": "to s3",
		},
	}

	if err := dyn.Write(ctx, record); err != nil {
		t.Errorf("Write failed: %v", err)
	}

	if len(s3Output.records) != 1 {
		t.Errorf("Expected 1 record to S3, got %d", len(s3Output.records))
	}

	_ = dyn.Close()
}

func TestDynamicOutput_Concurrent(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "dynamic",
		Config: map[string]interface{}{
			"output_field": "target",
			"fallback":     "default_output",
		},
	}

	dyn, err := NewDynamicOutput("dynamic_output", cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic output: %v", err)
	}

	output1 := newMockOutput("output1")
	output2 := newMockOutput("output2")
	defaultOutput := newMockOutput("default_output")

	dyn.SetOutputs(map[string]Output{
		"output1":        output1,
		"output2":        output2,
		"default_output": defaultOutput,
	})

	ctx := context.Background()
	_ = dyn.Open(ctx)

	// Concurrent writes
	var counter int64
	done := make(chan bool)
	numGoroutines := 10
	recordsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			target := "output1"
			if id%2 == 0 {
				target = "output2"
			}
			for j := 0; j < recordsPerGoroutine; j++ {
				record := source.Record{
					Data: map[string]interface{}{
						"target": target,
						"id":     atomic.AddInt64(&counter, 1),
					},
				}
				_ = dyn.Write(ctx, record)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	totalRecords := len(output1.records) + len(output2.records)
	expectedTotal := numGoroutines * recordsPerGoroutine

	if totalRecords != expectedTotal {
		t.Errorf("Expected %d total records, got %d", expectedTotal, totalRecords)
	}

	_ = dyn.Close()
}

func TestDynamicOutput_AddOutput(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "dynamic",
		Config: map[string]interface{}{
			"output_field": "target",
		},
	}

	dyn, err := NewDynamicOutput("dynamic_output", cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic output: %v", err)
	}

	// Add outputs one by one
	output1 := newMockOutput("output1")
	output2 := newMockOutput("output2")

	dyn.AddOutput("output1", output1)
	dyn.AddOutput("output2", output2)

	ctx := context.Background()
	_ = dyn.Open(ctx)

	record := source.Record{Data: map[string]interface{}{"target": "output1"}}
	if err := dyn.Write(ctx, record); err != nil {
		t.Errorf("Write failed: %v", err)
	}

	if len(output1.records) != 1 {
		t.Errorf("Expected 1 record to output1, got %d", len(output1.records))
	}

	_ = dyn.Close()
}
