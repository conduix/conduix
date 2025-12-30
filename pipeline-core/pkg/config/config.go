package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/conduix/conduix/shared/types"
)

// PipelineConfig 파이프라인 설정
type PipelineConfig struct {
	Version     string             `yaml:"version"`
	Name        string             `yaml:"name"`
	Type        types.PipelineType `yaml:"type,omitempty"` // flat 또는 actor
	Description string             `yaml:"description,omitempty"`

	// Actor 모드 설정
	ActorSystem *types.ActorSystemConfig `yaml:"actor_system,omitempty"`
	Pipeline    *types.ActorDefinition   `yaml:"pipeline,omitempty"`

	// Flat 모드 설정 (Vector 호환)
	Sources    map[string]SourceConfig    `yaml:"sources,omitempty"`
	Transforms map[string]TransformConfig `yaml:"transforms,omitempty"`
	Sinks      map[string]SinkConfig      `yaml:"sinks,omitempty"`

	// 공통 설정
	Checkpoint *types.CheckpointConfig `yaml:"checkpoint,omitempty"`
	Metrics    *MetricsConfig          `yaml:"metrics,omitempty"`
}

// SourceConfig 소스 설정
type SourceConfig struct {
	Type    string         `yaml:"type"`
	Options map[string]any `yaml:",inline"`
}

// TransformConfig 변환 설정
type TransformConfig struct {
	Type    string         `yaml:"type"`
	Inputs  []string       `yaml:"inputs"`
	Options map[string]any `yaml:",inline"`
}

// SinkConfig 싱크 설정
type SinkConfig struct {
	Type    string         `yaml:"type"`
	Inputs  []string       `yaml:"inputs"`
	Options map[string]any `yaml:",inline"`
}

// MetricsConfig 메트릭 설정
type MetricsConfig struct {
	Enabled bool          `yaml:"enabled"`
	Export  MetricsExport `yaml:"export,omitempty"`
}

// MetricsExport 메트릭 내보내기 설정
type MetricsExport struct {
	Prometheus *PrometheusConfig `yaml:"prometheus,omitempty"`
	Internal   *InternalConfig   `yaml:"internal,omitempty"`
}

// PrometheusConfig Prometheus 설정
type PrometheusConfig struct {
	Port int `yaml:"port"`
}

// InternalConfig 내부 메트릭 설정
type InternalConfig struct {
	Interval string `yaml:"interval"`
}

// Load 설정 파일 로드
func Load(path string) (*PipelineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	return Parse(data)
}

// Parse YAML 파싱
func Parse(data []byte) (*PipelineConfig, error) {
	var config PipelineConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// 기본값 설정
	if config.Type == "" {
		if config.Pipeline != nil {
			config.Type = types.PipelineTypeActor
		} else {
			config.Type = types.PipelineTypeFlat
		}
	}

	if config.Version == "" {
		config.Version = "1.0"
	}

	return &config, nil
}

// Validate 설정 검증
func (c *PipelineConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("pipeline name is required")
	}

	switch c.Type {
	case types.PipelineTypeFlat:
		return c.validateFlat()
	case types.PipelineTypeActor:
		return c.validateActor()
	default:
		return fmt.Errorf("unknown pipeline type: %s", c.Type)
	}
}

func (c *PipelineConfig) validateFlat() error {
	if len(c.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}
	if len(c.Sinks) == 0 {
		return fmt.Errorf("at least one sink is required")
	}

	// Transform 입력 검증
	availableOutputs := make(map[string]bool)
	for name := range c.Sources {
		availableOutputs[name] = true
	}

	for name, transform := range c.Transforms {
		for _, input := range transform.Inputs {
			if !availableOutputs[input] {
				return fmt.Errorf("transform %s has invalid input: %s", name, input)
			}
		}
		availableOutputs[name] = true
	}

	// Sink 입력 검증
	for name, sink := range c.Sinks {
		for _, input := range sink.Inputs {
			if !availableOutputs[input] {
				return fmt.Errorf("sink %s has invalid input: %s", name, input)
			}
		}
	}

	return nil
}

func (c *PipelineConfig) validateActor() error {
	if c.Pipeline == nil {
		return fmt.Errorf("pipeline definition is required for actor mode")
	}

	if c.Pipeline.Name == "" {
		return fmt.Errorf("pipeline root actor name is required")
	}

	return c.validateActorDefinition(c.Pipeline)
}

func (c *PipelineConfig) validateActorDefinition(def *types.ActorDefinition) error {
	if def.Name == "" {
		return fmt.Errorf("actor name is required")
	}

	switch def.Type {
	case types.ActorTypeSupervisor:
		// 자식 검증
		for _, child := range def.Children {
			if err := c.validateActorDefinition(&child); err != nil {
				return err
			}
		}
	case types.ActorTypeSource, types.ActorTypeTransform, types.ActorTypeSink, types.ActorTypeRouter:
		// 설정 필요시 검증
	default:
		if def.Type != "" {
			return fmt.Errorf("unknown actor type: %s", def.Type)
		}
	}

	return nil
}

// ToYAML YAML로 변환
func (c *PipelineConfig) ToYAML() ([]byte, error) {
	return yaml.Marshal(c)
}

// Save 설정 파일 저장
func (c *PipelineConfig) Save(path string) error {
	data, err := c.ToYAML()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
