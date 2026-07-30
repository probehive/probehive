import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'

import {
  activateMonitor,
  ApiError,
  createHTTPMonitor,
  createHTTPRevision,
  listMonitors,
  type MonitorResponse,
  type MonitorState,
} from '../api/monitors'
import { useTranslation } from '../i18n/context'

interface MonitorsPanelProps {
  organizationId: string
  projectId: string
}

interface SetupRequest {
  name: string
  url: string
  intervalSeconds: number
}

function monitorsQueryKey(organizationId: string, projectId: string) {
  return ['monitors', organizationId, projectId] as const
}

function validationMessages(
  error: unknown,
  field: string,
  translateError: ReturnType<typeof useTranslation>['translateError'],
): string[] {
  if (!(error instanceof ApiError) || error.status !== 400) {
    return []
  }
  return (error.problem.errors?.[field] ?? []).map(translateError)
}

export default function MonitorsPanel({ organizationId, projectId }: MonitorsPanelProps) {
  const { t, formatDateTime, translateError, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [url, setURL] = useState('')
  const [intervalSeconds, setIntervalSeconds] = useState('60')
  const [setupMonitor, setSetupMonitor] = useState<MonitorResponse | null>(null)
  const queryKey = monitorsQueryKey(organizationId, projectId)
  const query = useQuery({
    queryKey,
    queryFn: () => listMonitors(organizationId, projectId),
  })
  const mutation = useMutation<MonitorResponse, unknown, SetupRequest>({
    mutationFn: async (request) => {
      let monitor = setupMonitor
      if (monitor === null) {
        monitor = await createHTTPMonitor(
          organizationId,
          projectId,
          request.name,
          request.intervalSeconds,
        )
        setSetupMonitor(monitor)
      }
      if (monitor.latestRevisionNumber === 0) {
        const revision = await createHTTPRevision(
          organizationId,
          projectId,
          monitor.id,
          request.url,
        )
        monitor = { ...monitor, latestRevisionNumber: revision.revisionNumber }
        setSetupMonitor(monitor)
      }
      if (monitor.state === 'draft') {
        monitor = await activateMonitor(organizationId, projectId, monitor.id)
        setSetupMonitor(monitor)
      }
      return monitor
    },
    onSuccess: () => {
      setName('')
      setURL('')
      setIntervalSeconds('60')
      setSetupMonitor(null)
    },
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey })
    },
  })

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate({ name, url, intervalSeconds: Number(intervalSeconds) })
  }

  function resumeSetup(monitor: MonitorResponse) {
    setSetupMonitor(monitor)
    setName(monitor.name)
    setURL('')
    setIntervalSeconds(String(monitor.intervalSeconds))
    mutation.reset()
  }

  function cancelResume() {
    setSetupMonitor(null)
    setName('')
    setURL('')
    setIntervalSeconds('60')
    mutation.reset()
  }

  const stateLabels: Record<MonitorState, string> = {
    draft: t('monitor.state.draft'),
    active: t('monitor.state.active'),
    paused: t('monitor.state.paused'),
    archived: t('monitor.state.archived'),
  }
  const nameErrors = validationMessages(mutation.error, 'name', translateError)
  const intervalErrors = validationMessages(mutation.error, 'intervalSeconds', translateError)
  const urlErrors = validationMessages(mutation.error, 'checkConfiguration.url', translateError)
  const hasFieldError = nameErrors.length + intervalErrors.length + urlErrors.length > 0
  const needsRevision = setupMonitor === null || setupMonitor.latestRevisionNumber === 0
  const submitLabel = mutation.isPending
    ? t('monitor.setup.submitting')
    : setupMonitor === null
      ? t('monitor.setup.create')
      : needsRevision
        ? t('monitor.setup.configure')
        : t('monitor.setup.activate')

  return (
    <section className="monitor-section">
      <h2>{t('monitor.heading')}</h2>
      {query.isPending && <p>{t('monitor.loading')}</p>}
      {query.isError && (
        <p className="error" role="alert">
          {t('monitor.loadFailed')}
        </p>
      )}
      {query.data && query.data.length === 0 && <p className="muted">{t('monitor.empty')}</p>}
      {query.data && query.data.length > 0 && (
        <div className="table-scroll">
          <table className="monitor-table">
            <thead>
              <tr>
                <th scope="col">{t('monitor.name')}</th>
                <th scope="col">{t('monitor.state')}</th>
                <th scope="col">{t('monitor.interval')}</th>
                <th scope="col">{t('monitor.revision')}</th>
                <th scope="col">{t('monitor.updated')}</th>
                <th scope="col">{t('monitor.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {query.data.map((monitor) => (
                <tr key={monitor.id}>
                  <th scope="row">{monitor.name}</th>
                  <td>
                    <span className="monitor-state" data-state={monitor.state}>
                      {stateLabels[monitor.state]}
                    </span>
                  </td>
                  <td>{t('monitor.intervalValue', { seconds: monitor.intervalSeconds })}</td>
                  <td>
                    {monitor.latestRevisionNumber === 0
                      ? t('monitor.unconfigured')
                      : `v${monitor.latestRevisionNumber}`}
                  </td>
                  <td>{formatDateTime(monitor.updatedAt)}</td>
                  <td>
                    {monitor.state === 'draft' ? (
                      <button
                        type="button"
                        className="button-secondary button-compact"
                        onClick={() => resumeSetup(monitor)}
                        disabled={mutation.isPending}
                      >
                        {monitor.latestRevisionNumber === 0
                          ? t('monitor.setup.resume')
                          : t('monitor.setup.activate')}
                      </button>
                    ) : (
                      <span className="muted">{t('monitor.configured')}</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <h3>{t('monitor.setup.heading')}</h3>
      {setupMonitor && (
        <p className="form-context" role="status">
          {t('monitor.setup.resuming', { name: setupMonitor.name })}
        </p>
      )}
      <form className="monitor-form" onSubmit={submit} aria-label={t('monitor.setup.form')}>
        <div className="form-field">
          <label>
            {t('monitor.setup.name')}
            <input
              name="name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={100}
              required
              disabled={setupMonitor !== null}
              autoComplete="off"
            />
          </label>
          <ul className="field-errors" role="alert">
            {nameErrors.map((message) => <li key={message}>{message}</li>)}
          </ul>
        </div>
        <div className="form-field">
          <label>
            {t('monitor.setup.interval')}
            <input
              name="intervalSeconds"
              type="number"
              value={intervalSeconds}
              onChange={(event) => setIntervalSeconds(event.target.value)}
              min="30"
              max="86400"
              step="1"
              required
              disabled={setupMonitor !== null}
            />
          </label>
          <ul className="field-errors" role="alert">
            {intervalErrors.map((message) => <li key={message}>{message}</li>)}
          </ul>
        </div>
        {needsRevision && (
          <div className="form-field form-field-wide">
            <label>
              {t('monitor.setup.url')}
              <input
                name="url"
                type="url"
                value={url}
                onChange={(event) => setURL(event.target.value)}
                maxLength={2048}
                placeholder="https://example.com/health"
                required
                autoComplete="url"
              />
            </label>
            <ul className="field-errors" role="alert">
              {urlErrors.map((message) => <li key={message}>{message}</li>)}
            </ul>
          </div>
        )}
        <div className="form-actions form-field-wide">
          <button type="submit" disabled={mutation.isPending}>{submitLabel}</button>
          {setupMonitor && (
            <button type="button" className="button-secondary" onClick={cancelResume} disabled={mutation.isPending}>
              {t('monitor.setup.cancel')}
            </button>
          )}
        </div>
      </form>
      {mutation.isError && !hasFieldError && (
        <p className="error" role="alert">
          {mutation.error instanceof ApiError
            ? translateProblem(mutation.error.problem)
            : t('monitor.setup.failed')}
        </p>
      )}
      {mutation.data && (
        <p className="success" role="status">
          {t('monitor.setup.done', { name: mutation.data.name })}
        </p>
      )}
    </section>
  )
}
