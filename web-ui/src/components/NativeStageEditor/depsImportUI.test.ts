import { describe, expect, it } from 'vitest'

import {
  canManageModules,
  classifyImports,
  friendlyGoplsMessage,
  missingModulesFromError,
  owningModule,
  parseGoImports,
  uniqueSuggestedModules,
  UNREGISTERED_IMPORT_PREFIX,
} from './depsImportUI'

const source = `package geocode_kakao

import (
    "database/sql"
    "encoding/json" // 주석 뒤
    resty "github.com/go-resty/resty/v2"
    "github.com/aws/aws-sdk-go-v2/service/s3"
    // "github.com/commented/out"
    sdk "github.com/conduix/conduix/plugin-sdk"
)

import "github.com/google/uuid"
import u2 "github.com/google/uuid"
`

describe('parseGoImports', () => {
  it('블록·단일·alias import 를 모두 뽑고 주석 줄과 중복은 제외한다', () => {
    expect(parseGoImports(source)).toEqual([
      'database/sql',
      'encoding/json',
      'github.com/go-resty/resty/v2',
      'github.com/aws/aws-sdk-go-v2/service/s3',
      'github.com/conduix/conduix/plugin-sdk',
      'github.com/google/uuid',
    ])
  })

  it('import 가 없으면 빈 배열', () => {
    expect(parseGoImports('package x\n\nfunc F() {}\n')).toEqual([])
  })
})

describe('classifyImports', () => {
  it('표준 라이브러리와 conduix 내부 모듈은 제외하고, 서브패키지는 가장 긴 등록 모듈에 매핑한다', () => {
    const registry = ['github.com/aws/aws-sdk-go-v2', 'github.com/aws/aws-sdk-go-v2/service/s3', 'github.com/google/uuid']
    const usage = classifyImports(parseGoImports(source), registry)
    expect(usage).toEqual([
      { importPath: 'github.com/go-resty/resty/v2', modulePath: undefined },
      { importPath: 'github.com/aws/aws-sdk-go-v2/service/s3', modulePath: 'github.com/aws/aws-sdk-go-v2/service/s3' },
      { importPath: 'github.com/google/uuid', modulePath: 'github.com/google/uuid' },
    ])
  })

  it('owningModule 은 접두사 경계를 지킨다(github.com/a/bc 가 github.com/a/b 에 매핑되면 안 됨)', () => {
    expect(owningModule('github.com/a/bc', ['github.com/a/b'])).toBeUndefined()
    expect(owningModule('github.com/a/b/c', ['github.com/a/b'])).toBe('github.com/a/b')
  })
})

describe('missingModulesFromError', () => {
  const axiosLike = (code: string, details?: Record<string, string>) => ({
    response: { data: { success: false, error: { code, message: 'x', details } } },
  })

  it('BUSINESS_MISSING_MODULES 면 details 를 돌려준다', () => {
    expect(
      missingModulesFromError(axiosLike('BUSINESS_MISSING_MODULES', { 'github.com/x/y/sub': 'github.com/x/y' })),
    ).toEqual({ 'github.com/x/y/sub': 'github.com/x/y' })
  })

  it('다른 코드·형식이면 null (일반 에러 처리로 떨어진다)', () => {
    expect(missingModulesFromError(axiosLike('REQUEST_VALIDATION_FAILED'))).toBeNull()
    expect(missingModulesFromError(new Error('network'))).toBeNull()
    expect(missingModulesFromError(undefined)).toBeNull()
  })

  it('uniqueSuggestedModules 는 같은 모듈의 서브패키지 여러 개를 등록 1회로 합친다', () => {
    expect(
      uniqueSuggestedModules({ 'github.com/x/y/a': 'github.com/x/y', 'github.com/x/y/b': 'github.com/x/y', 'g.com/z': 'g.com/z' }),
    ).toEqual(['github.com/x/y', 'g.com/z'])
  })
})

describe('friendlyGoplsMessage', () => {
  it('gopls 의 모듈 없음 진단을 행동 가능한 문장으로 바꾸고 import 경로를 뽑는다', () => {
    const r = friendlyGoplsMessage(
      'could not import github.com/go-resty/resty/v2 (no required module provides package "github.com/go-resty/resty/v2")',
    )
    expect(r?.importPath).toBe('github.com/go-resty/resty/v2')
    expect(r?.message.startsWith(UNREGISTERED_IMPORT_PREFIX)).toBe(true)
  })

  it('무관한 진단은 건드리지 않는다', () => {
    expect(friendlyGoplsMessage('undefined: foo')).toBeNull()
  })
})

describe('canManageModules', () => {
  it('서버 라우트와 같은 기준(operator, admin)', () => {
    expect(canManageModules('admin')).toBe(true)
    expect(canManageModules('operator')).toBe(true)
    expect(canManageModules('viewer')).toBe(false)
    expect(canManageModules(undefined)).toBe(false)
  })
})
