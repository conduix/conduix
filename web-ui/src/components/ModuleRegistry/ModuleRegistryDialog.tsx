import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Stack,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import DeleteIcon from '@mui/icons-material/Delete'
import UpgradeIcon from '@mui/icons-material/Upgrade'

import {
  addModuleVersion,
  listModules,
  retireModuleVersion,
  updateModule,
  upgradeAllStages,
  type ModuleView,
  type UpgradeAllResult,
} from '../../services/moduleApi'

interface ModuleRegistryDialogProps {
  open: boolean
  onClose: () => void
}

/**
 * 모듈 레지스트리 관리 — 보유 버전 추가/폐기, 기본 버전 지정, single_version_only 토글,
 * 그리고 그 모듈을 비기본 버전으로 고정한 stage 들의 일괄 수렴(ADR-0005).
 *
 * 기본 버전을 바꿔도 기존 stage 는 자기 고정 버전으로 계속 동작한다 — 그 사실을 화면에서
 * 읽을 수 있어야 관리자가 안심하고 기본을 올린다.
 */
export default function ModuleRegistryDialog({ open, onClose }: ModuleRegistryDialogProps) {
  const { t } = useTranslation()
  const [modules, setModules] = useState<ModuleView[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busyKey, setBusyKey] = useState<string | null>(null)
  const [newVersion, setNewVersion] = useState<Record<string, string>>({})
  const [upgradeResults, setUpgradeResults] = useState<{ module: string; results: UpgradeAllResult[] } | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setModules(await listModules())
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open) void load()
  }, [open, load])

  // 서버가 사유(409 등)를 문장으로 주므로 그대로 보여준다.
  const run = useCallback(async (key: string, fn: () => Promise<unknown>) => {
    setBusyKey(key)
    setError(null)
    try {
      await fn()
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusyKey(null)
    }
  }, [load])

  const handleUpgradeAll = useCallback(async (modulePath: string) => {
    setBusyKey(`upgrade:${modulePath}`)
    setError(null)
    setUpgradeResults(null)
    try {
      const res = await upgradeAllStages(modulePath)
      setUpgradeResults({ module: modulePath, results: res.results })
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusyKey(null)
    }
  }, [load])

  return (
    <Dialog open={open} onClose={onClose} maxWidth="lg" fullWidth>
      <DialogTitle>{t('modules.registryTitle', '모듈 레지스트리')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('modules.registryHelp', '기본 버전은 새로 저장되는 stage 에 적용됩니다. 이미 저장된 stage 는 자기 고정 버전을 그대로 씁니다 — 올리려면 "일괄 올리기" 를 쓰세요(각 stage 를 컴파일해 보고 성공한 것만 반영합니다).')}
        </Typography>

        {error && <Alert severity="error" sx={{ mb: 2, whiteSpace: 'pre-wrap' }} onClose={() => setError(null)}>{error}</Alert>}

        {upgradeResults && (
          <Alert severity="info" sx={{ mb: 2 }} onClose={() => setUpgradeResults(null)}>
            <Typography variant="body2" sx={{ fontWeight: 600 }}>
              {upgradeResults.module}
            </Typography>
            {upgradeResults.results.length === 0 && (
              <Typography variant="body2">{t('modules.upgradeNothing', '올릴 stage 가 없습니다.')}</Typography>
            )}
            {upgradeResults.results.map((r) => (
              <Typography key={r.plugin_name} variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>
                {r.success ? '✓' : '✗'} {r.plugin_name}{r.error ? `: ${r.error}` : ''}
              </Typography>
            ))}
          </Alert>
        )}

        {loading && <CircularProgress size={20} />}

        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>{t('modules.path', '모듈')}</TableCell>
              <TableCell>{t('modules.versions', '보유 버전 (사용 stage 수)')}</TableCell>
              <TableCell align="center">{t('modules.singleOnly', '단일 버전만')}</TableCell>
              <TableCell align="right">{t('modules.actions', '작업')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {modules.map((m) => (
              <TableRow key={m.module_path}>
                <TableCell sx={{ fontFamily: 'monospace', fontSize: 13 }}>{m.module_path}</TableCell>
                <TableCell>
                  <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', gap: 0.5 }}>
                    {(m.versions || []).map((v) => {
                      const isDefault = v.version === m.version
                      const used = m.usage?.[v.version] || 0
                      return (
                        <Chip
                          key={v.version}
                          size="small"
                          variant={isDefault ? 'filled' : 'outlined'}
                          color={isDefault ? 'primary' : 'default'}
                          sx={{ fontFamily: 'monospace', fontSize: '0.7rem' }}
                          label={`${v.version}${used ? ` (${used})` : ''}`}
                          onClick={isDefault ? undefined : () => void run(`default:${m.module_path}:${v.version}`,
                            () => updateModule(m.module_path, v.version))}
                          onDelete={isDefault ? undefined : () => void run(`retire:${m.module_path}:${v.version}`,
                            () => retireModuleVersion(m.module_path, v.version))}
                          deleteIcon={<DeleteIcon />}
                        />
                      )
                    })}
                  </Stack>
                  <Stack direction="row" spacing={0.5} sx={{ mt: 1 }}>
                    <TextField
                      size="small"
                      placeholder={t('modules.versionPlaceholder', '비우면 최신')}
                      value={newVersion[m.module_path] || ''}
                      onChange={(e) => setNewVersion((p) => ({ ...p, [m.module_path]: e.target.value }))}
                      sx={{ width: 160, '& input': { fontFamily: 'monospace', fontSize: 12 } }}
                      disabled={m.single_version_only}
                    />
                    <Button
                      size="small"
                      startIcon={busyKey === `add:${m.module_path}` ? <CircularProgress size={12} /> : <AddIcon />}
                      disabled={m.single_version_only || busyKey !== null}
                      onClick={() => void run(`add:${m.module_path}`, async () => {
                        await addModuleVersion(m.module_path, newVersion[m.module_path]?.trim() || undefined)
                        setNewVersion((p) => ({ ...p, [m.module_path]: '' }))
                      })}
                    >
                      {t('modules.addVersion', '버전 추가')}
                    </Button>
                  </Stack>
                </TableCell>
                <TableCell align="center">
                  <Tooltip title={t('modules.singleOnlyHelp', 'init() 으로 전역 등록하는 모듈(DB 드라이버 등)은 두 버전이 함께 링크되면 충돌합니다. 켜면 기본 외 버전을 보유할 수 없습니다.')}>
                    <Switch
                      size="small"
                      checked={!!m.single_version_only}
                      disabled={busyKey !== null}
                      onChange={(e) => void run(`single:${m.module_path}`,
                        () => updateModule(m.module_path, undefined, e.target.checked))}
                    />
                  </Tooltip>
                </TableCell>
                <TableCell align="right">
                  <Tooltip title={t('modules.upgradeAllHelp', '이 모듈을 기본과 다른 버전으로 고정한 stage 들을 기본 버전으로 올립니다. 컴파일에 실패한 stage 는 그대로 둡니다.')}>
                    <span>
                      <IconButton
                        size="small"
                        disabled={busyKey !== null}
                        onClick={() => void handleUpgradeAll(m.module_path)}
                      >
                        {busyKey === `upgrade:${m.module_path}` ? <CircularProgress size={16} /> : <UpgradeIcon fontSize="small" />}
                      </IconButton>
                    </span>
                  </Tooltip>
                </TableCell>
              </TableRow>
            ))}
            {!loading && modules.length === 0 && (
              <TableRow>
                <TableCell colSpan={4}>
                  <Box sx={{ py: 2 }}>
                    <Typography variant="body2" color="text.secondary">
                      {t('modules.empty', '등록된 모듈이 없습니다. stage 편집기의 의존성 탭에서 추가하세요.')}
                    </Typography>
                  </Box>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.close', '닫기')}</Button>
      </DialogActions>
    </Dialog>
  )
}
