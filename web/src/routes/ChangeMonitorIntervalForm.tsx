import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'

import { ApiError, changeMonitorInterval, type MonitorResponse } from '../api/monitors'
import { useTranslation } from '../i18n/context'
import { monitorQueryKey, monitorsQueryKey } from './monitorQueries'

export default function ChangeMonitorIntervalForm({ monitor }: { monitor: MonitorResponse }) {
  const { t, translateError, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const [intervalSeconds, setIntervalSeconds] = useState(String(monitor.intervalSeconds))
  const listKey = monitorsQueryKey(monitor.organizationId, monitor.projectId)
  const detailKey = monitorQueryKey(monitor.organizationId, monitor.projectId, monitor.id)
  const mutation = useMutation<MonitorResponse, unknown, number>({
    mutationFn: (nextInterval) => changeMonitorInterval(
      monitor.organizationId,
      monitor.projectId,
      monitor.id,
      nextInterval,
    ),
    onSuccess: async (value) => {
      setIntervalSeconds(String(value.intervalSeconds))
      queryClient.setQueryData(detailKey, value)
      await queryClient.invalidateQueries({ queryKey: listKey, exact: true })
    },
  })

  if (monitor.state === 'archived') {
    return null
  }

  const parsedInterval = Number(intervalSeconds)
  const canSubmit = intervalSeconds.trim() !== '' &&
    Number.isInteger(parsedInterval) &&
    parsedInterval !== monitor.intervalSeconds

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmit) {
      return
    }
    mutation.mutate(parsedInterval)
  }

  const fieldErrors = mutation.error instanceof ApiError && mutation.error.status === 400
    ? (mutation.error.problem.errors?.intervalSeconds ?? []).map(translateError)
    : []
  const generalError = mutation.error instanceof ApiError
    ? translateProblem(mutation.error.problem)
    : t('monitor.intervalEdit.failed')

  return (
    <section className="monitor-interval-section" aria-labelledby="monitor-interval-heading">
      <h2 id="monitor-interval-heading">{t('monitor.intervalEdit.heading')}</h2>
      <form className="monitor-form" onSubmit={submit} aria-label={t('monitor.intervalEdit.form')}>
        <div className="form-field">
          <label>
            {t('monitor.intervalEdit.seconds')}
            <input
              name="intervalSeconds"
              type="number"
              min="30"
              max="86400"
              step="1"
              value={intervalSeconds}
              onChange={(event) => {
                setIntervalSeconds(event.target.value)
                mutation.reset()
              }}
              inputMode="numeric"
            />
          </label>
        </div>
        <div className="form-actions">
          <button type="submit" disabled={mutation.isPending || !canSubmit}>
            {mutation.isPending
              ? t('monitor.intervalEdit.submitting')
              : t('monitor.intervalEdit.submit')}
          </button>
        </div>
        {fieldErrors.length > 0 && (
          <ul className="field-errors form-field-wide" role="alert">
            {fieldErrors.map((message) => <li key={message}>{message}</li>)}
          </ul>
        )}
      </form>
      {mutation.isSuccess && (
        <p className="success" role="status">{t('monitor.intervalEdit.done')}</p>
      )}
      {mutation.isError && fieldErrors.length === 0 && (
        <p className="error" role="alert">{generalError}</p>
      )}
    </section>
  )
}
