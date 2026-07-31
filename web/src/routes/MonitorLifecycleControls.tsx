import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import {
  ApiError,
  changeMonitorState,
  type MonitorResponse,
  type MonitorStateTarget,
} from '../api/monitors'
import { useTranslation } from '../i18n/context'
import { monitorQueryKey, monitorsQueryKey } from './monitorQueries'

export default function MonitorLifecycleControls({ monitor }: { monitor: MonitorResponse }) {
  const { t, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const [confirmingArchive, setConfirmingArchive] = useState(false)
  const listKey = monitorsQueryKey(monitor.organizationId, monitor.projectId)
  const detailKey = monitorQueryKey(monitor.organizationId, monitor.projectId, monitor.id)
  const mutation = useMutation<MonitorResponse, unknown, MonitorStateTarget>({
    mutationFn: (state) => changeMonitorState(
      monitor.organizationId,
      monitor.projectId,
      monitor.id,
      state,
    ),
    onSuccess: async (value) => {
      setConfirmingArchive(false)
      queryClient.setQueryData(detailKey, value)
      await queryClient.invalidateQueries({ queryKey: listKey, exact: true })
    },
  })

  function changeState(state: MonitorStateTarget) {
    setConfirmingArchive(false)
    mutation.mutate(state)
  }

  const activationBlocked = monitor.state === 'draft' && monitor.latestRevisionNumber === 0
  const generalError = mutation.error instanceof ApiError
    ? translateProblem(mutation.error.problem)
    : t('monitor.lifecycle.failed')

  return (
    <section className="monitor-lifecycle-section" aria-labelledby="monitor-lifecycle-heading">
      <h2 id="monitor-lifecycle-heading">{t('monitor.lifecycle.heading')}</h2>
      {monitor.state === 'archived' ? (
        <p className="muted">{t('monitor.lifecycle.archivedReadonly')}</p>
      ) : (
        <>
          <div className="lifecycle-actions">
            {(monitor.state === 'draft' || monitor.state === 'paused') && (
              <button
                type="button"
                onClick={() => changeState('active')}
                disabled={mutation.isPending || activationBlocked}
              >
                {mutation.isPending && mutation.variables === 'active'
                  ? t('monitor.lifecycle.activating')
                  : t('monitor.lifecycle.activate')}
              </button>
            )}
            {monitor.state === 'active' && (
              <button
                type="button"
                onClick={() => changeState('paused')}
                disabled={mutation.isPending}
              >
                {mutation.isPending && mutation.variables === 'paused'
                  ? t('monitor.lifecycle.pausing')
                  : t('monitor.lifecycle.pause')}
              </button>
            )}
            <button
              type="button"
              className="button-secondary"
              onClick={() => {
                mutation.reset()
                setConfirmingArchive(true)
              }}
              disabled={mutation.isPending}
            >
              {t('monitor.lifecycle.archive')}
            </button>
          </div>

          {activationBlocked && (
            <p className="muted">{t('monitor.lifecycle.activationBlocked')}</p>
          )}

          {confirmingArchive && (
            <div
              className="lifecycle-confirmation"
              role="group"
              aria-label={t('monitor.lifecycle.archiveConfirmation')}
            >
              <p>{t('monitor.lifecycle.archiveWarning')}</p>
              <div className="form-actions">
                <button
                  type="button"
                  className="button-secondary"
                  onClick={() => setConfirmingArchive(false)}
                  disabled={mutation.isPending}
                >
                  {t('monitor.lifecycle.cancel')}
                </button>
                <button
                  type="button"
                  className="button-danger"
                  onClick={() => changeState('archived')}
                  disabled={mutation.isPending}
                >
                  {mutation.isPending
                    ? t('monitor.lifecycle.archiving')
                    : t('monitor.lifecycle.confirmArchive')}
                </button>
              </div>
            </div>
          )}
        </>
      )}

      {mutation.isSuccess && (
        <p className="success" role="status">
          {mutation.variables === 'active' && t('monitor.lifecycle.activated')}
          {mutation.variables === 'paused' && t('monitor.lifecycle.paused')}
          {mutation.variables === 'archived' && t('monitor.lifecycle.archived')}
        </p>
      )}
      {mutation.isError && (
        <p className="error" role="alert">{generalError}</p>
      )}
    </section>
  )
}
