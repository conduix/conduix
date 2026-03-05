/**
 * Pipeline YAML Converter 테스트
 */

import { describe, it, expect } from 'vitest'
import {
  pipelineToYaml,
  yamlToPipeline,
  validatePipelineYaml,
  validateYamlString,
  formatYaml,
} from './pipelineYamlConverter'
import type { WorkflowPipeline } from '../types/pipeline'

describe('pipelineYamlConverter', () => {
  describe('pipelineToYaml', () => {
    it('빈 파이프라인을 YAML로 변환', () => {
      const pipeline: WorkflowPipeline = {
        id: 'test-1',
        name: 'Test Pipeline',
        priority: 0,
      }

      const yaml = pipelineToYaml(pipeline)
      expect(yaml).toContain('name: Test Pipeline')
    })

    it('Input이 있는 파이프라인을 YAML로 변환', () => {
      const pipeline: WorkflowPipeline = {
        id: 'test-2',
        name: 'Kafka Pipeline',
        priority: 0,
        input: {
          type: 'kafka',
          name: 'kafka_source',
          config: {
            brokers: 'localhost:9092',
            topic: 'my-topic',
          },
        },
      }

      const yaml = pipelineToYaml(pipeline)
      expect(yaml).toContain('input:')
      expect(yaml).toContain('type: kafka')
      expect(yaml).toContain('brokers: localhost:9092')
      expect(yaml).toContain('topic: my-topic')
    })

    it('Stages가 있는 파이프라인을 YAML로 변환', () => {
      const pipeline: WorkflowPipeline = {
        id: 'test-3',
        name: 'Filter Pipeline',
        priority: 0,
        stages: [
          {
            id: 'stage-1',
            name: 'filter_active',
            type: 'filter',
            config: { condition: '.active == true' },
          },
          {
            id: 'stage-2',
            name: 'remap_fields',
            type: 'remap',
            config: { mappings: { old_name: 'new_name' } },
          },
        ],
      }

      const yaml = pipelineToYaml(pipeline)
      expect(yaml).toContain('stages:')
      expect(yaml).toContain('name: filter_active')
      expect(yaml).toContain('type: filter')
      expect(yaml).toContain('name: remap_fields')
      expect(yaml).toContain('type: remap')
    })

    it('Outputs가 있는 파이프라인을 YAML로 변환', () => {
      const pipeline: WorkflowPipeline = {
        id: 'test-4',
        name: 'ES Pipeline',
        priority: 0,
        outputs: [
          {
            id: 'output-1',
            name: 'es_output',
            type: 'elasticsearch',
            config: {
              addresses: ['http://localhost:9200'],
              index: 'my-index',
            },
          },
        ],
      }

      const yaml = pipelineToYaml(pipeline)
      expect(yaml).toContain('outputs:')
      expect(yaml).toContain('type: elasticsearch')
      expect(yaml).toContain('index: my-index')
    })

    it('Batch 설정이 있는 파이프라인을 YAML로 변환', () => {
      const pipeline: WorkflowPipeline = {
        id: 'test-5',
        name: 'Batch Pipeline',
        priority: 0,
        batch: {
          enabled: true,
          output_mode: 'bulk',
          size: 500,
          workers: 10,
          flush_interval: '10s',
        },
      }

      const yaml = pipelineToYaml(pipeline)
      expect(yaml).toContain('batch:')
      expect(yaml).toContain('enabled: true')
      expect(yaml).toContain('output_mode: bulk')
      expect(yaml).toContain('size: 500')
    })

    it('Pre-stages가 있는 Output을 YAML로 변환', () => {
      const pipeline: WorkflowPipeline = {
        id: 'test-6',
        name: 'PreStages Pipeline',
        priority: 0,
        outputs: [
          {
            id: 'output-1',
            name: 'es_output',
            type: 'elasticsearch',
            config: { index: 'test' },
            pre_stages: [
              {
                id: 'ps-1',
                name: 'timestamp_remap',
                type: 'remap',
                config: { mappings: { '@timestamp': '.created_at' } },
              },
            ],
          },
        ],
      }

      const yaml = pipelineToYaml(pipeline)
      expect(yaml).toContain('pre_stages:')
      expect(yaml).toContain('name: timestamp_remap')
    })
  })

  describe('yamlToPipeline', () => {
    it('기본 YAML을 파이프라인으로 변환', () => {
      const yaml = `
name: Test Pipeline
description: A test pipeline
`
      const pipeline = yamlToPipeline(yaml)
      expect(pipeline.name).toBe('Test Pipeline')
      expect(pipeline.description).toBe('A test pipeline')
      expect(pipeline.id).toBeDefined()
    })

    it('Input이 있는 YAML을 파이프라인으로 변환', () => {
      const yaml = `
name: Kafka Pipeline
input:
  type: kafka
  name: kafka_source
  config:
    brokers: localhost:9092
    topic: events
`
      const pipeline = yamlToPipeline(yaml)
      expect(pipeline.input?.type).toBe('kafka')
      expect(pipeline.input?.name).toBe('kafka_source')
      expect(pipeline.input?.config?.brokers).toBe('localhost:9092')
    })

    it('Stages가 있는 YAML을 파이프라인으로 변환', () => {
      const yaml = `
name: Filter Pipeline
stages:
  - name: filter_1
    type: filter
    config:
      condition: '.active'
  - name: remap_1
    type: remap
    config:
      mappings:
        id: user_id
`
      const pipeline = yamlToPipeline(yaml)
      expect(pipeline.stages).toHaveLength(2)
      expect(pipeline.stages?.[0].type).toBe('filter')
      expect(pipeline.stages?.[1].type).toBe('remap')
    })

    it('기존 파이프라인 ID를 보존', () => {
      const yaml = `name: Updated Pipeline`
      const existing: WorkflowPipeline = {
        id: 'existing-id-123',
        name: 'Old Pipeline',
        priority: 5,
      }

      const pipeline = yamlToPipeline(yaml, existing)
      expect(pipeline.id).toBe('existing-id-123')
      expect(pipeline.name).toBe('Updated Pipeline')
      expect(pipeline.priority).toBe(5)
    })

    it('source 필드를 input으로 변환 (하위 호환성)', () => {
      const yaml = `
name: Legacy Pipeline
source:
  type: sql
  name: sql_source
  config:
    query: SELECT * FROM users
`
      const pipeline = yamlToPipeline(yaml)
      expect(pipeline.input?.type).toBe('sql')
      expect(pipeline.source?.type).toBe('sql')
    })

    it('유효하지 않은 YAML에 대해 에러 발생', () => {
      const yaml = `not valid: yaml: content: [[`
      expect(() => yamlToPipeline(yaml)).toThrow()
    })
  })

  describe('validatePipelineYaml', () => {
    it('유효한 파이프라인 객체 검증 성공', () => {
      const parsed = {
        name: 'Valid Pipeline',
        input: { type: 'kafka', name: 'source' },
        stages: [{ name: 'filter', type: 'filter' }],
        outputs: [{ name: 'output', type: 'elasticsearch' }],
      }

      const result = validatePipelineYaml(parsed)
      expect(result.valid).toBe(true)
      expect(result.errors).toHaveLength(0)
    })

    it('Input 타입 누락 시 에러', () => {
      const parsed = {
        name: 'Invalid Pipeline',
        input: { name: 'source' }, // type 누락
      }

      const result = validatePipelineYaml(parsed)
      expect(result.valid).toBe(false)
      expect(result.errors).toContain('Input type is required')
    })

    it('Stage 타입 누락 시 에러', () => {
      const parsed = {
        name: 'Invalid Pipeline',
        stages: [{ name: 'stage_1' }], // type 누락
      }

      const result = validatePipelineYaml(parsed)
      expect(result.valid).toBe(false)
      expect(result.errors.some(e => e.includes('type is required'))).toBe(true)
    })

    it('Output 타입 누락 시 에러', () => {
      const parsed = {
        name: 'Invalid Pipeline',
        outputs: [{ name: 'output_1' }], // type 누락
      }

      const result = validatePipelineYaml(parsed)
      expect(result.valid).toBe(false)
      expect(result.errors.some(e => e.includes('type is required'))).toBe(true)
    })

    it('알 수 없는 Stage 타입에 대한 경고', () => {
      const parsed = {
        name: 'Pipeline',
        stages: [{ name: 'stage_1', type: 'unknown_type' }],
      }

      const result = validatePipelineYaml(parsed)
      expect(result.valid).toBe(true) // 경고는 valid를 false로 만들지 않음
      expect(result.warnings.some(w => w.includes('unknown type'))).toBe(true)
    })

    it('잘못된 Batch output_mode 검증', () => {
      const parsed = {
        name: 'Pipeline',
        batch: { output_mode: 'invalid_mode' },
      }

      const result = validatePipelineYaml(parsed)
      expect(result.valid).toBe(false)
      expect(result.errors.some(e => e.includes('output_mode'))).toBe(true)
    })
  })

  describe('validateYamlString', () => {
    it('유효한 YAML 문자열 검증 성공', () => {
      const yaml = `
name: Valid Pipeline
input:
  type: kafka
  name: source
`
      const result = validateYamlString(yaml)
      expect(result.valid).toBe(true)
    })

    it('잘못된 YAML 구문에 대해 에러 반환', () => {
      const yaml = `
name: Invalid
stages:
  - type: filter
    config: {invalid json: [
`
      const result = validateYamlString(yaml)
      expect(result.valid).toBe(false)
      expect(result.errors.length).toBeGreaterThan(0)
    })
  })

  describe('formatYaml', () => {
    it('YAML 포맷팅', () => {
      const messyYaml = `name: Test
stages:
  - type: filter
    name: stage1`

      const formatted = formatYaml(messyYaml)
      expect(formatted).toContain('name: Test')
      expect(formatted).toContain('type: filter')
    })

    it('잘못된 YAML은 원본 반환', () => {
      const invalidYaml = `{{invalid: yaml`
      const result = formatYaml(invalidYaml)
      expect(result).toBe(invalidYaml)
    })
  })

  describe('양방향 변환 일관성', () => {
    it('Pipeline → YAML → Pipeline 변환 일관성', () => {
      const original: WorkflowPipeline = {
        id: 'test-round-trip',
        name: 'Round Trip Test',
        priority: 0,
        description: 'Testing round trip conversion',
        input: {
          type: 'rest_api',
          name: 'api_source',
          config: {
            url: 'https://api.example.com/data',
            method: 'GET',
          },
        },
        stages: [
          {
            id: 'stage-1',
            name: 'filter_active',
            type: 'filter',
            config: { condition: '.active == true' },
          },
        ],
        outputs: [
          {
            id: 'output-1',
            name: 'kafka_output',
            type: 'kafka',
            config: {
              brokers: ['localhost:9092'],
              topic: 'processed-events',
            },
          },
        ],
      }

      const yaml = pipelineToYaml(original)
      const restored = yamlToPipeline(yaml, original)

      expect(restored.id).toBe(original.id)
      expect(restored.name).toBe(original.name)
      expect(restored.description).toBe(original.description)
      expect(restored.input?.type).toBe(original.input?.type)
      expect(restored.input?.config?.url).toBe(original.input?.config?.url)
      expect(restored.stages?.length).toBe(original.stages?.length)
      expect(restored.outputs?.length).toBe(original.outputs?.length)
    })
  })
})
