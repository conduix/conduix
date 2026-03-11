package stream

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// JSScriptStage executes JavaScript code for record transformation using goja.
// goja supports ES5.1 fully + ES6 partially (let/const, arrow functions, template literals, etc.)
// Built-in JS objects available: JSON, RegExp, Date, Math, String, Array, Object, etc.
// Go-registered helper: console.log only. Use Go builtin stages for hash, base64, etc.
type JSScriptStage struct {
	BaseStage
	code    string
	timeout time.Duration

	// pool of pre-compiled goja programs for reuse
	program *goja.Program
	pool    sync.Pool
}

// NewJSScriptStage creates a JSScriptStage from config.
// config keys:
//   - code (string, required): JavaScript source with function process(record)
//   - timeout (string, optional): per-record timeout, default "1s"
func NewJSScriptStage(name string, config map[string]any) (*JSScriptStage, error) {
	code, ok := config["code"].(string)
	if !ok || code == "" {
		return nil, fmt.Errorf("js_script stage %q: 'code' is required", name)
	}

	timeout := 1 * time.Second
	if t, ok := config["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(t); err == nil {
			timeout = parsed
		}
	}

	// compile once to validate syntax
	program, err := goja.Compile(name+".js", code, false)
	if err != nil {
		return nil, fmt.Errorf("js_script stage %q: compile error: %w", name, err)
	}

	s := &JSScriptStage{
		BaseStage: BaseStage{name: name, typ: "js_script", config: config},
		code:      code,
		timeout:   timeout,
		program:   program,
	}

	// validate: run once to check process function exists
	vm := s.createVM()
	if _, err := vm.RunProgram(s.program); err != nil {
		return nil, fmt.Errorf("js_script stage %q: execution error: %w", name, err)
	}
	processFn := vm.Get("process")
	if processFn == nil || goja.IsUndefined(processFn) {
		return nil, fmt.Errorf("js_script stage %q: must define a 'process(record)' function", name)
	}
	if _, ok := goja.AssertFunction(processFn); !ok {
		return nil, fmt.Errorf("js_script stage %q: 'process' must be a function", name)
	}

	s.pool = sync.Pool{
		New: func() any {
			vm := s.createVM()
			if _, err := vm.RunProgram(s.program); err != nil {
				return nil
			}
			return vm
		},
	}

	return s, nil
}

// createVM creates a new goja runtime with Go-registered helper functions.
func (s *JSScriptStage) createVM() *goja.Runtime {
	vm := goja.New()

	// console.log — only Go-registered helper
	console := vm.NewObject()
	_ = console.Set("log", func(call goja.FunctionCall) goja.Value {
		args := make([]any, len(call.Arguments))
		for i, arg := range call.Arguments {
			args[i] = arg.Export()
		}
		log.Printf("[js_script:%s] %v", s.name, args)
		return goja.Undefined()
	})
	_ = vm.Set("console", console)

	return vm
}

// Process executes the JavaScript process(record) function.
// Returns nil to drop the record (when script returns null/undefined).
func (s *JSScriptStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	vmAny := s.pool.Get()
	if vmAny == nil {
		s.incrementError()
		return record, fmt.Errorf("js_script stage %q: failed to create JS runtime", s.name)
	}
	vm := vmAny.(*goja.Runtime)
	defer s.pool.Put(vm)

	processFn, ok := goja.AssertFunction(vm.Get("process"))
	if !ok {
		s.incrementError()
		return record, fmt.Errorf("js_script stage %q: process is not a function", s.name)
	}

	// convert record.Data to JS object
	inputObj := vm.ToValue(record.Data)

	// execute with timeout
	done := make(chan struct{})
	var result goja.Value
	var callErr error

	go func() {
		defer func() {
			if r := recover(); r != nil {
				if interrupted, ok := r.(*goja.InterruptedError); ok {
					callErr = fmt.Errorf("interrupted: %s", interrupted.String())
				} else {
					callErr = fmt.Errorf("panic: %v", r)
				}
			}
			close(done)
		}()
		result, callErr = processFn(goja.Undefined(), inputObj)
	}()

	select {
	case <-done:
		// completed
	case <-time.After(s.timeout):
		vm.Interrupt("timeout")
		<-done
		s.incrementError()
		return record, fmt.Errorf("js_script stage %q: execution timeout (%v)", s.name, s.timeout)
	case <-ctx.Done():
		vm.Interrupt("context canceled")
		<-done
		return nil, ctx.Err()
	}

	if callErr != nil {
		s.incrementError()
		log.Printf("[js_script:%s] error: %v", s.name, callErr)
		return record, nil // passthrough on error
	}

	// null/undefined → drop record
	if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
		return nil, nil
	}

	// convert result back to Go map
	exported := result.Export()
	resultMap, ok := exported.(map[string]any)
	if !ok {
		s.incrementError()
		log.Printf("[js_script:%s] result is not an object: %T", s.name, exported)
		return record, nil
	}

	record.Data = resultMap
	s.incrementOutput()
	return record, nil
}
