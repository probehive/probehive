import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router'

import {
  activateMonitor,
  ApiError,
  createHTTPMonitor,
  createHTTPRevision,
  listMonitorInventory,
  type MonitorHealthState,
  type MonitorInventoryItemResponse,
  type MonitorInventoryQuery,
  type MonitorInventorySort,
  type MonitorMaintenanceState,
  type MonitorResponse,
  type MonitorRunOutcome,
  type MonitorState,
} from '../api/monitors'
import { organizationOverviewQueryKey } from '../api/overview'
import { useTranslation } from '../i18n/context'
import { monitorInventoryQueryKey, monitorsQueryKey } from './monitorQueries'
import MonitorInventoryActions from './MonitorInventoryActions'

interface MonitorInventoryPanelProps {
  organizationId: string
  projectId: string
}

interface SetupRequest {
  name: string
  url: string
  intervalSeconds: number
}

const pageSize = 10
const states: MonitorState[] = ['draft', 'active', 'paused', 'archived']
const healthStates: MonitorHealthState[] = ['notEvaluated', 'unknown', 'healthy', 'degraded', 'down']
const runOutcomes: MonitorRunOutcome[] = ['notRun', 'inProgress', 'passed', 'failed', 'errored', 'timedout', 'cancelled', 'skipped']
const maintenanceStates: MonitorMaintenanceState[] = ['none', 'upcoming', 'active']
const sortFields: MonitorInventorySort[] = ['name', 'createdAt', 'updatedAt']

function validationMessages(error: unknown, field: string, translateError: ReturnType<typeof useTranslation>['translateError']): string[] {
  if (!(error instanceof ApiError) || error.status !== 400) return []
  return (error.problem.errors?.[field] ?? []).map(translateError)
}

function validValue<T extends string>(value: string | null, values: readonly T[]): T | undefined {
  return value && values.includes(value as T) ? value as T : undefined
}

export default function MonitorInventoryPanel({ organizationId, projectId }: MonitorInventoryPanelProps) {
  const { t, formatDateTime, translateError, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [searchInput, setSearchInput] = useState(searchParams.get('monitorSearch') ?? '')
  const [name, setName] = useState('')
  const [url, setURL] = useState('')
  const [intervalSeconds, setIntervalSeconds] = useState('60')
  const [setupMonitor, setSetupMonitor] = useState<MonitorResponse | null>(null)

  useEffect(() => setSearchInput(searchParams.get('monitorSearch') ?? ''), [searchParams])

  const inventoryQuery: MonitorInventoryQuery = {
    search: searchParams.get('monitorSearch') || undefined,
    state: validValue(searchParams.get('monitorState'), states),
    health: validValue(searchParams.get('monitorHealth'), healthStates),
    runOutcome: validValue(searchParams.get('monitorRun'), runOutcomes),
    maintenance: validValue(searchParams.get('monitorMaintenance'), maintenanceStates),
    sort: validValue(searchParams.get('monitorSort'), sortFields) ?? 'name',
    direction: searchParams.get('monitorDirection') === 'desc' ? 'desc' : 'asc',
    page: Math.min(1000, Math.max(1, Number.parseInt(searchParams.get('monitorPage') ?? '1', 10) || 1)),
    pageSize,
  }
  const queryKey = monitorInventoryQueryKey(organizationId, projectId, inventoryQuery)
  const query = useQuery({
    queryKey,
    queryFn: () => listMonitorInventory(organizationId, projectId, inventoryQuery),
  })
  const mutation = useMutation<MonitorResponse, unknown, SetupRequest>({
    mutationFn: async (request) => {
      let monitor = setupMonitor
      if (monitor === null) {
        monitor = await createHTTPMonitor(organizationId, projectId, request.name, request.intervalSeconds)
        setSetupMonitor(monitor)
      }
      if (monitor.latestRevisionNumber === 0) {
        const revision = await createHTTPRevision(organizationId, projectId, monitor.id, request.url)
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
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: monitorsQueryKey(organizationId, projectId) }),
        queryClient.invalidateQueries({ queryKey: organizationOverviewQueryKey(organizationId), exact: true }),
      ])
    },
  })

  function updateParams(updates: Record<string, string | undefined>, resetPage = true) {
    const next = new URLSearchParams(searchParams)
    Object.entries(updates).forEach(([key, value]) => value ? next.set(key, value) : next.delete(key))
    if (resetPage) next.delete('monitorPage')
    setSearchParams(next)
  }

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    updateParams({ monitorSearch: searchInput.trim() || undefined })
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
    draft: t('monitor.state.draft'), active: t('monitor.state.active'), paused: t('monitor.state.paused'), archived: t('monitor.state.archived'),
  }
  const healthLabels: Record<MonitorHealthState, string> = {
    notEvaluated: t('monitor.inventory.health.notEvaluated'), unknown: t('health.state.unknown'), healthy: t('health.state.healthy'), degraded: t('health.state.degraded'), down: t('health.state.down'),
  }
  const runLabels: Record<MonitorRunOutcome, string> = {
    notRun: t('monitor.inventory.run.notRun'), inProgress: t('monitor.inventory.run.inProgress'), passed: t('monitor.inventory.run.passed'), failed: t('monitor.inventory.run.failed'), errored: t('monitor.inventory.run.errored'), timedout: t('monitor.inventory.run.timedout'), cancelled: t('monitor.inventory.run.cancelled'), skipped: t('monitor.inventory.run.skipped'),
  }
  const maintenanceLabels: Record<MonitorMaintenanceState, string> = {
    none: t('monitor.inventory.maintenance.none'), upcoming: t('maintenance.status.upcoming'), active: t('maintenance.status.active'),
  }
  const nameErrors = validationMessages(mutation.error, 'name', translateError)
  const intervalErrors = validationMessages(mutation.error, 'intervalSeconds', translateError)
  const urlErrors = validationMessages(mutation.error, 'checkConfiguration.url', translateError)
  const hasFieldError = nameErrors.length + intervalErrors.length + urlErrors.length > 0
  const needsRevision = setupMonitor === null || setupMonitor.latestRevisionNumber === 0
  const submitLabel = mutation.isPending ? t('monitor.setup.submitting') : setupMonitor === null ? t('monitor.setup.create') : needsRevision ? t('monitor.setup.configure') : t('monitor.setup.activate')
  const totalPages = query.data ? Math.max(1, Math.ceil(query.data.total / pageSize)) : 1
  useEffect(() => {
    if (!query.data || inventoryQuery.page <= totalPages) return
    const next = new URLSearchParams(searchParams)
    next.set('monitorPage', String(totalPages))
    setSearchParams(next)
  }, [inventoryQuery.page, query.data, searchParams, setSearchParams, totalPages])
  const hasFilters = Boolean(inventoryQuery.search || inventoryQuery.state || inventoryQuery.health || inventoryQuery.runOutcome || inventoryQuery.maintenance)
  const sortLabels: Record<MonitorInventorySort, string> = {
    name: t('monitor.inventory.sort.name'),
    createdAt: t('monitor.inventory.sort.createdAt'),
    updatedAt: t('monitor.inventory.sort.updatedAt'),
  }

  return (
    <section className="monitor-section" id="monitors" aria-labelledby="monitors-heading">
      <div className="section-heading">
        <div>
          <p className="eyebrow">{t('organization.defaultProject')}</p>
          <h2 id="monitors-heading">{t('monitor.heading')}</h2>
        </div>
        <span className="section-count" aria-label={t('monitor.inventory.resultCount')}>{query.data ? query.data.total : '-'}</span>
      </div>
      <form className="monitor-inventory-toolbar" onSubmit={submitSearch} aria-label={t('monitor.inventory.filters')}>
        <label className="monitor-inventory-search">{t('monitor.inventory.search')}<input value={searchInput} onChange={(event) => setSearchInput(event.target.value)} maxLength={100} placeholder={t('monitor.inventory.searchPlaceholder')} /></label>
        <label>{t('monitor.state')}<select value={inventoryQuery.state ?? ''} onChange={(event) => updateParams({ monitorState: event.target.value || undefined })}><option value="">{t('monitor.inventory.all')}</option>{states.map((state) => <option key={state} value={state}>{stateLabels[state]}</option>)}</select></label>
        <label>{t('monitor.inventory.health')}<select value={inventoryQuery.health ?? ''} onChange={(event) => updateParams({ monitorHealth: event.target.value || undefined })}><option value="">{t('monitor.inventory.all')}</option>{healthStates.map((state) => <option key={state} value={state}>{healthLabels[state]}</option>)}</select></label>
        <label>{t('monitor.inventory.run')}<select value={inventoryQuery.runOutcome ?? ''} onChange={(event) => updateParams({ monitorRun: event.target.value || undefined })}><option value="">{t('monitor.inventory.all')}</option>{runOutcomes.map((outcome) => <option key={outcome} value={outcome}>{runLabels[outcome]}</option>)}</select></label>
        <label>{t('monitor.inventory.maintenance')}<select value={inventoryQuery.maintenance ?? ''} onChange={(event) => updateParams({ monitorMaintenance: event.target.value || undefined })}><option value="">{t('monitor.inventory.all')}</option>{maintenanceStates.map((state) => <option key={state} value={state}>{maintenanceLabels[state]}</option>)}</select></label>
        <label>{t('monitor.inventory.sort')}<select value={inventoryQuery.sort} onChange={(event) => updateParams({ monitorSort: event.target.value })}>{sortFields.map((field) => <option key={field} value={field}>{sortLabels[field]}</option>)}</select></label>
        <label>{t('monitor.inventory.direction')}<select value={inventoryQuery.direction} onChange={(event) => updateParams({ monitorDirection: event.target.value })}><option value="asc">{t('monitor.inventory.direction.asc')}</option><option value="desc">{t('monitor.inventory.direction.desc')}</option></select></label>
        <button type="submit" className="button-secondary">{t('monitor.inventory.apply')}</button>
        {hasFilters && <button type="button" className="button-secondary" onClick={() => { setSearchInput(''); updateParams({ monitorSearch: undefined, monitorState: undefined, monitorHealth: undefined, monitorRun: undefined, monitorMaintenance: undefined }) }}>{t('monitor.inventory.clear')}</button>}
      </form>
      {query.isPending && <p>{t('monitor.loading')}</p>}
      {query.isError && <p className="error" role="alert">{query.error instanceof ApiError ? translateProblem(query.error.problem) : t('monitor.loadFailed')}</p>}
      {query.data && query.data.total === 0 && <p className="muted">{hasFilters ? t('monitor.inventory.noResults') : t('monitor.empty')}</p>}
      {query.data && query.data.items.length > 0 && (
        <div className="table-scroll monitor-inventory-scroll">
            <table className="monitor-table monitor-inventory-table">
              <thead><tr><th scope="col">{t('monitor.name')}</th><th scope="col">{t('monitor.state')}</th><th scope="col">{t('monitor.inventory.health')}</th><th scope="col">{t('monitor.inventory.run')}</th><th scope="col">{t('monitor.inventory.maintenance')}</th><th scope="col">{t('monitor.updated')}</th><th scope="col">{t('monitor.inventory.actions')}</th></tr></thead>
              <tbody>{query.data.items.map((item: MonitorInventoryItemResponse) => {
                const health = item.health?.state ?? 'notEvaluated'
                const run = item.lastRun?.outcome ?? 'notRun'
                return <tr key={item.monitor.id}>
                  <th scope="row" data-label={t('monitor.name')}><Link className="monitor-link" to={`/organizations/${organizationId}/projects/${projectId}/monitors/${item.monitor.id}`}>{item.monitor.name}<span aria-hidden="true"> &rarr;</span></Link><small>{item.monitor.checkType} · {t('monitor.intervalValue', { seconds: item.monitor.intervalSeconds })}</small></th>
                  <td data-label={t('monitor.state')}><span className="monitor-state" data-state={item.monitor.state}>{stateLabels[item.monitor.state]}</span></td>
                  <td data-label={t('monitor.inventory.health')}><span className="monitor-fact" data-state={health}>{healthLabels[health]}</span>{item.health && <small>{formatDateTime(item.health.updatedAt)}</small>}</td>
                  <td data-label={t('monitor.inventory.run')}><span className="monitor-fact" data-state={run}>{runLabels[run]}</span>{item.lastRun && <small>{formatDateTime(item.lastRun.scheduledFor)}</small>}</td>
                  <td data-label={t('monitor.inventory.maintenance')}><span className="monitor-fact" data-state={item.maintenance.state}>{maintenanceLabels[item.maintenance.state]}</span>{item.maintenance.startsAt && <small>{formatDateTime(item.maintenance.startsAt)}</small>}</td>
                  <td data-label={t('monitor.updated')}>{formatDateTime(item.monitor.updatedAt)}</td>
                  <td data-label={t('monitor.inventory.actions')}><MonitorInventoryActions monitor={item.monitor} onResume={resumeSetup} /></td>
                </tr>
              })}</tbody>
            </table>
        </div>
      )}
      {query.data && query.data.total > 0 && query.data.items.length === 0 && (
        <p className="muted" role="status">{t('monitor.inventory.pageEmpty')}</p>
      )}
      {query.data && query.data.total > 0 && (
        <nav className="monitor-inventory-pagination" aria-label={t('monitor.inventory.pagination')}>
          <button type="button" className="button-secondary" onClick={() => updateParams({ monitorPage: String(inventoryQuery.page - 1) }, false)} disabled={inventoryQuery.page <= 1}>{t('monitor.inventory.previous')}</button>
          <span>{t('monitor.inventory.pageOf', { page: inventoryQuery.page, pages: totalPages })}</span>
          <button type="button" className="button-secondary" onClick={() => updateParams({ monitorPage: String(inventoryQuery.page + 1) }, false)} disabled={inventoryQuery.page >= totalPages}>{t('monitor.inventory.next')}</button>
        </nav>
      )}

      <h3>{t('monitor.setup.heading')}</h3>
      {setupMonitor && <p className="form-context" role="status">{t('monitor.setup.resuming', { name: setupMonitor.name })}</p>}
      <form className="monitor-form" onSubmit={(event) => { event.preventDefault(); mutation.mutate({ name, url, intervalSeconds: Number(intervalSeconds) }) }} aria-label={t('monitor.setup.form')}>
        <div className="form-field"><label>{t('monitor.setup.name')}<input name="name" value={name} onChange={(event) => setName(event.target.value)} maxLength={100} required disabled={setupMonitor !== null} autoComplete="off" /></label><ul className="field-errors" role="alert">{nameErrors.map((message) => <li key={message}>{message}</li>)}</ul></div>
        <div className="form-field"><label>{t('monitor.setup.interval')}<input name="intervalSeconds" type="number" value={intervalSeconds} onChange={(event) => setIntervalSeconds(event.target.value)} min="30" max="86400" step="1" required disabled={setupMonitor !== null} /></label><ul className="field-errors" role="alert">{intervalErrors.map((message) => <li key={message}>{message}</li>)}</ul></div>
        {needsRevision && <div className="form-field form-field-wide"><label>{t('monitor.setup.url')}<input name="url" type="url" value={url} onChange={(event) => setURL(event.target.value)} maxLength={2048} placeholder="https://example.com/health" required autoComplete="url" /></label><ul className="field-errors" role="alert">{urlErrors.map((message) => <li key={message}>{message}</li>)}</ul></div>}
        <div className="form-actions form-field-wide"><button type="submit" disabled={mutation.isPending}>{submitLabel}</button>{setupMonitor && <button type="button" className="button-secondary" onClick={cancelResume} disabled={mutation.isPending}>{t('monitor.setup.cancel')}</button>}</div>
      </form>
      {mutation.isError && !hasFieldError && <p className="error" role="alert">{mutation.error instanceof ApiError ? translateProblem(mutation.error.problem) : t('monitor.setup.failed')}</p>}
      {mutation.data && <p className="success" role="status">{t('monitor.setup.done', { name: mutation.data.name })}</p>}
    </section>
  )
}
