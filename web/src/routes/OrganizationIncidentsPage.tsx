import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { Link, useParams, useSearchParams } from 'react-router'

import { ApiError } from '../api/http'
import {
  listOrganizationIncidents,
  type IncidentInboxItemResponse,
  type IncidentInboxState,
  type IncidentState,
} from '../api/incidents'
import { getOrganization } from '../api/organizations'
import { useTranslation } from '../i18n/context'
import type { MessageKey } from '../i18n/en'

type InboxFilter = 'all' | IncidentInboxState
type MonitorState = IncidentInboxItemResponse['monitor']['state']
type HealthState = NonNullable<IncidentInboxItemResponse['health']>['state']
type MaintenanceState = NonNullable<IncidentInboxItemResponse['maintenance']>['state']

const filters: InboxFilter[] = ['active', 'all', 'open', 'acknowledged', 'resolved']
const filterKeys: Record<InboxFilter, MessageKey> = {
  active: 'incident.inbox.filter.active',
  all: 'incident.inbox.filter.all',
  open: 'incident.inbox.filter.open',
  acknowledged: 'incident.inbox.filter.acknowledged',
  resolved: 'incident.inbox.filter.resolved',
}
const incidentStateKeys: Record<IncidentState, MessageKey> = {
  open: 'incident.state.open',
  acknowledged: 'incident.state.acknowledged',
  resolved: 'incident.state.resolved',
}
const monitorStateKeys: Record<MonitorState, MessageKey> = {
  draft: 'monitor.state.draft',
  active: 'monitor.state.active',
  paused: 'monitor.state.paused',
  archived: 'monitor.state.archived',
}
const healthStateKeys: Record<HealthState, MessageKey> = {
  unknown: 'health.state.unknown',
  healthy: 'health.state.healthy',
  degraded: 'health.state.degraded',
  down: 'health.state.down',
}
const maintenanceStateKeys: Record<MaintenanceState, MessageKey> = {
  active: 'incident.inbox.maintenance.active',
  upcoming: 'incident.inbox.maintenance.upcoming',
}

function validFilter(value: string | null): InboxFilter {
  return value !== null && filters.includes(value as InboxFilter)
    ? value as InboxFilter
    : 'active'
}

function monitorPath(item: IncidentInboxItemResponse): string {
  return '/organizations/' + encodeURIComponent(item.incident.organizationId) +
    '/projects/' + encodeURIComponent(item.incident.projectId) +
    '/monitors/' + encodeURIComponent(item.incident.monitorId)
}

function incidentPath(item: IncidentInboxItemResponse): string {
  return monitorPath(item) + '/incidents/' + encodeURIComponent(item.incident.id)
}

function runPath(item: IncidentInboxItemResponse, runId: string): string {
  return monitorPath(item) + '/runs/' + encodeURIComponent(runId)
}

function IncidentStateBadge({ state }: { state: IncidentState }) {
  const { t } = useTranslation()
  return (
    <span className="incident-state" data-state={state}>
      {t(incidentStateKeys[state])}
    </span>
  )
}

function IncidentInboxTable({ items }: { items: IncidentInboxItemResponse[] }) {
  const { t, formatDateTime } = useTranslation()

  return (
    <div className="incident-inbox-scroll">
      <table className="incident-inbox-table">
        <thead>
          <tr>
            <th scope="col">{t('incident.inbox.monitor')}</th>
            <th scope="col">{t('incident.inbox.lifecycle')}</th>
            <th scope="col">{t('incident.inbox.health')}</th>
            <th scope="col">{t('incident.inbox.maintenance')}</th>
            <th scope="col">{t('incident.inbox.evidence')}</th>
            <th scope="col"><span className="visually-hidden">{t('incident.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.incident.id}>
              <th scope="row" data-label={t('incident.inbox.monitor')}>
                <Link className="monitor-link" to={monitorPath(item)}>{item.monitor.name}</Link>
                <small>{t('incident.inbox.monitorState', {
                  state: t(monitorStateKeys[item.monitor.state]),
                })}</small>
              </th>
              <td data-label={t('incident.inbox.lifecycle')}>
                <IncidentStateBadge state={item.incident.state} />
                <small>{t('incident.inbox.opened', {
                  time: formatDateTime(item.incident.createdAt),
                })}</small>
                <small>{t('incident.inbox.updated', {
                  time: formatDateTime(item.incident.updatedAt),
                })}</small>
              </td>
              <td data-label={t('incident.inbox.health')}>
                {item.health === null ? (
                  <span className="muted">{t('incident.inbox.health.notEvaluated')}</span>
                ) : (
                  <>
                    <span className="health-state" data-state={item.health.state}>
                      {t(healthStateKeys[item.health.state])}
                    </span>
                    <small>{t('incident.inbox.health.updated', {
                      time: formatDateTime(item.health.updatedAt),
                    })}</small>
                  </>
                )}
              </td>
              <td data-label={t('incident.inbox.maintenance')}>
                {item.maintenance === null ? (
                  <span className="muted">{t('incident.inbox.maintenance.none')}</span>
                ) : (
                  <>
                    <span className="maintenance-state" data-state={item.maintenance.state}>
                      {t(maintenanceStateKeys[item.maintenance.state])}
                    </span>
                    <small>{t('incident.inbox.maintenance.window', {
                      start: formatDateTime(item.maintenance.startsAt),
                      end: formatDateTime(item.maintenance.endsAt),
                    })}</small>
                  </>
                )}
              </td>
              <td data-label={t('incident.inbox.evidence')}>
                {item.openingRun === null ? (
                  <span className="muted">{t('incident.inbox.run.none')}</span>
                ) : item.openingRun.available ? (
                  <>
                    <Link className="run-detail-link" to={runPath(item, item.openingRun.id)}>
                      {t('incident.inbox.run.view')}
                    </Link>
                    <small>{formatDateTime(item.openingRun.scheduledFor)}</small>
                  </>
                ) : (
                  <>
                    <span className="muted">{t('incident.inbox.run.unavailable')}</span>
                    <small>{formatDateTime(item.openingRun.scheduledFor)}</small>
                  </>
                )}
              </td>
              <td data-label={t('incident.actions')}>
                <Link className="incident-detail-link" to={incidentPath(item)}>
                  {t('incident.view')}
                </Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export default function OrganizationIncidentsPage() {
  const { organizationId } = useParams<'organizationId'>()
  const [searchParams, setSearchParams] = useSearchParams()
  const { t } = useTranslation()
  const filter = validFilter(searchParams.get('state'))
  const apiState = filter === 'all' ? undefined : filter
  const organizationQuery = useQuery({
    queryKey: ['organizations', organizationId],
    queryFn: () => getOrganization(organizationId ?? ''),
    enabled: organizationId !== undefined,
  })
  const inboxQuery = useInfiniteQuery({
    queryKey: ['organization-incidents', organizationId, apiState ?? 'all'],
    queryFn: ({ pageParam }) => listOrganizationIncidents(
      organizationId ?? '',
      apiState,
      pageParam === '' ? undefined : pageParam,
    ),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    enabled: organizationId !== undefined,
  })
  const items = inboxQuery.data?.pages.flatMap((page) => page.items) ?? []

  function changeFilter(value: InboxFilter) {
    const next = new URLSearchParams(searchParams)
    if (value === 'active') next.delete('state')
    else next.set('state', value)
    setSearchParams(next)
  }

  if (organizationQuery.isPending) return <p>{t('organization.loading')}</p>
  if (organizationQuery.isError) {
    const notFound = organizationQuery.error instanceof ApiError &&
      organizationQuery.error.status === 404
    return (
      <p className="error" role="alert">
        {notFound ? t('organization.notFound') : t('organization.loadFailed')}
      </p>
    )
  }

  return (
    <section className="organization-incidents-page">
      <p className="breadcrumb">
        <Link to={'/organizations/' + organizationQuery.data.id}>
          {organizationQuery.data.displayName}
        </Link>
      </p>
      <header className="page-heading">
        <div>
          <p className="eyebrow">{t('organization.eyebrow')}</p>
          <h1>{t('incident.inbox.heading')}</h1>
          <p className="page-intro">{organizationQuery.data.displayName}</p>
        </div>
      </header>
      <section className="incident-inbox" aria-labelledby="incident-inbox-heading">
        <div className="section-heading">
          <div>
            <p className="eyebrow">{t('incident.inbox.eyebrow')}</p>
            <h2 id="incident-inbox-heading">{t('incident.inbox.recent')}</h2>
          </div>
          <label className="incident-inbox-filter">
            <span>{t('incident.inbox.filter')}</span>
            <select value={filter} onChange={(event) => changeFilter(event.target.value as InboxFilter)}>
              {filters.map((value) => (
                <option key={value} value={value}>{t(filterKeys[value])}</option>
              ))}
            </select>
          </label>
        </div>
        {inboxQuery.isPending && <p>{t('incident.inbox.loading')}</p>}
        {inboxQuery.isError && items.length === 0 && (
          <div className="error" role="alert">
            <p>{inboxQuery.error instanceof ApiError && inboxQuery.error.status === 403
              ? t('incident.inbox.permission')
              : t('incident.inbox.loadFailed')}</p>
            <button type="button" className="button-secondary button-compact" onClick={() => void inboxQuery.refetch()}>
              {t('overview.retry')}
            </button>
          </div>
        )}
        {!inboxQuery.isPending && !inboxQuery.isError && items.length === 0 && (
          <div className="empty-state incident-inbox-empty">
            <p>{t('incident.inbox.empty')}</p>
            {filter !== 'active' && (
              <button type="button" className="button-secondary button-compact" onClick={() => changeFilter('active')}>
                {t('incident.inbox.reset')}
              </button>
            )}
          </div>
        )}
        {items.length > 0 && <IncidentInboxTable items={items} />}
        {inboxQuery.isFetchNextPageError && (
          <div className="error incident-inbox-page-error" role="alert">
            <p>{t('incident.inbox.loadMoreFailed')}</p>
            <button
              type="button"
              className="button-secondary button-compact"
              onClick={() => void inboxQuery.fetchNextPage()}
            >
              {t('overview.retry')}
            </button>
          </div>
        )}
        {inboxQuery.hasNextPage && items.length > 0 && (
          <button
            type="button"
            className="button-secondary incident-inbox-load"
            disabled={inboxQuery.isFetchingNextPage}
            onClick={() => void inboxQuery.fetchNextPage()}
          >
            {inboxQuery.isFetchingNextPage
              ? t('incident.inbox.loadingMore')
              : t('incident.inbox.loadMore')}
          </button>
        )}
      </section>
    </section>
  )
}
