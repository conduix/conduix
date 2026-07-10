import { api } from './api'

// 허용 의존성 모듈(allowed_modules) — custom stage 가 import 가능한 외부 Go 모듈.
// 버전은 module 당 하나로 고정(플랫폼 관리). 사용자는 추가만 하고 버전은 등록 시점 최신으로 자동 고정.
export interface AllowedModule {
  module_path: string
  version: string
  description?: string
  added_by?: string
  status: string
  created_at?: string
  updated_at?: string
}

export async function listModules(): Promise<AllowedModule[]> {
  const resp = await api.get<{ success: boolean; data: AllowedModule[] }>('/modules')
  return resp.data?.data || []
}

// 모듈 추가 — 버전은 서버가 GOPROXY @latest 로 자동 고정한다(요청에 버전 없음).
export async function addModule(modulePath: string, description?: string): Promise<AllowedModule> {
  const resp = await api.post<{ success: boolean; data: AllowedModule }>('/modules', {
    module_path: modulePath,
    description,
  })
  return resp.data.data
}

// 버전 갱신(빈 버전이면 최신 재조회). 전역 일괄 반영.
export async function updateModule(modulePath: string, version?: string): Promise<AllowedModule> {
  const resp = await api.put<{ success: boolean; data: AllowedModule }>(
    `/modules/${modulePath}`,
    { version },
  )
  return resp.data.data
}

export async function deleteModule(modulePath: string): Promise<void> {
  await api.delete(`/modules/${modulePath}`)
}
