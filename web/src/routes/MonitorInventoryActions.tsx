import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router'

import { organizationOverviewQueryKey } from '../api/overview'
import { ApiError, changeMonitorState, type MonitorResponse } from '../api/monitors'
import { triggerManualRun } from '../api/runs'
import { useTranslation } from '../i18n/context'
import { monitorQueryKey, monitorsQueryKey } from './monitorQueries'

interface MonitorInventoryActionsProps {
  monitor: MonitorResponse
  onResume: (monitor: MonitorResponse) => void
}

export default function MonitorInventoryActions({ monitor, onResume }: MonitorInventoryActionsProps) {
  const { t, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [confirmingArchive, setConfirmingArchive] = useState(false)
  const listKey = monitorsQueryKey(monitor.organizationId, monitor.projectId)
  const detailKey = monitorQueryKey(monitor.organizationId, monitor.projectId, monitor.id)
  const stateMutation = useMutation({
    mutationFn: (state: 'active' | 'paused' | 'archived') => changeMonitorState(
      monitor.organizationId, monitor.projectId, monitor.id, state,
    ),
    onSuccess: async (value) => {
      setConfirmingArchive(false)
      queryClient.setQueryData(detailKey, value)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: listKey }),
        queryClient.invalidateQueries({ queryKey: organizationOverviewQueryKey(monitor.organizationId), exact: true }),
      ])
    },
  })
  const runMutation = useMutation({
    mutationFn: () => triggerManualRun(monitor.organizationId, monitor.projectId, monitor.id),
    onSuccess: async (run) => {
      await queryClient.invalidateQueries({ queryKey: listKey })
      void navigate(`/organizations/${monitor.organizationId}/projects/${monitor.projectId}/monitors/${monitor.id}/runs/${run.id}`)
    },
  })

  const error = stateMutation.error instanceof ApiError
    ? translateProblem(stateMutation.error.problem)
    : runMutation.error instanceof ApiError
      ? translateProblem(runMutation.error.problem)
      : stateMutation.error || runMutation.error
        ? t('monitor.inventory.actionFailed')
        : null
  const actionPending = stateMutation.isPending || runMutation.isPending
  const stateAction = monitor.state === 'active' ? 'paused' : 'active'
  const stateLabel = monitor.state === 'active'
    ? (stateMutation.isPending ? t('monitor.lifecycle.pausing') : t('monitor.lifecycle.pause'))
    : stateMutation.isPending
      ? t('monitor.lifecycle.activating')
      : t('monitor.lifecycle.activate')

  if (monitor.state === 'archived') {
    return <div className="monitor-inventory-actions"><span className="muted">{t('monitor.lifecycle.archivedReadonly')}</span></div>
  }

  return (
    <div className="monitor-inventory-actions">
      {monitor.state === 'draft' && monitor.latestRevisionNumber === 0 ? (
        <button type="button" className="button-secondary button-compact" onClick={() => onResume(monitor)} disabled={actionPending}>
          {t('monitor.setup.resume')}
        </button>
      ) : (
        <button type="button" className="button-secondary button-compact" onClick={() => stateMutation.mutate(stateAction)} disabled={actionPending}>
          {stateLabel}
        </button>
      )}
      {monitor.latestRevisionNumber > 0 && (
        <button type="button" className="button-secondary button-compact" onClick={() => runMutation.mutate()} disabled={actionPending}>
          {runMutation.isPending ? t('run.manual.running') : t('run.manual.action')}
        </button>
      )}
      {!confirmingArchive ? (
        <button type="button" className="button-danger button-compact" onClick={() => { stateMutation.reset(); setConfirmingArchive(true) }} disabled={actionPending}>
          {t('monitor.lifecycle.archive')}
        </button>
      ) : (
        <span className="monitor-inventory-confirmation" role="group" aria-label={t('monitor.lifecycle.archiveConfirmation')}>
          <span>{t('monitor.lifecycle.archiveWarning')}</span>
          <button type="button" className="button-secondary button-compact" onClick={() => setConfirmingArchive(false)} disabled={actionPending}>
            {t('monitor.lifecycle.cancel')}
          </button>
          <button type="button" className="button-danger button-compact" onClick={() => stateMutation.mutate('archived')} disabled={actionPending}>
            {stateMutation.isPending ? t('monitor.lifecycle.archiving') : t('monitor.lifecycle.confirmArchive')}
          </button>
        </span>
      )}
      {error && <span className="error" role="alert">{typeof error === 'string' ? error : t('monitor.inventory.actionFailed')}</span>}
    </div>
  )
}
