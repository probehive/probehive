import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { Link } from 'react-router'

import type { HealthCountsResponse, HealthState } from '../api/health'
import { ApiError } from '../api/http'
import {
  getIncident,
  listIncidents,
  type IncidentResponse,
  type IncidentState,
  type IncidentTimelineKind,
  type IncidentTimelineResponse,
} from '../api/incidents'
import { useTranslation } from '../i18n/context'

const incidentPageSize = 25

function incidentRoute(
  organizationId: string,
  projectId: string,
  monitorId: string,
  incidentId?: string,
): string {
  const monitorRoute = '/organizations/' + organizationId +
    '/projects/' + projectId + '/monitors/' + monitorId
  return incidentId === undefined ? monitorRoute : monitorRoute + '/incidents/' + incidentId
}

function runRoute(
  organizationId: string,
  projectId: string,
  monitorId: string,
  runId: string,
): string {
  return '/organizations/' + organizationId +
    '/projects/' + projectId + '/monitors/' + monitorId + '/runs/' + runId
}

function stateLabel(
  state: IncidentState,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  return t(`incident.state.${state}`)
}

function timelineKindLabel(
  kind: IncidentTimelineKind,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  return t(`incident.timeline.kind.${kind}`)
}

function healthStateLabel(
  state: HealthState,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  return t(`health.state.${state}`)
}

function DetailValue({ children }: { children: ReactNode }) {
  return <dd>{children ?? '--'}</dd>
}

function IncidentStateBadge({ state }: { state: IncidentState }) {
  const { t } = useTranslation()
  return (
    <span className="incident-state" data-state={state}>
      {stateLabel(state, t)}
    </span>
  )
}

function EvidenceCounts({ counts }: { counts: HealthCountsResponse }) {
  const { t, locale } = useTranslation()
  const number = new Intl.NumberFormat(locale)
  return (
    <dl className="incident-counts">
      <div><dt>{t('health.counts.configured')}</dt><dd>{number.format(counts.configured)}</dd></div>
      <div><dt>{t('health.counts.eligible')}</dt><dd>{number.format(counts.eligible)}</dd></div>
      <div><dt>{t('health.counts.responding')}</dt><dd>{number.format(counts.responding)}</dd></div>
      <div><dt>{t('health.counts.passing')}</dt><dd>{number.format(counts.passing)}</dd></div>
      <div><dt>{t('health.counts.failing')}</dt><dd>{number.format(counts.failing)}</dd></div>
      <div><dt>{t('health.counts.locationFault')}</dt><dd>{number.format(counts.locationFault)}</dd></div>
      <div><dt>{t('health.counts.indeterminate')}</dt><dd>{number.format(counts.indeterminate)}</dd></div>
      <div><dt>{t('health.counts.missing')}</dt><dd>{number.format(counts.missing)}</dd></div>
    </dl>
  )
}

function IncidentsTable({
  incidents,
  organizationId,
  projectId,
  monitorId,
  selectedIncidentId,
}: {
  incidents: IncidentResponse[]
  organizationId: string
  projectId: string
  monitorId: string
  selectedIncidentId?: string
}) {
  const { t, formatDateTime } = useTranslation()
  return (
    <div className="table-scroll">
      <table className="incidents-table">
        <thead>
          <tr>
            <th scope="col">{t('incident.state')}</th>
            <th scope="col">{t('incident.opened')}</th>
            <th scope="col">{t('incident.updated')}</th>
            <th scope="col">{t('incident.version')}</th>
            <th scope="col"><span className="visually-hidden">{t('incident.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {incidents.map((incident) => (
            <tr key={incident.id} data-selected={incident.id === selectedIncidentId}>
              <td><IncidentStateBadge state={incident.state} /></td>
              <td>{formatDateTime(incident.createdAt)}</td>
              <td>{formatDateTime(incident.updatedAt)}</td>
              <td>{incident.version}</td>
              <td>
                <Link
                  className="incident-detail-link"
                  to={incidentRoute(organizationId, projectId, monitorId, incident.id)}
                  aria-current={incident.id === selectedIncidentId ? 'page' : undefined}
                  aria-label={t('incident.view')}
                  title={t('incident.view')}
                >
                  <span className="incident-detail-text" aria-hidden="true">
                    {t('incident.view')}
                  </span>
                  <span className="incident-detail-icon" aria-hidden="true">&rarr;</span>
                </Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function TimelineEntry({
  entry,
  organizationId,
  projectId,
  monitorId,
}: {
  entry: IncidentTimelineResponse
  organizationId: string
  projectId: string
  monitorId: string
}) {
  const { t, locale, formatDateTime } = useTranslation()
  const number = new Intl.NumberFormat(locale)
  const hasHealthChange = entry.oldHealthState !== null && entry.newHealthState !== null

  return (
    <li className="incident-timeline-entry">
      <div className="incident-timeline-heading">
        <strong>{timelineKindLabel(entry.kind, t)}</strong>
        <time dateTime={entry.occurredAt}>{formatDateTime(entry.occurredAt)}</time>
      </div>
      <dl className="detail-grid incident-timeline-details">
        <div>
          <dt>{t('incident.timeline.version')}</dt>
          <DetailValue>{number.format(entry.incidentVersion)}</DetailValue>
        </div>
        {hasHealthChange && (
          <div>
            <dt>{t('incident.timeline.healthChange')}</dt>
            <DetailValue>
              {t('incident.timeline.healthChangeValue', {
                old: healthStateLabel(entry.oldHealthState!, t),
                next: healthStateLabel(entry.newHealthState!, t),
              })}
            </DetailValue>
          </div>
        )}
        {entry.policyVersion !== null && (
          <div>
            <dt>{t('incident.timeline.policy')}</dt>
            <DetailValue><code>{entry.policyVersion}</code></DetailValue>
          </div>
        )}
        {entry.healthTransitionId !== null && (
          <div>
            <dt>{t('incident.timeline.healthTransition')}</dt>
            <DetailValue><code>{entry.healthTransitionId}</code></DetailValue>
          </div>
        )}
        {entry.actorUserId !== null && (
          <div>
            <dt>{t('incident.timeline.actor')}</dt>
            <DetailValue><code>{entry.actorUserId}</code></DetailValue>
          </div>
        )}
        {entry.causalRunId !== null && (
          <div>
            <dt>{t('incident.timeline.causalRun')}</dt>
            <DetailValue>
              <Link
                className="run-detail-link"
                to={runRoute(organizationId, projectId, monitorId, entry.causalRunId)}
              >
                <code>{entry.causalRunId}</code>
              </Link>
            </DetailValue>
          </div>
        )}
        {entry.causalRunScheduledFor !== null && (
          <div>
            <dt>{t('incident.timeline.runScheduledFor')}</dt>
            <DetailValue>{formatDateTime(entry.causalRunScheduledFor)}</DetailValue>
          </div>
        )}
        <div>
          <dt>{t('incident.timeline.identifier')}</dt>
          <DetailValue><code>{entry.id}</code></DetailValue>
        </div>
      </dl>
      {entry.counts !== null && (
        <>
          <h5>{t('incident.timeline.counts')}</h5>
          <EvidenceCounts counts={entry.counts} />
        </>
      )}
    </li>
  )
}

function IncidentDetails({
  organizationId,
  projectId,
  monitorId,
  incidentId,
}: {
  organizationId: string
  projectId: string
  monitorId: string
  incidentId: string
}) {
  const { t, locale, formatDateTime } = useTranslation()
  const number = new Intl.NumberFormat(locale)
  const query = useQuery({
    queryKey: ['incidents', organizationId, projectId, monitorId, incidentId],
    queryFn: () => getIncident(organizationId, projectId, monitorId, incidentId),
  })

  return (
    <section className="incident-detail-section" aria-labelledby="incident-detail-heading">
      <p>
        <Link to={incidentRoute(organizationId, projectId, monitorId)}>
          {t('incident.detail.backToList')}
        </Link>
      </p>
      <h3 id="incident-detail-heading">{t('incident.detail.heading')}</h3>
      {query.isPending && <p>{t('incident.detail.loading')}</p>}
      {query.isError && (
        <p className="error" role="alert">
          {query.error instanceof ApiError && query.error.status === 404
            ? t('incident.detail.notFound')
            : t('incident.detail.loadFailed')}
        </p>
      )}
      {query.data && (
        <>
          <dl className="detail-grid">
            <div>
              <dt>{t('incident.detail.identifier')}</dt>
              <DetailValue><code>{query.data.id}</code></DetailValue>
            </div>
            <div>
              <dt>{t('incident.state')}</dt>
              <DetailValue><IncidentStateBadge state={query.data.state} /></DetailValue>
            </div>
            <div>
              <dt>{t('incident.version')}</dt>
              <DetailValue>{number.format(query.data.version)}</DetailValue>
            </div>
            <div>
              <dt>{t('incident.detail.created')}</dt>
              <DetailValue>{formatDateTime(query.data.createdAt)}</DetailValue>
            </div>
            <div>
              <dt>{t('incident.detail.updated')}</dt>
              <DetailValue>{formatDateTime(query.data.updatedAt)}</DetailValue>
            </div>
            <div>
              <dt>{t('incident.detail.openedTransition')}</dt>
              <DetailValue><code>{query.data.openedTransitionId}</code></DetailValue>
            </div>
            {query.data.acknowledgedAt !== null && (
              <div>
                <dt>{t('incident.detail.acknowledgedAt')}</dt>
                <DetailValue>{formatDateTime(query.data.acknowledgedAt)}</DetailValue>
              </div>
            )}
            {query.data.acknowledgedBy !== null && (
              <div>
                <dt>{t('incident.detail.acknowledgedBy')}</dt>
                <DetailValue><code>{query.data.acknowledgedBy}</code></DetailValue>
              </div>
            )}
            {query.data.resolvedAt !== null && (
              <div>
                <dt>{t('incident.detail.resolvedAt')}</dt>
                <DetailValue>{formatDateTime(query.data.resolvedAt)}</DetailValue>
              </div>
            )}
            {query.data.resolvedTransitionId !== null && (
              <div>
                <dt>{t('incident.detail.resolvedTransition')}</dt>
                <DetailValue><code>{query.data.resolvedTransitionId}</code></DetailValue>
              </div>
            )}
          </dl>

          <h4>{t('incident.timeline.heading')}</h4>
          {query.data.timeline.length === 0 ? (
            <p className="muted">{t('incident.timeline.empty')}</p>
          ) : (
            <ol className="incident-timeline">
              {query.data.timeline.map((entry) => (
                <TimelineEntry
                  key={entry.id}
                  entry={entry}
                  organizationId={organizationId}
                  projectId={projectId}
                  monitorId={monitorId}
                />
              ))}
            </ol>
          )}
        </>
      )}
    </section>
  )
}

export default function MonitorIncidentsSection({
  organizationId,
  projectId,
  monitorId,
  incidentId,
}: {
  organizationId: string
  projectId: string
  monitorId: string
  incidentId?: string
}) {
  const { t } = useTranslation()
  const query = useInfiniteQuery({
    queryKey: ['incidents', organizationId, projectId, monitorId],
    queryFn: ({ pageParam }) => listIncidents(organizationId, projectId, monitorId, {
      pageSize: incidentPageSize,
      cursor: pageParam === '' ? undefined : pageParam,
    }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  })
  const incidents = query.data?.pages.flatMap((page) => page.items) ?? []

  return (
    <section className="incidents-section" aria-labelledby="incidents-heading">
      <h2 id="incidents-heading">{t('incident.heading')}</h2>
      {query.isPending && <p>{t('incident.loading')}</p>}
      {query.isError && <p className="error" role="alert">{t('incident.loadFailed')}</p>}
      {query.data && incidents.length === 0 && (
        <p className="muted">{t('incident.empty')}</p>
      )}
      {incidents.length > 0 && (
        <IncidentsTable
          incidents={incidents}
          organizationId={organizationId}
          projectId={projectId}
          monitorId={monitorId}
          selectedIncidentId={incidentId}
        />
      )}
      {query.hasNextPage && (
        <button
          type="button"
          className="button-secondary load-more"
          onClick={() => query.fetchNextPage()}
          disabled={query.isFetchingNextPage}
        >
          {query.isFetchingNextPage ? t('incident.loadingMore') : t('incident.loadMore')}
        </button>
      )}
      {incidentId && (
        <IncidentDetails
          organizationId={organizationId}
          projectId={projectId}
          monitorId={monitorId}
          incidentId={incidentId}
        />
      )}
    </section>
  )
}
