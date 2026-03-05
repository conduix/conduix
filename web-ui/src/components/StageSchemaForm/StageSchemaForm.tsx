/**
 * StageSchemaForm - Stage Schema 기반 폼 자동 생성 컴포넌트
 *
 * Stage Schema를 받아서 자동으로 폼을 생성합니다.
 * CustomEditor가 지정된 경우 해당 커스텀 컴포넌트를 렌더링합니다.
 */
import { useMemo, lazy, Suspense } from 'react'
import { Stack, Typography, Divider, CircularProgress, Box, Alert } from '@mui/material'
import { useTranslation } from 'react-i18next'
import type { StageSchema } from '../../types/stage-schema'
import { SchemaField } from './SchemaField'

// 커스텀 에디터 동적 임포트
const customEditors: Record<string, React.LazyExoticComponent<React.ComponentType<CustomEditorProps>>> = {
  ContractStageEditor: lazy(() => import('../ContractStageEditor/ContractStageEditor')),
  // 추가 커스텀 에디터들
  // RouteRuleEditor: lazy(() => import('../RouteRuleEditor/RouteRuleEditor')),
}

interface CustomEditorProps {
  value: Record<string, unknown>
  onChange: (value: Record<string, unknown>) => void
  disabled?: boolean
}

interface StageSchemaFormProps {
  schema: StageSchema
  value: Record<string, unknown>
  onChange: (value: Record<string, unknown>) => void
  errors?: Record<string, string>
  disabled?: boolean
}

export function StageSchemaForm({
  schema,
  value,
  onChange,
  errors = {},
  disabled,
}: StageSchemaFormProps) {
  const { t } = useTranslation()

  // 값 변경 핸들러
  const handleFieldChange = (fieldName: string, fieldValue: unknown) => {
    onChange({
      ...value,
      [fieldName]: fieldValue,
    })
  }

  // 필드 그룹화 (object 타입 필드 기준)
  // useMemo는 조건부 return 전에 호출되어야 함
  const groupedFields = useMemo(() => {
    if (!schema.fields || schema.fields.length === 0) {
      return []
    }

    const groups: Array<{ title?: string; fields: typeof schema.fields }> = []
    let currentGroup: typeof schema.fields = []

    for (const field of schema.fields) {
      if (field.type === 'object' && field.fields) {
        // 이전 그룹 저장
        if (currentGroup.length > 0) {
          groups.push({ fields: currentGroup })
          currentGroup = []
        }
        // object 필드는 별도 섹션
        groups.push({ title: field.display_name, fields: field.fields })
      } else {
        currentGroup.push(field)
      }
    }

    // 마지막 그룹 저장
    if (currentGroup.length > 0) {
      groups.push({ fields: currentGroup })
    }

    return groups
  }, [schema.fields, schema])

  // 커스텀 에디터가 지정된 경우
  if (schema.custom_editor) {
    const CustomEditor = customEditors[schema.custom_editor]

    if (!CustomEditor) {
      return (
        <Alert severity="warning">
          {t('stage.customEditorNotFound', { editor: schema.custom_editor })}
        </Alert>
      )
    }

    return (
      <Suspense
        fallback={
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
            <CircularProgress />
          </Box>
        }
      >
        <CustomEditor
          value={value}
          onChange={onChange}
          disabled={disabled}
        />
      </Suspense>
    )
  }

  // 필드가 없는 경우
  if (!schema.fields || schema.fields.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary">
        {t('stage.noConfigRequired', '이 Stage는 추가 설정이 필요하지 않습니다.')}
      </Typography>
    )
  }

  return (
    <Stack spacing={3}>
      {groupedFields.map((group, groupIndex) => (
        <Box key={groupIndex}>
          {group.title && (
            <>
              <Typography variant="subtitle2" gutterBottom>
                {group.title}
              </Typography>
              <Divider sx={{ mb: 2 }} />
            </>
          )}
          <Stack spacing={2}>
            {group.fields.map((field) => (
              <SchemaField
                key={field.name}
                field={field}
                value={value[field.name]}
                onChange={(v) => handleFieldChange(field.name, v)}
                allValues={value}
                error={errors[field.name]}
                disabled={disabled}
              />
            ))}
          </Stack>
        </Box>
      ))}
    </Stack>
  )
}

export default StageSchemaForm
