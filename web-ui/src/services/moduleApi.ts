import { api } from './api'

// 허용 의존성 모듈(allowed_modules) — custom stage 가 import 가능한 외부 Go 모듈.
// version 은 이 모듈의 **기본 버전**이다. 레지스트리는 버전을 여러 개 보유할 수 있고,
// stage 는 저마다 자기 버전을 고정한다(ADR-0005).
export interface AllowedModule {
  module_path: string
  version: string
  description?: string
  added_by?: string
  status: string
  single_version_only?: boolean
  created_at?: string
  updated_at?: string
}

// 레지스트리가 보유한 버전 하나.
export interface AllowedModuleVersion {
  module_path: string
  version: string
  status: string
  added_by?: string
  created_at?: string
}

// 목록 응답 — 보유 버전과 버전별 사용 stage 수를 함께 준다.
export interface ModuleView extends AllowedModule {
  versions: AllowedModuleVersion[]
  usage: Record<string, number>
}

export async function listModules(): Promise<ModuleView[]> {
  const resp = await api.get<{ success: boolean; data: ModuleView[] }>('/modules')
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

// 기본 버전 변경(빈 버전이면 최신 재조회). **기존 stage 의 고정 버전은 바뀌지 않는다** —
// 새로 작성되는 stage 와 "기본 버전으로 올리기" 의 기준만 바뀐다.
export async function updateModule(
  modulePath: string,
  version?: string,
  singleVersionOnly?: boolean,
): Promise<AllowedModule> {
  const resp = await api.put<{ success: boolean; data: AllowedModule }>(
    `/modules/${modulePath}`,
    { version, single_version_only: singleVersionOnly },
  )
  return resp.data.data
}

// 보유 버전 추가(빈 값이면 @latest). 기본 버전은 그대로 둔다.
export async function addModuleVersion(
  modulePath: string,
  version?: string,
): Promise<AllowedModuleVersion> {
  const resp = await api.post<{ success: boolean; data: AllowedModuleVersion }>('/module-versions', {
    module_path: modulePath,
    version,
  })
  return resp.data.data
}

// 보유 버전 폐기. 기본 버전이거나 고정한 stage 가 있으면 서버가 409 로 거부한다.
export async function retireModuleVersion(modulePath: string, version: string): Promise<void> {
  const qs = new URLSearchParams({ module_path: modulePath, version })
  await api.delete(`/module-versions?${qs.toString()}`)
}

// 일괄 올리기 결과(stage 하나).
export interface UpgradeAllResult {
  plugin_name: string
  success: boolean
  changed?: Record<string, string>
  error?: string
}

// 그 모듈을 비기본 버전으로 고정한 stage 들을 기본 버전으로 수렴시킨다(admin).
// stage 마다 컴파일해 보므로 느릴 수 있고, 실패한 stage 는 그대로 남는다.
export async function upgradeAllStages(
  modulePath: string,
): Promise<{ module_path: string; default_version: string; results: UpgradeAllResult[] }> {
  const resp = await api.post<{
    success: boolean
    data: { module_path: string; default_version: string; results: UpgradeAllResult[] }
  }>('/module-versions/upgrade-all', { module_path: modulePath })
  return resp.data.data
}

export async function deleteModule(modulePath: string): Promise<void> {
  await api.delete(`/modules/${modulePath}`)
}
