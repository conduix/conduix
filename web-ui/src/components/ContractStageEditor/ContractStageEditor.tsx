/**
 * ContractStageEditor - Data Contract Stage 설정 에디터
 * 복합 비즈니스 규칙, 스키마, DLQ, Circuit Breaker 설정을 GUI로 관리
 */
import { useState, useCallback } from 'react'
import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Button,
  Tabs,
  Tab,
  Chip,
  Stack,
  Switch,
  FormControlLabel,
  Accordion,
  AccordionSummary,
  AccordionDetails,
  Alert,
  Tooltip,
  Paper,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import WarningIcon from '@mui/icons-material/Warning'
import ErrorIcon from '@mui/icons-material/Error'
import InfoIcon from '@mui/icons-material/Info'
import GavelIcon from '@mui/icons-material/Gavel'
import SecurityIcon from '@mui/icons-material/Security'
import StorageIcon from '@mui/icons-material/Storage'
import { useTranslation } from 'react-i18next'
import { RuleBuilder } from './RuleBuilder'
import { SchemaFieldEditor } from './SchemaFieldEditor'
import { CircuitBreakerConfig } from './CircuitBreakerConfig'
import { DLQConfigEditor } from './DLQConfigEditor'
import type {
  DataContract,
  BusinessRule,
  ContractSchema,
  ContractField,
  ViolationAction,
  CircuitBreakerConfig as CBConfig,
  DLQConfig,
  RuleSeverity,
} from '../../types/contract'

interface ContractStageEditorProps {
  value: {
    contract?: DataContract
    action?: ViolationAction
    tag_field?: string
    circuit_breaker?: CBConfig
    dlq?: DLQConfig
  }
  onChange: (value: ContractStageEditorProps['value']) => void
  availableFields?: string[] // 소스/이전 스테이지에서 사용 가능한 필드 목록
}

const defaultContract: DataContract = {
  name: '',
  version: '1.0.0',
  rules: [],
}

const defaultRule: Omit<BusinessRule, 'id'> = {
  name: '',
  description: '',
  condition: '',
  severity: 'error',
}

export function ContractStageEditor({ value, onChange, availableFields = [] }: ContractStageEditorProps) {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState(0)

  const contract = value.contract || defaultContract
  const action = value.action || 'drop'

  // Contract 기본 정보 업데이트
  const updateContract = useCallback((updates: Partial<DataContract>) => {
    onChange({
      ...value,
      contract: { ...contract, ...updates },
    })
  }, [value, contract, onChange])

  // 규칙 추가
  const addRule = useCallback(() => {
    const newRule: BusinessRule = {
      id: crypto.randomUUID(),
      ...defaultRule,
      name: `rule_${(contract.rules?.length || 0) + 1}`,
    }
    updateContract({ rules: [...(contract.rules || []), newRule] })
  }, [contract.rules, updateContract])

  // 규칙 업데이트
  const updateRule = useCallback((ruleId: string, updates: Partial<BusinessRule>) => {
    const newRules = (contract.rules || []).map(rule =>
      rule.id === ruleId ? { ...rule, ...updates } : rule
    )
    updateContract({ rules: newRules })
  }, [contract.rules, updateContract])

  // 규칙 삭제
  const deleteRule = useCallback((ruleId: string) => {
    const newRules = (contract.rules || []).filter(rule => rule.id !== ruleId)
    updateContract({ rules: newRules })
  }, [contract.rules, updateContract])

  // 스키마 업데이트
  const updateSchema = useCallback((schema: ContractSchema | undefined) => {
    updateContract({ schema })
  }, [updateContract])

  // 필드 추가
  const addSchemaField = useCallback(() => {
    const currentFields = contract.schema?.fields || []
    const newField: ContractField = {
      name: `field_${currentFields.length + 1}`,
      type: 'string',
      required: false,
    }
    updateSchema({
      ...contract.schema,
      fields: [...currentFields, newField],
    })
  }, [contract.schema, updateSchema])

  // 필드 업데이트
  const updateSchemaField = useCallback((index: number, field: ContractField) => {
    const newFields = [...(contract.schema?.fields || [])]
    newFields[index] = field
    updateSchema({ ...contract.schema, fields: newFields })
  }, [contract.schema, updateSchema])

  // 필드 삭제
  const deleteSchemaField = useCallback((index: number) => {
    const newFields = (contract.schema?.fields || []).filter((_, i) => i !== index)
    updateSchema({ ...contract.schema, fields: newFields })
  }, [contract.schema, updateSchema])

  // Severity별 아이콘
  const severityIcon = (severity: RuleSeverity) => {
    switch (severity) {
      case 'error': return <ErrorIcon fontSize="small" color="error" />
      case 'warning': return <WarningIcon fontSize="small" color="warning" />
      case 'info': return <InfoIcon fontSize="small" color="info" />
    }
  }

  return (
    <Box>
      {/* 기본 정보 */}
      <Card variant="outlined" sx={{ mb: 2 }}>
        <CardContent>
          <Typography variant="subtitle2" gutterBottom sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <GavelIcon fontSize="small" />
            {t('contract.basicInfo', 'Contract 기본 정보')}
          </Typography>
          <Stack direction="row" spacing={2} sx={{ mb: 2 }}>
            <TextField
              label={t('contract.name', '계약 이름')}
              value={contract.name}
              onChange={(e) => updateContract({ name: e.target.value })}
              size="small"
              sx={{ flex: 2 }}
              placeholder="order_data_contract"
            />
            <TextField
              label={t('contract.version', '버전')}
              value={contract.version}
              onChange={(e) => updateContract({ version: e.target.value })}
              size="small"
              sx={{ flex: 1 }}
              placeholder="1.0.0"
            />
          </Stack>
          <Stack direction="row" spacing={2} sx={{ mb: 2 }}>
            <TextField
              label={t('contract.owner', '소유자')}
              value={contract.owner || ''}
              onChange={(e) => updateContract({ owner: e.target.value })}
              size="small"
              sx={{ flex: 1 }}
              placeholder="data-team"
            />
            <TextField
              label={t('contract.team', '팀')}
              value={contract.team || ''}
              onChange={(e) => updateContract({ team: e.target.value })}
              size="small"
              sx={{ flex: 1 }}
              placeholder="platform"
            />
          </Stack>
          <TextField
            label={t('contract.description', '설명')}
            value={contract.description || ''}
            onChange={(e) => updateContract({ description: e.target.value })}
            size="small"
            fullWidth
            multiline
            rows={2}
            placeholder={t('contract.descriptionPlaceholder', '이 계약의 목적과 적용 범위를 설명하세요')}
          />
        </CardContent>
      </Card>

      {/* 위반 처리 방식 */}
      <Card variant="outlined" sx={{ mb: 2 }}>
        <CardContent>
          <Typography variant="subtitle2" gutterBottom>
            {t('contract.violationAction', '위반 시 처리 방식')}
          </Typography>
          <Stack direction="row" spacing={2} alignItems="center">
            <FormControl size="small" sx={{ minWidth: 200 }}>
              <InputLabel>{t('contract.action', '처리 방식')}</InputLabel>
              <Select
                value={action}
                label={t('contract.action', '처리 방식')}
                onChange={(e) => onChange({ ...value, action: e.target.value as ViolationAction })}
              >
                <MenuItem value="drop">
                  <Stack direction="row" spacing={1} alignItems="center">
                    <span>{t('contract.actionDrop', '삭제 (Drop)')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="quarantine">
                  <Stack direction="row" spacing={1} alignItems="center">
                    <span>{t('contract.actionQuarantine', 'DLQ로 격리 (Quarantine)')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="tag">
                  <Stack direction="row" spacing={1} alignItems="center">
                    <span>{t('contract.actionTag', '태그 추가 후 통과 (Tag)')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="error">
                  <Stack direction="row" spacing={1} alignItems="center">
                    <span>{t('contract.actionError', '파이프라인 중단 (Error)')}</span>
                  </Stack>
                </MenuItem>
              </Select>
            </FormControl>

            {action === 'tag' && (
              <TextField
                label={t('contract.tagField', '태그 필드명')}
                value={value.tag_field || '_contract_violations'}
                onChange={(e) => onChange({ ...value, tag_field: e.target.value })}
                size="small"
                sx={{ flex: 1 }}
              />
            )}
          </Stack>

          {action === 'quarantine' && (
            <Alert severity="info" sx={{ mt: 2 }}>
              {t('contract.quarantineInfo', 'DLQ(Dead Letter Queue) 설정이 필요합니다. 아래 DLQ 탭에서 설정하세요.')}
            </Alert>
          )}
        </CardContent>
      </Card>

      {/* 탭 네비게이션 */}
      <Box sx={{ borderBottom: 1, borderColor: 'divider', mb: 2 }}>
        <Tabs value={activeTab} onChange={(_, v) => setActiveTab(v)}>
          <Tab
            label={
              <Stack direction="row" spacing={1} alignItems="center">
                <GavelIcon fontSize="small" />
                <span>{t('contract.rulesTab', '비즈니스 규칙')}</span>
                <Chip label={contract.rules?.length || 0} size="small" />
              </Stack>
            }
          />
          <Tab
            label={
              <Stack direction="row" spacing={1} alignItems="center">
                <StorageIcon fontSize="small" />
                <span>{t('contract.schemaTab', '스키마 검증')}</span>
                <Chip label={contract.schema?.fields?.length || 0} size="small" />
              </Stack>
            }
          />
          <Tab
            label={
              <Stack direction="row" spacing={1} alignItems="center">
                <SecurityIcon fontSize="small" />
                <span>{t('contract.circuitBreakerTab', 'Circuit Breaker')}</span>
              </Stack>
            }
          />
          {action === 'quarantine' && (
            <Tab
              label={
                <Stack direction="row" spacing={1} alignItems="center">
                  <StorageIcon fontSize="small" />
                  <span>{t('contract.dlqTab', 'DLQ 설정')}</span>
                </Stack>
              }
            />
          )}
        </Tabs>
      </Box>

      {/* 비즈니스 규칙 탭 */}
      {activeTab === 0 && (
        <Box>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 2 }}>
            <Typography variant="body2" color="text.secondary">
              {t('contract.rulesDescription', '필드 간 관계, 비즈니스 로직 등 복합 조건을 정의합니다.')}
            </Typography>
            <Button startIcon={<AddIcon />} size="small" onClick={addRule}>
              {t('contract.addRule', '규칙 추가')}
            </Button>
          </Box>

          {(contract.rules || []).length === 0 ? (
            <Paper variant="outlined" sx={{ p: 4, textAlign: 'center' }}>
              <Typography color="text.secondary">
                {t('contract.noRules', '정의된 규칙이 없습니다. 규칙을 추가하세요.')}
              </Typography>
            </Paper>
          ) : (
            <Stack spacing={2}>
              {(contract.rules || []).map((rule, index) => (
                <Accordion key={rule.id} defaultExpanded={index === 0}>
                  <AccordionSummary expandIcon={<ExpandMoreIcon />}>
                    <Stack direction="row" spacing={2} alignItems="center" sx={{ flex: 1, pr: 2 }}>
                      {severityIcon(rule.severity)}
                      <Typography sx={{ fontWeight: 500 }}>{rule.name || `규칙 ${index + 1}`}</Typography>
                      <Chip
                        label={rule.severity}
                        size="small"
                        color={rule.severity === 'error' ? 'error' : rule.severity === 'warning' ? 'warning' : 'info'}
                      />
                      {rule.description && (
                        <Typography variant="body2" color="text.secondary" sx={{ flex: 1 }}>
                          {rule.description}
                        </Typography>
                      )}
                    </Stack>
                  </AccordionSummary>
                  <AccordionDetails>
                    <RuleBuilder
                      rule={rule}
                      onChange={(updates) => updateRule(rule.id, updates)}
                      onDelete={() => deleteRule(rule.id)}
                      availableFields={availableFields}
                    />
                  </AccordionDetails>
                </Accordion>
              ))}
            </Stack>
          )}
        </Box>
      )}

      {/* 스키마 검증 탭 */}
      {activeTab === 1 && (
        <Box>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
            <Stack direction="row" spacing={2} alignItems="center">
              <Typography variant="body2" color="text.secondary">
                {t('contract.schemaDescription', '필드별 타입, 필수 여부, 제약 조건을 정의합니다.')}
              </Typography>
              <FormControlLabel
                control={
                  <Switch
                    checked={contract.schema?.strict || false}
                    onChange={(e) => updateSchema({ ...contract.schema, fields: contract.schema?.fields || [], strict: e.target.checked })}
                    size="small"
                  />
                }
                label={
                  <Tooltip title={t('contract.strictModeTooltip', '활성화하면 스키마에 정의되지 않은 필드가 있을 경우 위반으로 처리합니다')}>
                    <Typography variant="body2">{t('contract.strictMode', 'Strict 모드')}</Typography>
                  </Tooltip>
                }
              />
            </Stack>
            <Button startIcon={<AddIcon />} size="small" onClick={addSchemaField}>
              {t('contract.addField', '필드 추가')}
            </Button>
          </Box>

          {(contract.schema?.fields || []).length === 0 ? (
            <Paper variant="outlined" sx={{ p: 4, textAlign: 'center' }}>
              <Typography color="text.secondary">
                {t('contract.noFields', '정의된 필드가 없습니다. 필드를 추가하세요.')}
              </Typography>
            </Paper>
          ) : (
            <Stack spacing={1}>
              {(contract.schema?.fields || []).map((field, index) => (
                <SchemaFieldEditor
                  key={index}
                  field={field}
                  onChange={(f) => updateSchemaField(index, f)}
                  onDelete={() => deleteSchemaField(index)}
                />
              ))}
            </Stack>
          )}
        </Box>
      )}

      {/* Circuit Breaker 탭 */}
      {activeTab === 2 && (
        <CircuitBreakerConfig
          value={value.circuit_breaker}
          onChange={(cb) => onChange({ ...value, circuit_breaker: cb })}
        />
      )}

      {/* DLQ 설정 탭 */}
      {activeTab === 3 && action === 'quarantine' && (
        <DLQConfigEditor
          value={value.dlq}
          onChange={(dlq) => onChange({ ...value, dlq })}
        />
      )}
    </Box>
  )
}

export default ContractStageEditor
