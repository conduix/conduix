import { useState, useCallback } from 'react'

type SnackbarSeverity = 'success' | 'error' | 'warning' | 'info'

interface SnackbarState {
  open: boolean
  message: string
  severity: SnackbarSeverity
}

// Simple hook for now - will integrate with MUI Snackbar context later
export function useSnackbar() {
  const [state, setState] = useState<SnackbarState>({
    open: false,
    message: '',
    severity: 'info',
  })

  const showMessage = useCallback((message: string, severity: SnackbarSeverity) => {
    setState({ open: true, message, severity })
  }, [])

  const showSuccess = useCallback((message: string) => {
    showMessage(message, 'success')
  }, [showMessage])

  const showError = useCallback((message: string) => {
    showMessage(message, 'error')
  }, [showMessage])

  const showWarning = useCallback((message: string) => {
    showMessage(message, 'warning')
  }, [showMessage])

  const showInfo = useCallback((message: string) => {
    showMessage(message, 'info')
  }, [showMessage])

  const closeSnackbar = useCallback(() => {
    setState((prev) => ({ ...prev, open: false }))
  }, [])

  return {
    ...state,
    showSuccess,
    showError,
    showWarning,
    showInfo,
    closeSnackbar,
  }
}
