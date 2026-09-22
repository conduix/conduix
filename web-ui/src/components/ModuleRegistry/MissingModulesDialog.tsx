/**
 * MissingModulesDialog — stage 저장이 "레지스트리 미등록 모듈" 로 거부됐을 때 뜨는 다이얼로그.
 *
 * 서버가 {import 경로: 제안 모듈 경로} 를 내려주므로 사용자는 탭을 옮겨 다시 타이핑하지 않고
 * 여기서 확인만 한다. 자동 등록은 하지 않는다 — 레지스트리는 공급망 허용 목록이라 승인은
 * 사람이 하고, 자동화되는 것은 타이핑과 재시도다. 권한이 없으면 목록 복사만 제공한다.
 */
import { useState } from 'react'
import {
  Alert,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  List,
  ListItem,
  ListItemText,
  Typography,
} from '@mui/material'
import { useTranslation } from 'react-i18next'

import { addModule } from '../../services/moduleApi'
import { uniqueSuggestedModules } from '../NativeStageEditor/depsImportUI'

interface Props {
  open: boolean
  /** import 경로 → 제안 모듈 경로 */
  details: Record<string, string>
  canAdd: boolean
  onClose: () => void
  /** 모듈이 전부 등록된 뒤 호출 — 저장을 재시도한다 */
  onRegistered: () => void | Promise<void>
}

export default function MissingModulesDialog({ open, details, canAdd, onClose, onRegistered }: Props) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [failures, setFailures] = useState<Record<string, string>>({})

  const modules = uniqueSuggestedModules(details)

  const handleAddAndRetry = async () => {
    setBusy(true)
    setFailures({})
    const failed: Record<string, string> = {}
    // 순차 등록: 병렬로 던지면 같은 GOPROXY 조회가 겹치고, 실패 원인을 모듈별로 보여주기도 어렵다.
    for (const m of modules) {
      try {
        await addModule(m)
      } catch (e) {
        const msg = e instanceof Error ? e.message : String(e)
        // 이미 등록됨(409)은 성공으로 본다 — 다른 사용자가 먼저 추가한 경우.
        if (!/409|already registered/i.test(msg)) failed[m] = msg
      }
    }
    setBusy(false)
    if (Object.keys(failed).length > 0) {
      setFailures(failed)
      return
    }
    await onRegistered()
  }

  const copyList = () => {
    void navigator.clipboard?.writeText(modules.join('\n'))
  }

  return (
    <Dialog open={open} onClose={busy ? undefined : onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{t('plugins.missing.title', '레지스트리에 없는 모듈이 있습니다')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
          {canAdd
            ? t('plugins.missing.helpCanAdd', '아래 모듈을 레지스트리에 추가(등록 시점 최신 버전 고정)하고 저장을 다시 시도합니다.')
            : t('plugins.missing.helpNoPerm', '모듈 추가 권한이 없습니다. 목록을 복사해 operator 또는 admin 에게 등록을 요청하세요.')}
        </Typography>
        <List dense sx={{ border: 1, borderColor: 'divider', borderRadius: 1 }}>
          {Object.entries(details).map(([imp, mod]) => (
            <ListItem key={imp}>
              <ListItemText
                primary={<Typography sx={{ fontFamily: 'monospace', fontSize: 13 }}>{imp}</Typography>}
                secondary={
                  imp !== mod
                    ? `${t('plugins.missing.module', '모듈')}: ${mod}`
                    : undefined
                }
              />
            </ListItem>
          ))}
        </List>
        {Object.keys(failures).length > 0 && (
          <Alert severity="error" sx={{ mt: 1, whiteSpace: 'pre-wrap' }}>
            {Object.entries(failures)
              .map(([m, msg]) => `${m}: ${msg}`)
              .join('\n')}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={copyList} disabled={busy}>
          {t('plugins.missing.copy', '목록 복사')}
        </Button>
        <Button onClick={onClose} disabled={busy}>
          {t('common.close', '닫기')}
        </Button>
        {canAdd && (
          <Button variant="contained" onClick={() => void handleAddAndRetry()} disabled={busy || modules.length === 0}>
            {busy ? <CircularProgress size={16} /> : t('plugins.missing.addAndSave', '추가하고 저장')}
          </Button>
        )}
      </DialogActions>
    </Dialog>
  )
}
