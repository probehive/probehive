import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'

import { ApiError, renameMonitor, type MonitorResponse } from '../api/monitors'
import { useTranslation } from '../i18n/context'
import { monitorQueryKey, monitorsQueryKey } from './monitorQueries'

export default function RenameMonitorForm({ monitor }: { monitor: MonitorResponse }) {
  const { t, translateError, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const [name, setName] = useState(monitor.name)
  const listKey = monitorsQueryKey(monitor.organizationId, monitor.projectId)
  const detailKey = monitorQueryKey(monitor.organizationId, monitor.projectId, monitor.id)
  const mutation = useMutation<MonitorResponse, unknown, string>({
    mutationFn: (nextName) => renameMonitor(
      monitor.organizationId,
      monitor.projectId,
      monitor.id,
      nextName,
    ),
    onSuccess: async (value) => {
      setName(value.name)
      queryClient.setQueryData(detailKey, value)
      await queryClient.invalidateQueries({ queryKey: listKey, exact: true })
    },
  })

  if (monitor.state === 'archived') {
    return null
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate(name)
  }

  const fieldErrors = mutation.error instanceof ApiError && mutation.error.status === 400
    ? (mutation.error.problem.errors?.name ?? []).map(translateError)
    : []
  const generalError = mutation.error instanceof ApiError
    ? translateProblem(mutation.error.problem)
    : t('monitor.rename.failed')

  return (
    <section className="monitor-rename-section" aria-labelledby="monitor-rename-heading">
      <h2 id="monitor-rename-heading">{t('monitor.rename.heading')}</h2>
      <form className="monitor-form" onSubmit={submit} aria-label={t('monitor.rename.form')}>
        <div className="form-field">
          <label>
            {t('monitor.rename.name')}
            <input
              name="name"
              value={name}
              onChange={(event) => {
                setName(event.target.value)
                mutation.reset()
              }}
              autoComplete="off"
            />
          </label>
        </div>
        <div className="form-actions">
          <button
            type="submit"
            disabled={mutation.isPending || name.trim() === monitor.name}
          >
            {mutation.isPending ? t('monitor.rename.submitting') : t('monitor.rename.submit')}
          </button>
        </div>
        {fieldErrors.length > 0 && (
          <ul className="field-errors form-field-wide" role="alert">
            {fieldErrors.map((message) => <li key={message}>{message}</li>)}
          </ul>
        )}
      </form>
      {mutation.isSuccess && (
        <p className="success" role="status">{t('monitor.rename.done')}</p>
      )}
      {mutation.isError && fieldErrors.length === 0 && (
        <p className="error" role="alert">{generalError}</p>
      )}
    </section>
  )
}
