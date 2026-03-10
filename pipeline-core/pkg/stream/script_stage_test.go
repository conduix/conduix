package stream

import (
	"context"
	"testing"
	"time"
)

func TestScriptStage_BasicProcess(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    record["processed"] = True
    record["upper_name"] = record.get("name", "").upper()
    return record
`,
	}

	stage, err := NewScriptStage("test-basic", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
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

func TestScriptStage_NoneDropsRecord(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    if record.get("level") == "debug":
        return None
    return record
`,
	}

	stage, err := NewScriptStage("test-drop", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
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

func TestScriptStage_Timeout(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    # long loop to trigger timeout
    x = 0
    for i in range(999999999):
        x += 1
    return record
`,
		"timeout": "100ms",
	}

	stage, err := NewScriptStage("test-timeout", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"key": "value"}}
	start := time.Now()
	result, err := stage.Process(context.Background(), record)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	// should return original record on timeout
	if result == nil {
		t.Error("expected original record on timeout, got nil")
	}
	// should complete within reasonable time (timeout + buffer)
	if elapsed > 2*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestScriptStage_CompileError(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record)  # missing colon
    return record
`,
	}

	_, err := NewScriptStage("test-compile-err", config)
	if err == nil {
		t.Error("expected compile error, got nil")
	}
}

func TestScriptStage_MissingProcessFunc(t *testing.T) {
	config := map[string]any{
		"code": `
def transform(record):
    return record
`,
	}

	_, err := NewScriptStage("test-no-process", config)
	if err == nil {
		t.Error("expected error for missing process(), got nil")
	}
}

func TestScriptStage_MissingCode(t *testing.T) {
	config := map[string]any{}
	_, err := NewScriptStage("test-no-code", config)
	if err == nil {
		t.Error("expected error for missing code, got nil")
	}
}

func TestScriptStage_RuntimeError(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    # runtime error: division by zero
    x = 1 / 0
    return record
`,
	}

	stage, err := NewScriptStage("test-runtime-err", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
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

func TestScriptStage_BuiltinHashSHA256(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    record["hash"] = hash_sha256(record.get("value", ""))
    return record
`,
	}

	stage, err := NewScriptStage("test-hash", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"value": "hello"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	hash, ok := result.Data["hash"].(string)
	if !ok || hash == "" {
		t.Error("expected non-empty hash string")
	}
	// SHA256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if hash != expected {
		t.Errorf("expected hash %s, got %s", expected, hash)
	}
}

func TestScriptStage_BuiltinBase64(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    encoded = base64_encode(record["value"])
    decoded = base64_decode(encoded)
    record["encoded"] = encoded
    record["decoded"] = decoded
    return record
`,
	}

	stage, err := NewScriptStage("test-b64", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"value": "hello world"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.Data["encoded"] != "aGVsbG8gd29ybGQ=" {
		t.Errorf("unexpected encoded: %v", result.Data["encoded"])
	}
	if result.Data["decoded"] != "hello world" {
		t.Errorf("unexpected decoded: %v", result.Data["decoded"])
	}
}

func TestScriptStage_BuiltinJSON(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    encoded = json_encode({"key": "value", "num": 42})
    record["json_str"] = encoded
    decoded = json_decode(encoded)
    record["decoded_key"] = decoded["key"]
    return record
`,
	}

	stage, err := NewScriptStage("test-json", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.Data["decoded_key"] != "value" {
		t.Errorf("expected decoded_key=value, got %v", result.Data["decoded_key"])
	}
}

func TestScriptStage_BuiltinRegex(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    record["is_email"] = regex_match(r"^[\w.]+@[\w.]+$", record.get("email", ""))
    record["masked"] = regex_replace(r"\d{4}", record.get("phone", ""), "****")
    return record
`,
	}

	stage, err := NewScriptStage("test-regex", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
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

func TestScriptStage_BuiltinTimestamp(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    record["now"] = timestamp_now()
    record["parsed"] = timestamp_parse("2024-01-15", "2006-01-02")
    return record
`,
	}

	stage, err := NewScriptStage("test-ts", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	now, ok := result.Data["now"].(string)
	if !ok || now == "" {
		t.Error("expected non-empty timestamp_now result")
	}
	parsed, ok := result.Data["parsed"].(string)
	if !ok || parsed != "2024-01-15T00:00:00Z" {
		t.Errorf("expected 2024-01-15T00:00:00Z, got %v", parsed)
	}
}

func TestScriptStage_ContextCancellation(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    x = 0
    for i in range(999999999):
        x += 1
    return record
`,
		"timeout": "10s",
	}

	stage, err := NewScriptStage("test-ctx-cancel", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	record := &Record{Data: map[string]any{"key": "value"}}
	_, err = stage.Process(ctx, record)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestScriptStage_Stats(t *testing.T) {
	config := map[string]any{
		"code": `
def process(record):
    return record
`,
	}

	stage, err := NewScriptStage("test-stats", config)
	if err != nil {
		t.Fatalf("NewScriptStage failed: %v", err)
	}

	for i := 0; i < 5; i++ {
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

func TestScriptStage_NewStageFactory(t *testing.T) {
	cfg := StageConfig{
		Name: "script-test",
		Type: "script",
		Config: map[string]any{
			"code": `
def process(record):
    return record
`,
		},
	}

	stage, err := NewStage(cfg)
	if err != nil {
		t.Fatalf("NewStage(script) failed: %v", err)
	}
	if stage.Type() != "script" {
		t.Errorf("expected type=script, got %s", stage.Type())
	}
	if stage.Name() != "script-test" {
		t.Errorf("expected name=script-test, got %s", stage.Name())
	}
}

func TestScriptStage_RegistryHasScript(t *testing.T) {
	schema, ok := StageRegistry.Get("script")
	if !ok {
		t.Fatal("script stage not found in registry")
	}
	if schema.DisplayName != "Script" {
		t.Errorf("expected DisplayName=Script, got %s", schema.DisplayName)
	}
	if len(schema.Fields) < 2 {
		t.Errorf("expected at least 2 fields (code, timeout), got %d", len(schema.Fields))
	}
}
