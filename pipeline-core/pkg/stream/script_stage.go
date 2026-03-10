package stream

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sync"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

// ScriptStage executes Starlark scripts for record transformation.
// Starlark is sandboxed by design — no file, network, or system access.
type ScriptStage struct {
	BaseStage
	code    string
	timeout time.Duration

	// compiled Starlark thread + globals, per-goroutine via pool
	pool sync.Pool
}

// scriptEnv holds a pre-compiled Starlark environment for reuse.
type scriptEnv struct {
	thread  *starlark.Thread
	globals starlark.StringDict
}

// NewScriptStage creates a ScriptStage from config.
// config keys:
//   - code (string, required): Starlark source with def process(record)
//   - timeout (string, optional): per-record timeout, default "1s"
//   - on_error (string, optional): "passthrough" (default) or "drop"
func NewScriptStage(name string, config map[string]any) (*ScriptStage, error) {
	code, ok := config["code"].(string)
	if !ok || code == "" {
		return nil, fmt.Errorf("script stage %q: 'code' is required", name)
	}

	timeout := 1 * time.Second
	if t, ok := config["timeout"].(string); ok {
		parsed, err := time.ParseDuration(t)
		if err == nil {
			timeout = parsed
		}
	}

	s := &ScriptStage{
		BaseStage: BaseStage{name: name, typ: "script", config: config},
		code:      code,
		timeout:   timeout,
	}

	// validate: compile once to check syntax
	if _, err := s.compile(); err != nil {
		return nil, fmt.Errorf("script stage %q: compile error: %w", name, err)
	}

	s.pool = sync.Pool{
		New: func() any {
			env, err := s.compile()
			if err != nil {
				return nil // should not happen after initial validation
			}
			return env
		},
	}

	return s, nil
}

// compile creates a new Starlark environment with built-in functions.
func (s *ScriptStage) compile() (*scriptEnv, error) {
	thread := &starlark.Thread{
		Name: s.name,
		Print: func(_ *starlark.Thread, msg string) {
			log.Printf("[script:%s] %s", s.name, msg)
		},
	}

	predeclared := s.builtinFunctions()

	globals, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, s.name+".star", s.code, predeclared)
	if err != nil {
		return nil, err
	}

	// verify process function exists
	if _, ok := globals["process"]; !ok {
		return nil, fmt.Errorf("script must define a 'process(record)' function")
	}

	return &scriptEnv{thread: thread, globals: globals}, nil
}

// Process executes the Starlark process(record) function.
// Returns nil to drop the record (when script returns None).
func (s *ScriptStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	envAny := s.pool.Get()
	if envAny == nil {
		s.incrementError()
		return record, fmt.Errorf("script stage %q: failed to create starlark env", s.name)
	}
	env := envAny.(*scriptEnv)
	defer s.pool.Put(env)

	// convert record.Data to starlark dict
	inputDict := goMapToStarlark(record.Data)

	processFn := env.globals["process"]

	// set timeout via context cancellation
	env.thread.SetLocal("context", ctx)
	done := make(chan struct{})
	var result starlark.Value
	var callErr error

	go func() {
		defer close(done)
		result, callErr = starlark.Call(env.thread, processFn, starlark.Tuple{inputDict}, nil)
	}()

	select {
	case <-done:
		// completed
	case <-time.After(s.timeout):
		env.thread.Cancel("timeout")
		<-done
		s.incrementError()
		return record, fmt.Errorf("script stage %q: execution timeout (%v)", s.name, s.timeout)
	case <-ctx.Done():
		env.thread.Cancel("context cancelled")
		<-done
		return nil, ctx.Err()
	}

	if callErr != nil {
		s.incrementError()
		// on error: pass through original record (default behavior)
		log.Printf("[script:%s] error: %v", s.name, callErr)
		return record, nil
	}

	// None → drop record
	if result == starlark.None {
		return nil, nil
	}

	// convert result back to Go map
	resultMap, err := starlarkToGoMap(result)
	if err != nil {
		s.incrementError()
		log.Printf("[script:%s] result conversion error: %v", s.name, err)
		return record, nil
	}

	record.Data = resultMap
	s.incrementOutput()
	return record, nil
}

// builtinFunctions returns predeclared Starlark built-in functions.
func (s *ScriptStage) builtinFunctions() starlark.StringDict {
	return starlark.StringDict{
		"hash_sha256": starlark.NewBuiltin("hash_sha256", builtinHashSHA256),
		"base64_encode": starlark.NewBuiltin("base64_encode", builtinBase64Encode),
		"base64_decode": starlark.NewBuiltin("base64_decode", builtinBase64Decode),
		"json_encode": starlark.NewBuiltin("json_encode", builtinJSONEncode),
		"json_decode": starlark.NewBuiltin("json_decode", builtinJSONDecode),
		"regex_match": starlark.NewBuiltin("regex_match", builtinRegexMatch),
		"regex_replace": starlark.NewBuiltin("regex_replace", builtinRegexReplace),
		"timestamp_now": starlark.NewBuiltin("timestamp_now", builtinTimestampNow),
		"timestamp_parse": starlark.NewBuiltin("timestamp_parse", builtinTimestampParse),
		"log": starlark.NewBuiltin("log", builtinLog),
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}
}

// --- Built-in function implementations ---

func builtinHashSHA256(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var s starlark.String
	if err := starlark.UnpackPositionalArgs("hash_sha256", args, nil, 1, &s); err != nil {
		return nil, err
	}
	h := sha256.Sum256([]byte(string(s)))
	return starlark.String(hex.EncodeToString(h[:])), nil
}

func builtinBase64Encode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var s starlark.String
	if err := starlark.UnpackPositionalArgs("base64_encode", args, nil, 1, &s); err != nil {
		return nil, err
	}
	return starlark.String(base64.StdEncoding.EncodeToString([]byte(string(s)))), nil
}

func builtinBase64Decode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var s starlark.String
	if err := starlark.UnpackPositionalArgs("base64_decode", args, nil, 1, &s); err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(string(s))
	if err != nil {
		return starlark.None, fmt.Errorf("base64_decode: %w", err)
	}
	return starlark.String(decoded), nil
}

func builtinJSONEncode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if args.Len() != 1 {
		return nil, fmt.Errorf("json_encode: expected 1 argument, got %d", args.Len())
	}
	goVal := starlarkToGo(args.Index(0))
	data, err := json.Marshal(goVal)
	if err != nil {
		return starlark.None, fmt.Errorf("json_encode: %w", err)
	}
	return starlark.String(string(data)), nil
}

func builtinJSONDecode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var s starlark.String
	if err := starlark.UnpackPositionalArgs("json_decode", args, nil, 1, &s); err != nil {
		return nil, err
	}
	var result any
	if err := json.Unmarshal([]byte(string(s)), &result); err != nil {
		return starlark.None, fmt.Errorf("json_decode: %w", err)
	}
	return goToStarlark(result), nil
}

func builtinRegexMatch(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var pattern, s starlark.String
	if err := starlark.UnpackPositionalArgs("regex_match", args, nil, 2, &pattern, &s); err != nil {
		return nil, err
	}
	matched, err := regexp.MatchString(string(pattern), string(s))
	if err != nil {
		return starlark.False, fmt.Errorf("regex_match: %w", err)
	}
	return starlark.Bool(matched), nil
}

func builtinRegexReplace(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var pattern, s, replacement starlark.String
	if err := starlark.UnpackPositionalArgs("regex_replace", args, nil, 3, &pattern, &s, &replacement); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(string(pattern))
	if err != nil {
		return starlark.String(string(s)), fmt.Errorf("regex_replace: %w", err)
	}
	return starlark.String(re.ReplaceAllString(string(s), string(replacement))), nil
}

func builtinTimestampNow(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	return starlark.String(time.Now().UTC().Format(time.RFC3339)), nil
}

func builtinTimestampParse(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var s, format starlark.String
	if err := starlark.UnpackPositionalArgs("timestamp_parse", args, nil, 2, &s, &format); err != nil {
		return nil, err
	}
	t, err := time.Parse(string(format), string(s))
	if err != nil {
		return starlark.None, fmt.Errorf("timestamp_parse: %w", err)
	}
	return starlark.String(t.UTC().Format(time.RFC3339)), nil
}

func builtinLog(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var level, message starlark.String
	if err := starlark.UnpackPositionalArgs("log", args, nil, 2, &level, &message); err != nil {
		return nil, err
	}
	log.Printf("[script:%s][%s] %s", thread.Name, string(level), string(message))
	return starlark.None, nil
}

// --- Type conversion helpers ---

// goMapToStarlark converts a Go map to a Starlark dict.
func goMapToStarlark(m map[string]any) *starlark.Dict {
	d := starlark.NewDict(len(m))
	for k, v := range m {
		_ = d.SetKey(starlark.String(k), goToStarlark(v))
	}
	return d
}

// goToStarlark converts a Go value to a Starlark value.
func goToStarlark(v any) starlark.Value {
	switch val := v.(type) {
	case nil:
		return starlark.None
	case bool:
		return starlark.Bool(val)
	case int:
		return starlark.MakeInt(val)
	case int64:
		return starlark.MakeInt64(val)
	case float64:
		return starlark.Float(val)
	case string:
		return starlark.String(val)
	case map[string]any:
		return goMapToStarlark(val)
	case []any:
		elems := make([]starlark.Value, len(val))
		for i, e := range val {
			elems[i] = goToStarlark(e)
		}
		return starlark.NewList(elems)
	default:
		return starlark.String(fmt.Sprintf("%v", val))
	}
}

// starlarkToGoMap converts a Starlark value to a Go map.
func starlarkToGoMap(v starlark.Value) (map[string]any, error) {
	dict, ok := v.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("expected dict, got %s", v.Type())
	}
	result := make(map[string]any, dict.Len())
	for _, item := range dict.Items() {
		key, ok := starlark.AsString(item[0])
		if !ok {
			continue
		}
		result[key] = starlarkToGo(item[1])
	}
	return result, nil
}

// starlarkToGo converts a Starlark value to a Go value.
func starlarkToGo(v starlark.Value) any {
	switch val := v.(type) {
	case starlark.NoneType:
		return nil
	case starlark.Bool:
		return bool(val)
	case starlark.Int:
		if i, ok := val.Int64(); ok {
			return i
		}
		return val.String()
	case starlark.Float:
		return float64(val)
	case starlark.String:
		return string(val)
	case *starlark.Dict:
		m, _ := starlarkToGoMap(v)
		return m
	case *starlark.List:
		result := make([]any, val.Len())
		for i := 0; i < val.Len(); i++ {
			result[i] = starlarkToGo(val.Index(i))
		}
		return result
	case starlark.Tuple:
		result := make([]any, val.Len())
		for i := 0; i < val.Len(); i++ {
			result[i] = starlarkToGo(val.Index(i))
		}
		return result
	default:
		return val.String()
	}
}
