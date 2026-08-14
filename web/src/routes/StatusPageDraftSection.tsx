import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'

import { ApiError } from '../api/http'
import { listMonitors, type MonitorResponse } from '../api/monitors'
import {
  getStatusPageDraft,
  publishStatusPage,
  replaceStatusPageDraft,
  revokeStatusPage,
  type StatusComponentInput,
  type StatusPageDraftResponse,
} from '../api/statusPage'
import { useTranslation } from '../i18n/context'
import { monitorsQueryKey } from './monitorQueries'

function statusPageQueryKey(organizationId: string) {
  return ['status-page-draft', organizationId] as const
}

function initialComponents(
  draft: StatusPageDraftResponse | null,
): StatusComponentInput[] {
  return (draft?.components ?? [])
    .slice()
    .sort((left, right) => left.position - right.position)
    .map(({ monitorId, label }) => ({ monitorId, label }))
}

export default function StatusPageDraftSection({
  organizationId,
  projectId,
}: {
  organizationId: string
  projectId: string
}) {
  const { t, formatDateTime, translateError, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const draftKey = statusPageQueryKey(organizationId)
  const monitorsKey = monitorsQueryKey(organizationId, projectId)
  const draftQuery = useQuery({
    queryKey: draftKey,
    queryFn: () => getStatusPageDraft(organizationId),
  })
  const monitorsQuery = useQuery({
    queryKey: monitorsKey,
    queryFn: () => listMonitors(organizationId, projectId),
  })
  const [title, setTitle] = useState('')
  const [components, setComponents] = useState<StatusComponentInput[]>([])
  const [loadedVersion, setLoadedVersion] = useState<number | null>(null)
  const [publicUrl, setPublicUrl] = useState<string | null>(null)

  useEffect(() => {
    if (!draftQuery.isSuccess) {
      return
    }
    const draft = draftQuery.data
    if (loadedVersion === (draft?.version ?? 0)) {
      return
    }
    setTitle(draft?.title ?? '')
    setComponents(initialComponents(draft))
    setLoadedVersion(draft?.version ?? 0)
  }, [draftQuery.data, draftQuery.isSuccess, loadedVersion])

  const mutation = useMutation<StatusPageDraftResponse, unknown>({
    mutationFn: () => replaceStatusPageDraft(
      organizationId,
      title,
      draftQuery.data?.version ?? 0,
      components,
    ),
    onSuccess: (draft) => {
      queryClient.setQueryData(draftKey, draft)
      setLoadedVersion(draft.version)
    },
  })
  const publishMutation = useMutation({
    mutationFn: () => publishStatusPage(organizationId),
    onSuccess: (publication) => {
      setPublicUrl(publication.publicUrl)
      queryClient.setQueryData<StatusPageDraftResponse | null>(draftKey, (current) => current && ({
        ...current,
        publication: { publishedAt: publication.publishedAt },
      }))
    },
  })
  const revokeMutation = useMutation({
    mutationFn: () => revokeStatusPage(organizationId),
    onSuccess: () => {
      setPublicUrl(null)
      queryClient.setQueryData<StatusPageDraftResponse | null>(draftKey, (current) => current && ({
        ...current,
        publication: null,
      }))
    },
  })

  const availableMonitors = (monitorsQuery.data ?? []).filter(
    (monitor) => monitor.state !== 'archived',
  )
  const selected = new Set(components.map((component) => component.monitorId))

  function toggleMonitor(monitor: MonitorResponse, checked: boolean) {
    mutation.reset()
    setComponents((current) => {
      if (!checked) {
        return current.filter((component) => component.monitorId !== monitor.id)
      }
      if (current.some((component) => component.monitorId === monitor.id)) {
        return current
      }
      return [...current, { monitorId: monitor.id, label: monitor.name }]
    })
  }

  function updateLabel(monitorId: string, label: string) {
    mutation.reset()
    setComponents((current) => current.map((component) =>
      component.monitorId === monitorId ? { ...component, label } : component,
    ))
  }

  function removeComponent(monitorId: string) {
    mutation.reset()
    setComponents((current) => current.filter(
      (component) => component.monitorId !== monitorId,
    ))
  }

  function move(index: number, offset: -1 | 1) {
    const destination = index + offset
    if (destination < 0 || destination >= components.length) {
      return
    }
    mutation.reset()
    setComponents((current) => {
      const reordered = current.slice()
      const [component] = reordered.splice(index, 1)
      reordered.splice(destination, 0, component as StatusComponentInput)
      return reordered
    })
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate()
  }

  const fieldErrors = mutation.error instanceof ApiError && mutation.error.status === 400
    ? Object.values(mutation.error.problem.errors ?? {}).flat().map(translateError)
    : []
  const mutationError = mutation.error instanceof ApiError
    ? translateProblem(mutation.error.problem)
    : t('statusPage.save.failed')
  const canSave = title.trim().length > 0 && title.trim().length <= 100 &&
    components.length > 0 && components.length <= 50 &&
    components.every((component) => component.label.trim().length > 0 && component.label.trim().length <= 100)

  return (
    <section className="status-page-section" id="status-page" aria-labelledby="status-page-heading">
      <div className="section-heading">
        <div>
          <h2 id="status-page-heading">{t('statusPage.heading')}</h2>
          <p className="muted">{t('statusPage.private')}</p>
        </div>
      </div>
      {draftQuery.isPending && <p>{t('statusPage.loading')}</p>}
      {draftQuery.isError && (
        <p className="error" role="alert">
          {draftQuery.error instanceof ApiError && draftQuery.error.status === 403
            ? t('statusPage.administratorOnly')
            : t('statusPage.loadFailed')}
        </p>
      )}
      {draftQuery.isSuccess && monitorsQuery.isError && (
        <p className="error" role="alert">{t('statusPage.monitorsFailed')}</p>
      )}
      {draftQuery.isSuccess && monitorsQuery.isSuccess && (
        <form className="status-page-form" onSubmit={submit} aria-label={t('statusPage.form')}>
          <label className="form-field">
            {t('statusPage.title')}
            <input
              value={title}
              maxLength={100}
              onChange={(event) => { setTitle(event.target.value); mutation.reset() }}
            />
          </label>
          <fieldset>
            <legend>{t('statusPage.components')}</legend>
            {availableMonitors.length === 0 && (
              <p className="muted">{t('statusPage.noMonitors')}</p>
            )}
            <div className="status-monitor-options">
              {availableMonitors.map((monitor) => (
                <label key={monitor.id}>
                  <input
                    type="checkbox"
                    checked={selected.has(monitor.id)}
                    onChange={(event) => toggleMonitor(monitor, event.target.checked)}
                  />
                  <span>{monitor.name}</span>
                </label>
              ))}
            </div>
          </fieldset>
          {components.length > 0 && (
            <ol className="status-component-list">
              {components.map((component, index) => {
                const monitor = availableMonitors.find((candidate) => candidate.id === component.monitorId)
                return (
                  <li key={component.monitorId}>
                    <div>
                      <strong>{monitor?.name ?? t('statusPage.unavailableMonitor')}</strong>
                      <label>
                        {t('statusPage.publicLabel')}
                        <input
                          value={component.label}
                          maxLength={100}
                          onChange={(event) => updateLabel(component.monitorId, event.target.value)}
                        />
                      </label>
                    </div>
                    <div className="status-order-actions" aria-label={t('statusPage.order', { label: component.label })}>
                      <button
                        type="button"
                        className="button-secondary"
                        disabled={index === 0}
                        onClick={() => move(index, -1)}
                        aria-label={t('statusPage.moveUp', { label: component.label })}
                      >
                        {t('statusPage.up')}
                      </button>
                      <button
                        type="button"
                        className="button-secondary"
                        disabled={index === components.length - 1}
                        onClick={() => move(index, 1)}
                        aria-label={t('statusPage.moveDown', { label: component.label })}
                      >
                        {t('statusPage.down')}
                      </button>
                      <button
                        type="button"
                        className="button-secondary"
                        onClick={() => removeComponent(component.monitorId)}
                        aria-label={t('statusPage.removeLabel', { label: component.label })}
                      >
                        {t('statusPage.remove')}
                      </button>
                    </div>
                  </li>
                )
              })}
            </ol>
          )}
          <button type="submit" disabled={!canSave || mutation.isPending}>
            {mutation.isPending ? t('statusPage.saving') : t('statusPage.save')}
          </button>
          <div className="status-publication">
            <h3>{t('statusPage.publication.heading')}</h3>
            {draftQuery.data?.publication ? (
              <>
                <p className="muted">
                  {t('statusPage.publication.published', {
                    time: formatDateTime(draftQuery.data.publication.publishedAt),
                  })}
                </p>
                {publicUrl && (
                  <p className="status-public-url">
                    <a href={publicUrl}>{publicUrl}</a>
                  </p>
                )}
                <p className="muted">{t('statusPage.publication.once')}</p>
                <button
                  type="button"
                  className="button-danger"
                  disabled={revokeMutation.isPending}
                  onClick={() => revokeMutation.mutate()}
                >
                  {revokeMutation.isPending ? t('statusPage.publication.revoking') : t('statusPage.publication.revoke')}
                </button>
              </>
            ) : (
              <>
                <p className="muted">{t('statusPage.publication.unpublished')}</p>
                <button
                  type="button"
                  disabled={!draftQuery.data || publishMutation.isPending}
                  onClick={() => publishMutation.mutate()}
                >
                  {publishMutation.isPending ? t('statusPage.publication.publishing') : t('statusPage.publication.publish')}
                </button>
              </>
            )}
            {publishMutation.isError && <p className="error" role="alert">{t('statusPage.publication.failed')}</p>}
            {revokeMutation.isError && <p className="error" role="alert">{t('statusPage.publication.revokeFailed')}</p>}
            {revokeMutation.isSuccess && <p className="success" role="status">{t('statusPage.publication.revoked')}</p>}
          </div>
          {fieldErrors.length > 0 && (
            <ul className="field-errors" role="alert">
              {fieldErrors.map((message) => <li key={message}>{message}</li>)}
            </ul>
          )}
        </form>
      )}
      {mutation.isSuccess && (
        <p className="success" role="status">{t('statusPage.saved')}</p>
      )}
      {mutation.isError && fieldErrors.length === 0 && (
        <p className="error" role="alert">{mutationError}</p>
      )}
    </section>
  )
}
