package bento

import (
	"fmt"

	"github.com/warpstreamlabs/bento/public/service"
)

// ConfigBuilder builds Bento configurations from our config format
type ConfigBuilder struct {
	env *service.Environment
}

// NewConfigBuilder creates a new config builder
func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		env: service.NewEnvironment(),
	}
}

// BuildInput creates a Bento input from our config
// TODO: Bento API 변경으로 인해 StreamBuilder 방식으로 재구현 필요
func (b *ConfigBuilder) BuildInput(inputType string, config map[string]any) (*service.OwnedInput, error) {
	_, err := b.buildInputSpec(inputType, config)
	if err != nil {
		return nil, err
	}

	// 현재 버전의 Bento는 StreamBuilder.AsInput()을 지원하지 않습니다.
	// 전체 스트림을 구성하는 방식으로 변경되었습니다.
	return nil, fmt.Errorf("BuildInput: Bento API 변경으로 인해 미구현 상태")
}

// BuildOutput creates a Bento output from our config
// TODO: Bento API 변경으로 인해 StreamBuilder 방식으로 재구현 필요
func (b *ConfigBuilder) BuildOutput(outputType string, config map[string]any) (*service.OwnedOutput, error) {
	_, err := b.buildOutputSpec(outputType, config)
	if err != nil {
		return nil, err
	}

	// 현재 버전의 Bento는 StreamBuilder.AsOutput()을 지원하지 않습니다.
	return nil, fmt.Errorf("BuildOutput: Bento API 변경으로 인해 미구현 상태")
}

// BuildProcessor creates a Bento processor from our config
// TODO: Bento API 변경으로 인해 StreamBuilder 방식으로 재구현 필요
func (b *ConfigBuilder) BuildProcessor(processorType string, config map[string]any) (*service.OwnedProcessor, error) {
	_, err := b.buildProcessorSpec(processorType, config)
	if err != nil {
		return nil, err
	}

	// 현재 버전의 Bento는 StreamBuilder.AsProcessor()를 지원하지 않습니다.
	return nil, fmt.Errorf("BuildProcessor: Bento API 변경으로 인해 미구현 상태")
}

func (b *ConfigBuilder) buildInputSpec(inputType string, config map[string]any) (string, error) {
	switch inputType {
	case "kafka":
		return b.buildKafkaInputSpec(config)
	case "http_server":
		return b.buildHTTPServerInputSpec(config)
	case "file":
		return b.buildFileInputSpec(config)
	case "generate":
		return b.buildGenerateInputSpec(config)
	case "stdin":
		return "input:\n  stdin: {}", nil
	default:
		return "", fmt.Errorf("unsupported input type: %s", inputType)
	}
}

func (b *ConfigBuilder) buildKafkaInputSpec(config map[string]any) (string, error) {
	addresses := getStringSlice(config, "brokers", []string{"localhost:9092"})
	topics := getStringSlice(config, "topics", []string{"test"})
	consumerGroup := getString(config, "group_id", "bento-consumer")

	spec := fmt.Sprintf(`input:
  kafka:
    addresses: [%s]
    topics: [%s]
    consumer_group: "%s"
    start_from_oldest: true
`, joinStrings(addresses), joinStrings(topics), consumerGroup)

	return spec, nil
}

func (b *ConfigBuilder) buildHTTPServerInputSpec(config map[string]any) (string, error) {
	address := getString(config, "address", "0.0.0.0:8080")
	path := getString(config, "path", "/")

	spec := fmt.Sprintf(`input:
  http_server:
    address: "%s"
    path: "%s"
`, address, path)

	return spec, nil
}

func (b *ConfigBuilder) buildFileInputSpec(config map[string]any) (string, error) {
	paths := getStringSlice(config, "paths", []string{"/tmp/input.log"})

	spec := fmt.Sprintf(`input:
  file:
    paths: [%s]
    scanner:
      lines: {}
`, joinStrings(paths))

	return spec, nil
}

func (b *ConfigBuilder) buildGenerateInputSpec(config map[string]any) (string, error) {
	interval := getString(config, "interval", "1s")
	mapping := getString(config, "mapping", `root.message = "test"
root.timestamp = now()`)

	spec := fmt.Sprintf(`input:
  generate:
    interval: "%s"
    mapping: |
      %s
`, interval, mapping)

	return spec, nil
}

func (b *ConfigBuilder) buildOutputSpec(outputType string, config map[string]any) (string, error) {
	switch outputType {
	case "kafka":
		return b.buildKafkaOutputSpec(config)
	case "elasticsearch":
		return b.buildElasticsearchOutputSpec(config)
	case "http_client":
		return b.buildHTTPClientOutputSpec(config)
	case "file":
		return b.buildFileOutputSpec(config)
	case "aws_s3":
		return b.buildS3OutputSpec(config)
	case "stdout":
		return "output:\n  stdout: {}", nil
	case "drop":
		return "output:\n  drop: {}", nil
	default:
		return "", fmt.Errorf("unsupported output type: %s", outputType)
	}
}

func (b *ConfigBuilder) buildKafkaOutputSpec(config map[string]any) (string, error) {
	addresses := getStringSlice(config, "brokers", []string{"localhost:9092"})
	topic := getString(config, "topic", "output")

	spec := fmt.Sprintf(`output:
  kafka:
    addresses: [%s]
    topic: "%s"
`, joinStrings(addresses), topic)

	return spec, nil
}

func (b *ConfigBuilder) buildElasticsearchOutputSpec(config map[string]any) (string, error) {
	urls := getStringSlice(config, "endpoints", []string{"http://localhost:9200"})
	index := getString(config, "index", "logs")

	spec := fmt.Sprintf(`output:
  elasticsearch:
    urls: [%s]
    index: "%s"
    action: "index"
`, joinStrings(urls), index)

	return spec, nil
}

func (b *ConfigBuilder) buildHTTPClientOutputSpec(config map[string]any) (string, error) {
	url := getString(config, "url", "http://localhost:8080")
	verb := getString(config, "verb", "POST")

	spec := fmt.Sprintf(`output:
  http_client:
    url: "%s"
    verb: "%s"
`, url, verb)

	return spec, nil
}

func (b *ConfigBuilder) buildFileOutputSpec(config map[string]any) (string, error) {
	path := getString(config, "path", "/tmp/output.log")

	spec := fmt.Sprintf(`output:
  file:
    path: "%s"
    codec: lines
`, path)

	return spec, nil
}

func (b *ConfigBuilder) buildS3OutputSpec(config map[string]any) (string, error) {
	bucket := getString(config, "bucket", "my-bucket")
	path := getString(config, "prefix", "data/")

	spec := fmt.Sprintf(`output:
  aws_s3:
    bucket: "%s"
    path: "%s${!count("files")}.json"
`, bucket, path)

	return spec, nil
}

func (b *ConfigBuilder) buildProcessorSpec(processorType string, config map[string]any) (string, error) {
	switch processorType {
	case "bloblang":
		return b.buildBloblangProcessorSpec(config)
	case "jq":
		return b.buildJQProcessorSpec(config)
	case "json_parse":
		return "pipeline:\n  processors:\n    - mapping: 'root = this.parse_json()'", nil
	case "compress":
		return b.buildCompressProcessorSpec(config)
	case "decompress":
		return b.buildDecompressProcessorSpec(config)
	default:
		return "", fmt.Errorf("unsupported processor type: %s", processorType)
	}
}

func (b *ConfigBuilder) buildBloblangProcessorSpec(config map[string]any) (string, error) {
	mapping := getString(config, "mapping", "root = this")
	if source := getString(config, "source", ""); source != "" {
		mapping = source
	}

	spec := fmt.Sprintf(`pipeline:
  processors:
    - mapping: |
        %s
`, mapping)

	return spec, nil
}

func (b *ConfigBuilder) buildJQProcessorSpec(config map[string]any) (string, error) {
	query := getString(config, "query", ".")

	spec := fmt.Sprintf(`pipeline:
  processors:
    - jq:
        query: '%s'
`, query)

	return spec, nil
}

func (b *ConfigBuilder) buildCompressProcessorSpec(config map[string]any) (string, error) {
	algorithm := getString(config, "algorithm", "gzip")

	spec := fmt.Sprintf(`pipeline:
  processors:
    - compress:
        algorithm: "%s"
`, algorithm)

	return spec, nil
}

func (b *ConfigBuilder) buildDecompressProcessorSpec(config map[string]any) (string, error) {
	algorithm := getString(config, "algorithm", "gzip")

	spec := fmt.Sprintf(`pipeline:
  processors:
    - decompress:
        algorithm: "%s"
`, algorithm)

	return spec, nil
}

// Helper functions

func getString(config map[string]any, key string, defaultVal string) string {
	if v, ok := config[key].(string); ok {
		return v
	}
	return defaultVal
}

func getStringSlice(config map[string]any, key string, defaultVal []string) []string {
	if v, ok := config[key].([]string); ok {
		return v
	}
	if v, ok := config[key].([]any); ok {
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = fmt.Sprintf("%v", item)
		}
		return result
	}
	return defaultVal
}

func joinStrings(strs []string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf(`"%s"`, s)
	}
	return result
}
