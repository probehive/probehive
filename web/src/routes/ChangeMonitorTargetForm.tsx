import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'

import {
  ApiError,
  createHTTPRevision,
  type MonitorResponse,
  type MonitorRevisionResponse,
} from '../api/monitors'
import { useTranslation } from '../i18n/context'
import { monitorQueryKey, monitorsQueryKey } from './monitorQueries'

export default function ChangeMonitorTargetForm({ monitor }: { monitor: MonitorResponse }) {
  const { t, translateError, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const [url, setURL] = useState('')
  const listKey = monitorsQueryKey(monitor.organizationId, monitor.projectId)
  const detailKey = monitorQueryKey(monitor.organizationId, monitor.projectId, monitor.id)
  const mutation = useMutation<MonitorRevisionResponse, unknown, string>({
    mutationFn: (nextURL) => createHTTPRevision(
      monitor.organizationId,
      monitor.projectId,
      monitor.id,
      nextURL,
    ),
    onSuccess: async (revision) => {
      setURL('')
      queryClient.setQueryData(detailKey, {
        ...monitor,
        latestRevisionNumber: revision.revisionNumber,
        updatedAt: revision.createdAt,
      })
      await queryClient.invalidateQueries({ queryKey: listKey, exact: true })
    },
  })

  if (monitor.state === 'archived' || monitor.latestRevisionNumber === 0) {
    return null
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (url.trim() === '') {
      return
    }
    mutation.mutate(url)
  }

  const fieldErrors = mutation.error instanceof ApiError && mutation.error.status === 400
    ? (mutation.error.problem.errors?.['checkConfiguration.url'] ?? []).map(translateError)
    : []
  const generalError = mutation.error instanceof ApiError
    ? translateProblem(mutation.error.problem)
    : t('monitor.targetEdit.failed')

  return (
    <section className="monitor-target-section" aria-labelledby="monitor-target-heading">
      <h2 id="monitor-target-heading">{t('monitor.targetEdit.heading')}</h2>
      <form className="monitor-form" onSubmit={submit} aria-label={t('monitor.targetEdit.form')}>
        <div className="form-field form-field-wide">
          <label>
            {t('monitor.targetEdit.url')}
            <input
              name="url"
              type="url"
              value={url}
              onChange={(event) => {
                setURL(event.target.value)
                mutation.reset()
              }}
              maxLength={2048}
              placeholder="https://example.com/health"
              required
              autoComplete="url"
            />
          </label>
        </div>
        <div className="form-actions form-field-wide">
          <button type="submit" disabled={mutation.isPending || url.trim() === ''}>
            {mutation.isPending
              ? t('monitor.targetEdit.submitting')
              : t('monitor.targetEdit.submit')}
          </button>
        </div>
        {fieldErrors.length > 0 && (
          <ul className="field-errors form-field-wide" role="alert">
            {fieldErrors.map((message) => <li key={message}>{message}</li>)}
          </ul>
        )}
      </form>
      {mutation.isSuccess && (
        <p className="success" role="status">
          {t('monitor.targetEdit.done', { revision: mutation.data.revisionNumber })}
        </p>
      )}
      {mutation.isError && fieldErrors.length === 0 && (
        <p className="error" role="alert">{generalError}</p>
      )}
    </section>
  )
}
