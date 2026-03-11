package stream

import (
	"context"
	"testing"
	"time"
)

func TestJSScriptStage_BasicProcess(t *testing.T) {
	config := map[string]any{
		"code": `
function process(record) {
    record.processed = true;
    record.upper_name = (record.name || "").toUpperCase();
    return record;
}
`,
	}

	stage, err := NewJSScriptStage("test-basic", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	record := &Record{
		Data: map[string]any{
			"name":  "hello",
			"value": 42,
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected record, got nil")
	}
	if result.Data["processed"] != true {
		t.Errorf("expected processed=true, got %v", result.Data["processed"])
	}
	if result.Data["upper_name"] != "HELLO" {
		t.Errorf("expected upper_name=HELLO, got %v", result.Data["upper_name"])
	}
}

func TestJSScriptStage_NullDropsRecord(t *testing.T) {
	config := map[string]any{
		"code": `
function process(record) {
    if (record.level === "debug") return null;
    return record;
}
`,
	}

	stage, err := NewJSScriptStage("test-drop", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	// debug → drop
	record := &Record{Data: map[string]any{"level": "debug"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result != nil {
		t.Error("expected nil (drop), got record")
	}

	// info → pass
	record2 := &Record{Data: map[string]any{"level": "info"}}
	result2, err := stage.Process(context.Background(), record2)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result2 == nil {
		t.Error("expected record, got nil")
	}
}

func TestJSScriptStage_ArrowFunction(t *testing.T) {
	config := map[string]any{
		"code": `
const process = (record) => {
    record.tags = (record.tags || []).map(t => t.toLowerCase());
    return record;
};
`,
	}

	stage, err := NewJSScriptStage("test-arrow", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"tags": []any{"Hello", "WORLD", "Go"},
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	tags, ok := result.Data["tags"].([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result.Data["tags"])
	}
	expected := []string{"hello", "world", "go"}
	for i, tag := range tags {
		if tag != expected[i] {
			t.Errorf("tags[%d]: expected %q, got %v", i, expected[i], tag)
		}
	}
}

func TestJSScriptStage_JSONParseStringify(t *testing.T) {
	config := map[string]any{
		"code": `
function process(record) {
    const parsed = JSON.parse(record.raw_json);
    record.parsed_name = parsed.name;
    record.json_out = JSON.stringify({key: "value", num: 42});
    return record;
}
`,
	}

	stage, err := NewJSScriptStage("test-json", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"raw_json": `{"name":"test","count":10}`,
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result.Data["parsed_name"] != "test" {
		t.Errorf("expected parsed_name=test, got %v", result.Data["parsed_name"])
	}
}

func TestJSScriptStage_RegExp(t *testing.T) {
	config := map[string]any{
		"code": `
function process(record) {
    record.is_email = /^[\w.]+@[\w.]+$/.test(record.email || "");
    record.masked = (record.phone || "").replace(/\d{4}/g, "****");
    return record;
}
`,
	}

	stage, err := NewJSScriptStage("test-regex", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"email": "user@example.com",
		"phone": "010-1234-5678",
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result.Data["is_email"] != true {
		t.Errorf("expected is_email=true, got %v", result.Data["is_email"])
	}
	if result.Data["masked"] != "010-****-****" {
		t.Errorf("expected 010-****-****, got %v", result.Data["masked"])
	}
}

func TestJSScriptStage_DateMath(t *testing.T) {
	config := map[string]any{
		"code": `
function process(record) {
    record.now = new Date().toISOString();
    record.rounded = Math.round(record.score * 100) / 100;
    return record;
}
`,
	}

	stage, err := NewJSScriptStage("test-date-math", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"score": 3.14159}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	now, ok := result.Data["now"].(string)
	if !ok || now == "" {
		t.Error("expected non-empty ISO timestamp")
	}
	rounded, ok := result.Data["rounded"].(float64)
	if !ok || rounded != 3.14 {
		t.Errorf("expected 3.14, got %v", result.Data["rounded"])
	}
}

func TestJSScriptStage_Timeout(t *testing.T) {
	config := map[string]any{
		"code": `
function process(record) {
    while(true) {} // infinite loop
    return record;
}
`,
		"timeout": "100ms",
	}

	stage, err := NewJSScriptStage("test-timeout", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"key": "value"}}
	start := time.Now()
	result, err := stage.Process(context.Background(), record)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if result == nil {
		t.Error("expected original record on timeout, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestJSScriptStage_CompileError(t *testing.T) {
	config := map[string]any{
		"code": `function process(record { return record; }`, // missing )
	}

	_, err := NewJSScriptStage("test-compile-err", config)
	if err == nil {
		t.Error("expected compile error, got nil")
	}
}

func TestJSScriptStage_MissingProcessFunc(t *testing.T) {
	config := map[string]any{
		"code": `function transform(record) { return record; }`,
	}

	_, err := NewJSScriptStage("test-no-process", config)
	if err == nil {
		t.Error("expected error for missing process(), got nil")
	}
}

func TestJSScriptStage_MissingCode(t *testing.T) {
	config := map[string]any{}
	_, err := NewJSScriptStage("test-no-code", config)
	if err == nil {
		t.Error("expected error for missing code, got nil")
	}
}

func TestJSScriptStage_RuntimeError(t *testing.T) {
	config := map[string]any{
		"code": `
function process(record) {
    // runtime error: undefined is not a function
    record.x.y.z();
    return record;
}
`,
	}

	stage, err := NewJSScriptStage("test-runtime-err", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"key": "value"}}
	result, err := stage.Process(context.Background(), record)

	// error → passthrough (returns original record, no error)
	if err != nil {
		t.Errorf("expected nil error (passthrough), got %v", err)
	}
	if result == nil {
		t.Error("expected original record on error, got nil")
	}
}

func TestJSScriptStage_ContextCancellation(t *testing.T) {
	config := map[string]any{
		"code": `
function process(record) {
    while(true) {}
    return record;
}
`,
		"timeout": "10s",
	}

	stage, err := NewJSScriptStage("test-ctx-cancel", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	record := &Record{Data: map[string]any{"key": "value"}}
	_, err = stage.Process(ctx, record)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestJSScriptStage_Stats(t *testing.T) {
	config := map[string]any{
		"code": `function process(record) { return record; }`,
	}

	stage, err := NewJSScriptStage("test-stats", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	for i := range 5 {
		record := &Record{Data: map[string]any{"i": i}}
		_, _ = stage.Process(context.Background(), record)
	}

	input, output, errors := stage.Stats()
	if input != 5 {
		t.Errorf("expected input=5, got %d", input)
	}
	if output != 5 {
		t.Errorf("expected output=5, got %d", output)
	}
	if errors != 0 {
		t.Errorf("expected errors=0, got %d", errors)
	}
}

func TestJSScriptStage_ES6Features(t *testing.T) {
	config := map[string]any{
		"code": `
function process(record) {
    // destructuring
    const { first, last } = record;

    // template literal
    record.full_name = ` + "`${first} ${last}`" + `;

    // spread
    const nums = [1, 2, 3];
    record.sum = [...nums].reduce((a, b) => a + b, 0);

    // Map
    const m = new Map([["a", 1], ["b", 2]]);
    record.map_size = m.size;

    return record;
}
`,
	}

	stage, err := NewJSScriptStage("test-es6", config)
	if err != nil {
		t.Fatalf("NewJSScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"first": "John", "last": "Doe"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result.Data["full_name"] != "John Doe" {
		t.Errorf("expected 'John Doe', got %v", result.Data["full_name"])
	}
}

func TestJSScriptStage_NewStageFactory(t *testing.T) {
	cfg := StageConfig{
		Name: "js-test",
		Type: "js_script",
		Config: map[string]any{
			"code": `function process(record) { return record; }`,
		},
	}

	stage, err := NewStage(cfg)
	if err != nil {
		t.Fatalf("NewStage(js_script) failed: %v", err)
	}
	if stage.Type() != "js_script" {
		t.Errorf("expected type=js_script, got %s", stage.Type())
	}
}
