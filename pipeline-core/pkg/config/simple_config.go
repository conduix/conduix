package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SimplePipelineConfig 단순화된 파이프라인 설정
// 읽기 → 처리 → 처리 → 처리 ...
type SimplePipelineConfig struct {
	Version string       `yaml:"version,omitempty"`
	Name    string       `yaml:"name"`
	Input   InputConfig  `yaml:"input"`
	Steps   []StepConfig `yaml:"steps"`

	// 선택적 설정
	Checkpoint *SimpleCheckpointConfig `yaml:"checkpoint,omitempty"`
	Metrics    *SimpleMetricsConfig    `yaml:"metrics,omitempty"`
}

// InputConfig 입력 소스 설정
type InputConfig struct {
	Type string `yaml:"type"` // kafka, http_server, file, generate, stdin

	// Kafka
	Brokers []string `yaml:"brokers,omitempty"`
	Topics  []string `yaml:"topics,omitempty"`
	GroupID string   `yaml:"group_id,omitempty"`

	// HTTP Server
	Address string `yaml:"address,omitempty"`
	Path    string `yaml:"path,omitempty"`

	// File
	Paths []string `yaml:"paths,omitempty"`

	// Generate (테스트용)
	Interval string `yaml:"interval,omitempty"`
	Mapping  string `yaml:"mapping,omitempty"`
}

// StepConfig 처리 단계 설정
type StepConfig struct {
	Name string `yaml:"name"`

	// 처리 옵션 (하나만 선택)
	Transform string  `yaml:"transform,omitempty"` // Bloblang 변환
	Filter    string  `yaml:"filter,omitempty"`    // 조건식 (통과하는 것만)
	Sample    float64 `yaml:"sample,omitempty"`    // 샘플링 비율 (0.0-1.0)

	// 저장 옵션 (선택적)
	Save *SaveConfig `yaml:"save,omitempty"`
}

// SaveConfig 저장 설정
type SaveConfig struct {
	Type string `yaml:"type"` // elasticsearch, kafka, s3, http, file, stdout

	// Elasticsearch
	URL   string `yaml:"url,omitempty"`
	Index string `yaml:"index,omitempty"`

	// Kafka
	Brokers []string `yaml:"brokers,omitempty"`
	Topic   string   `yaml:"topic,omitempty"`

	// S3
	Bucket string `yaml:"bucket,omitempty"`
	Prefix string `yaml:"prefix,omitempty"`

	// HTTP
	Method string `yaml:"method,omitempty"` // GET, POST, PUT

	// File
	Path string `yaml:"path,omitempty"`

	// 공통
	Buffer *BufferConfig `yaml:"buffer,omitempty"`
}

// BufferConfig 버퍼 설정
type BufferConfig struct {
	MaxEvents int    `yaml:"max_events,omitempty"`
	Timeout   string `yaml:"timeout,omitempty"`
}

// SimpleCheckpointConfig 체크포인트 설정 (Simple 전용)
type SimpleCheckpointConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Storage  string `yaml:"storage,omitempty"` // redis, file
	Interval string `yaml:"interval,omitempty"`
}

// SimpleMetricsConfig 메트릭 설정 (Simple 전용)
type SimpleMetricsConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port,omitempty"`
}

// LoadSimpleConfig 단순 설정 파일 로드
func LoadSimpleConfig(path string) (*SimplePipelineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	return ParseSimpleConfig(data)
}

// ParseSimpleConfig YAML 파싱
func ParseSimpleConfig(data []byte) (*SimplePipelineConfig, error) {
	var config SimplePipelineConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate 설정 검증
func (c *SimplePipelineConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("pipeline name is required")
	}

	if c.Input.Type == "" {
		return fmt.Errorf("input type is required")
	}

	// 입력 타입별 검증
	switch c.Input.Type {
	case "kafka":
		if len(c.Input.Brokers) == 0 {
			return fmt.Errorf("kafka brokers are required")
		}
		if len(c.Input.Topics) == 0 {
			return fmt.Errorf("kafka topics are required")
		}
	case "http_server":
		if c.Input.Address == "" {
			c.Input.Address = "0.0.0.0:8080"
		}
		if c.Input.Path == "" {
			c.Input.Path = "/"
		}
	case "file":
		if len(c.Input.Paths) == 0 {
			return fmt.Errorf("file paths are required")
		}
	case "generate", "stdin", "demo":
		// OK
	default:
		return fmt.Errorf("unsupported input type: %s", c.Input.Type)
	}

	// 단계 검증
	for i, step := range c.Steps {
		if step.Name == "" {
			return fmt.Errorf("step %d: name is required", i)
		}

		// 저장 설정 검증
		if step.Save != nil {
			if err := validateSaveConfig(step.Save); err != nil {
				return fmt.Errorf("step %s: %w", step.Name, err)
			}
		}
	}

	return nil
}

func validateSaveConfig(save *SaveConfig) error {
	switch save.Type {
	case "elasticsearch":
		if save.URL == "" {
			return fmt.Errorf("elasticsearch url is required")
		}
	case "kafka":
		if len(save.Brokers) == 0 {
			return fmt.Errorf("kafka brokers are required")
		}
		if save.Topic == "" {
			return fmt.Errorf("kafka topic is required")
		}
	case "s3":
		if save.Bucket == "" {
			return fmt.Errorf("s3 bucket is required")
		}
	case "http":
		if save.URL == "" {
			return fmt.Errorf("http url is required")
		}
	case "file":
		if save.Path == "" {
			return fmt.Errorf("file path is required")
		}
	case "stdout":
		// OK
	default:
		return fmt.Errorf("unsupported save type: %s", save.Type)
	}

	return nil
}

// ToLegacyConfig 기존 설정 형식으로 변환 (호환성)
func (c *SimplePipelineConfig) ToLegacyConfig() *PipelineConfig {
	legacy := &PipelineConfig{
		Version:    c.Version,
		Name:       c.Name,
		Type:       "flat",
		Sources:    make(map[string]SourceConfig),
		Transforms: make(map[string]TransformConfig),
		Sinks:      make(map[string]SinkConfig),
	}

	// Metrics 변환
	if c.Metrics != nil {
		legacy.Metrics = &MetricsConfig{
			Enabled: c.Metrics.Enabled,
		}
		if c.Metrics.Port > 0 {
			legacy.Metrics.Export.Prometheus = &PrometheusConfig{
				Port: c.Metrics.Port,
			}
		}
	}

	// Input → Source (Options 맵 사용)
	inputOpts := make(map[string]any)
	if len(c.Input.Brokers) > 0 {
		inputOpts["brokers"] = c.Input.Brokers
	}
	if len(c.Input.Topics) > 0 {
		inputOpts["topics"] = c.Input.Topics
	}
	if c.Input.GroupID != "" {
		inputOpts["group_id"] = c.Input.GroupID
	}
	if c.Input.Address != "" {
		inputOpts["address"] = c.Input.Address
	}
	if c.Input.Path != "" {
		inputOpts["path"] = c.Input.Path
	}
	if len(c.Input.Paths) > 0 {
		inputOpts["paths"] = c.Input.Paths
	}

	legacy.Sources["input"] = SourceConfig{
		Type:    c.Input.Type,
		Options: inputOpts,
	}

	// Steps → Transforms + Sinks
	prevStep := "input"
	for _, step := range c.Steps {
		// Transform 또는 Filter
		if step.Transform != "" || step.Filter != "" || step.Sample > 0 {
			transformType := "remap"
			transformOpts := make(map[string]any)

			if step.Filter != "" {
				transformType = "filter"
				transformOpts["condition"] = step.Filter
			} else if step.Sample > 0 {
				transformType = "sample"
				transformOpts["rate"] = step.Sample
			} else {
				transformOpts["source"] = step.Transform
			}

			legacy.Transforms[step.Name] = TransformConfig{
				Type:    transformType,
				Inputs:  []string{prevStep},
				Options: transformOpts,
			}
			prevStep = step.Name
		}

		// Save → Sink
		if step.Save != nil {
			sinkName := step.Name + "_sink"
			sinkOpts := make(map[string]any)

			if step.Save.URL != "" {
				sinkOpts["endpoints"] = []string{step.Save.URL}
			}
			if step.Save.Index != "" {
				sinkOpts["index"] = step.Save.Index
			}
			if len(step.Save.Brokers) > 0 {
				sinkOpts["brokers"] = step.Save.Brokers
			}
			if step.Save.Topic != "" {
				sinkOpts["topic"] = step.Save.Topic
			}
			if step.Save.Bucket != "" {
				sinkOpts["bucket"] = step.Save.Bucket
			}
			if step.Save.Prefix != "" {
				sinkOpts["prefix"] = step.Save.Prefix
			}
			if step.Save.Path != "" {
				sinkOpts["path"] = step.Save.Path
			}

			legacy.Sinks[sinkName] = SinkConfig{
				Type:    step.Save.Type,
				Inputs:  []string{prevStep},
				Options: sinkOpts,
			}
		}
	}

	return legacy
}
