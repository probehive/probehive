import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router'

import { ApiError } from '../api/http'
import { getMonitor, type MonitorState } from '../api/monitors'
import {
  getObservation,
  getRun,
  listRuns,
  type ObservationResponse,
  type RunKind,
  type RunResponse,
} from '../api/runs'
import { useTranslation } from '../i18n/context'
import ChangeMonitorIntervalForm from './ChangeMonitorIntervalForm'
import ChangeMonitorTargetForm from './ChangeMonitorTargetForm'
import ManualRunControl from './ManualRunControl'
import MonitorAlertsSection from './MonitorAlertsSection'
import MonitorHealthSection from './MonitorHealthSection'
import MonitorIncidentsSection from './MonitorIncidentsSection'
import MonitorLifecycleControls from './MonitorLifecycleControls'
import RenameMonitorForm from './RenameMonitorForm'
import { monitorQueryKey } from './monitorQueries'

const runLookbackDays = 30
const runPageSize = 25

function runsNotBefore(): string {
  const date = new Date()
  date.setUTCDate(date.getUTCDate() - runLookbackDays)
  date.setUTCHours(0, 0, 0, 0)
  return date.toISOString()
}

function outcomeLabel(
  run: Pick<RunResponse, 'outcome' | 'leaseExpiresAt'>,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  switch (run.outcome) {
    case 'passed':
      return t('run.outcome.passed')
    case 'failed':
      return t('run.outcome.failed')
    case 'errored':
      return t('run.outcome.errored')
    case 'timedout':
      return t('run.outcome.timedout')
    case 'cancelled':
      return t('run.outcome.cancelled')
    case 'skipped':
      return t('run.outcome.skipped')
    default:
      return leaseExpired(run) ? t('run.outcome.leaseExpired') : t('run.outcome.inProgress')
  }
}

function leaseExpired(run: Pick<RunResponse, 'outcome' | 'leaseExpiresAt'>): boolean {
  return run.outcome === null && run.leaseExpiresAt !== null &&
    Date.parse(run.leaseExpiresAt) <= Date.now()
}

function kindLabel(kind: RunKind, t: ReturnType<typeof useTranslation>['t']): string {
  switch (kind) {
    case 'scheduled':
      return t('run.kind.scheduled')
    case 'confirmation':
      return t('run.kind.confirmation')
    case 'manual':
      return t('run.kind.manual')
  }
}

function formatDuration(microseconds: number, locale: string): string {
  const milliseconds = microseconds / 1000
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 3 }).format(milliseconds)} ms`
}

function runRoute(
  organizationId: string,
  projectId: string,
  monitorId: string,
  runId?: string,
): string {
  const monitorRoute = `/organizations/${organizationId}/projects/${projectId}/monitors/${monitorId}`
  return runId === undefined ? monitorRoute : `${monitorRoute}/runs/${runId}`
}

function Outcome({ run }: { run: Pick<RunResponse, 'outcome' | 'leaseExpiresAt'> }) {
  const { t } = useTranslation()
  const displayState = run.outcome ?? (leaseExpired(run) ? 'leaseExpired' : 'inProgress')
  return (
    <span className="run-outcome" data-outcome={displayState}>
      {outcomeLabel(run, t)}
    </span>
  )
}

function RunsTable({
  runs,
  organizationId,
  projectId,
  monitorId,
  selectedRunId,
}: {
  runs: RunResponse[]
  organizationId: string
  projectId: string
  monitorId: string
  selectedRunId?: string
}) {
  const { t, formatDateTime } = useTranslation()
  return (
    <div className="table-scroll">
      <table className="runs-table">
        <thead>
          <tr>
            <th scope="col">{t('run.outcome')}</th>
            <th scope="col">{t('run.scheduledFor')}</th>
            <th scope="col">{t('run.kind')}</th>
            <th scope="col">{t('run.location')}</th>
            <th scope="col">{t('run.revision')}</th>
            <th scope="col"><span className="visually-hidden">{t('run.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {runs.map((run) => (
            <tr key={run.id} data-selected={run.id === selectedRunId}>
              <td><Outcome run={run} /></td>
              <td>{formatDateTime(run.scheduledFor)}</td>
              <td>{kindLabel(run.kind, t)}</td>
              <td><code>{run.location}</code></td>
              <td>v{run.revisionNumber}</td>
              <td>
                <Link
                  className="run-detail-link"
                  to={runRoute(organizationId, projectId, monitorId, run.id)}
                  aria-current={run.id === selectedRunId ? 'page' : undefined}
                  aria-label={t('run.view')}
                  title={t('run.view')}
                >
                  <span className="run-detail-text" aria-hidden="true">{t('run.view')}</span>
                  <span className="run-detail-icon" aria-hidden="true">&rarr;</span>
                </Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function DetailValue({ children }: { children: ReactNode }) {
  return <dd>{children ?? '--'}</dd>
}

function ObservationDetails({ observation }: { observation: ObservationResponse }) {
  const { t, locale, formatDateTime } = useTranslation()
  const number = new Intl.NumberFormat(locale)
  return (
    <section className="observation-section" aria-labelledby="observation-heading">
      <h3 id="observation-heading">{t('run.observation.heading')}</h3>
      <dl className="detail-grid">
        <div>
          <dt>{t('run.observation.duration')}</dt>
          <DetailValue>{formatDuration(observation.durationMicroseconds, locale)}</DetailValue>
        </div>
        <div>
          <dt>{t('run.observation.connect')}</dt>
          <DetailValue>{formatDuration(observation.phases.connectMicroseconds, locale)}</DetailValue>
        </div>
        <div>
          <dt>{t('run.observation.tls')}</dt>
          <DetailValue>{formatDuration(observation.phases.tlsMicroseconds, locale)}</DetailValue>
        </div>
        <div>
          <dt>{t('run.observation.firstByte')}</dt>
          <DetailValue>{formatDuration(observation.phases.firstByteMicroseconds, locale)}</DetailValue>
        </div>
        {observation.failureCode !== '' && (
          <div>
            <dt>{t('run.observation.failureCode')}</dt>
            <DetailValue><code>{observation.failureCode}</code></DetailValue>
          </div>
        )}
        {observation.failureClass !== '' && (
          <div>
            <dt>{t('run.observation.failureClass')}</dt>
            <DetailValue><code>{observation.failureClass}</code></DetailValue>
          </div>
        )}
      </dl>

      {observation.http === null ? (
        <p className="muted">{t('run.observation.noProtocol')}</p>
      ) : (
        <>
          <h4>{t('run.observation.http')}</h4>
          <dl className="detail-grid">
            <div>
              <dt>{t('run.observation.statusCode')}</dt>
              <DetailValue>{number.format(observation.http.statusCode)}</DetailValue>
            </div>
            <div>
              <dt>{t('run.observation.protocol')}</dt>
              <DetailValue>{observation.http.protocol}</DetailValue>
            </div>
            <div>
              <dt>{t('run.observation.redirects')}</dt>
              <DetailValue>{number.format(observation.http.redirectCount)}</DetailValue>
            </div>
            <div>
              <dt>{t('run.observation.bodyBytes')}</dt>
              <DetailValue>
                {t('run.observation.bytes', { count: number.format(observation.http.bodyBytes) })}
                {observation.http.bodyTruncated ? ` ${t('run.observation.truncated')}` : ''}
              </DetailValue>
            </div>
          </dl>
          {observation.http.tls === null ? (
            <p className="muted">{t('run.observation.noTLS')}</p>
          ) : (
            <>
              <h4>{t('run.observation.tlsDetails')}</h4>
              <dl className="detail-grid">
                <div>
                  <dt>{t('run.observation.tlsVersion')}</dt>
                  <DetailValue>{observation.http.tls.version}</DetailValue>
                </div>
                <div>
                  <dt>{t('run.observation.cipherSuite')}</dt>
                  <DetailValue><code>{observation.http.tls.cipherSuite}</code></DetailValue>
                </div>
                <div>
                  <dt>{t('run.observation.certificateExpires')}</dt>
                  <DetailValue>
                    {observation.http.tls.certificateExpiresAt === null
                      ? '--'
                      : formatDateTime(observation.http.tls.certificateExpiresAt)}
                  </DetailValue>
                </div>
              </dl>
            </>
          )}
        </>
      )}
    </section>
  )
}

function RunDetails({
  organizationId,
  projectId,
  monitorId,
  runId,
}: {
  organizationId: string
  projectId: string
  monitorId: string
  runId: string
}) {
  const { t, formatDateTime } = useTranslation()
  const query = useQuery({
    queryKey: ['runs', organizationId, projectId, monitorId, runId],
    queryFn: () => getRun(organizationId, projectId, monitorId, runId),
  })
  const expectsObservation =
    query.data !== undefined && query.data.outcome !== null && query.data.outcome !== 'skipped'
  const observation = useQuery({
    queryKey: ['observations', organizationId, projectId, monitorId, runId],
    queryFn: () => getObservation(organizationId, projectId, monitorId, runId),
    enabled: expectsObservation,
  })

  return (
    <section className="run-detail-section" aria-labelledby="run-detail-heading">
      <p>
        <Link to={runRoute(organizationId, projectId, monitorId)}>{t('run.backToList')}</Link>
      </p>
      <h2 id="run-detail-heading">{t('run.detail.heading')}</h2>
      {query.isPending && <p>{t('run.detail.loading')}</p>}
      {query.isError && (
        <p className="error" role="alert">
          {query.error instanceof ApiError && query.error.status === 404
            ? t('run.detail.notFound')
            : t('run.detail.loadFailed')}
        </p>
      )}
      {query.data && (
        <>
          <dl className="detail-grid">
            <div>
              <dt>{t('run.identifier')}</dt>
              <DetailValue><code>{query.data.id}</code></DetailValue>
            </div>
            <div>
              <dt>{t('run.outcome')}</dt>
              <DetailValue><Outcome run={query.data} /></DetailValue>
            </div>
            <div>
              <dt>{t('run.kind')}</dt>
              <DetailValue>{kindLabel(query.data.kind, t)}</DetailValue>
            </div>
            <div>
              <dt>{t('run.location')}</dt>
              <DetailValue><code>{query.data.location}</code></DetailValue>
            </div>
            <div>
              <dt>{t('run.scheduledFor')}</dt>
              <DetailValue>{formatDateTime(query.data.scheduledFor)}</DetailValue>
            </div>
            <div>
              <dt>{t('run.startedAt')}</dt>
              <DetailValue>{query.data.startedAt ? formatDateTime(query.data.startedAt) : '--'}</DetailValue>
            </div>
            <div>
              <dt>{t('run.finishedAt')}</dt>
              <DetailValue>{query.data.finishedAt ? formatDateTime(query.data.finishedAt) : '--'}</DetailValue>
            </div>
            {query.data.leaseExpiresAt && (
              <div>
                <dt>{t('run.leaseExpiresAt')}</dt>
                <DetailValue>{formatDateTime(query.data.leaseExpiresAt)}</DetailValue>
              </div>
            )}
            <div>
              <dt>{t('run.revision')}</dt>
              <DetailValue>v{query.data.revisionNumber}</DetailValue>
            </div>
          </dl>
          {query.data.confirmation && (
            <details className="confirmation-detail">
              <summary>{t('run.confirmation.heading')}</summary>
              <dl className="detail-grid">
                <div><dt>{t('run.confirmation.policy')}</dt><DetailValue>{query.data.confirmation.policyVersion}</DetailValue></div>
                <div><dt>{t('run.confirmation.trigger')}</dt><DetailValue><code>{query.data.confirmation.triggeringRunId}</code></DetailValue></div>
                <div><dt>{t('run.confirmation.triggeredAt')}</dt><DetailValue>{formatDateTime(query.data.confirmation.triggeringScheduledFor)}</DetailValue></div>
                <div><dt>{t('run.confirmation.candidate')}</dt><DetailValue><code>{query.data.confirmation.candidateId}</code></DetailValue></div>
                <div><dt>{t('run.confirmation.event')}</dt><DetailValue><code>{query.data.confirmation.causationEventId}</code></DetailValue></div>
              </dl>
            </details>
          )}
          {query.data.outcome === null && (
            <p className="muted">
              {leaseExpired(query.data)
                ? t('run.observation.leaseExpired')
                : t('run.observation.inProgress')}
            </p>
          )}
          {query.data.outcome === 'skipped' && <p className="muted">{t('run.observation.skipped')}</p>}
          {expectsObservation && observation.isPending && <p>{t('run.observation.loading')}</p>}
          {expectsObservation && observation.isError && (
            <p className="error" role="alert">{t('run.observation.loadFailed')}</p>
          )}
          {observation.data && <ObservationDetails observation={observation.data} />}
        </>
      )}
    </section>
  )
}

export default function MonitorPage() {
  const { organizationId = '', projectId = '', monitorId = '', runId, incidentId } =
    useParams<'organizationId' | 'projectId' | 'monitorId' | 'runId' | 'incidentId'>()
  const { t, formatDateTime } = useTranslation()
  const notBefore = runsNotBefore()
  const monitor = useQuery({
    queryKey: monitorQueryKey(organizationId, projectId, monitorId),
    queryFn: () => getMonitor(organizationId, projectId, monitorId),
  })
  const runs = useInfiniteQuery({
    queryKey: ['runs', organizationId, projectId, monitorId, notBefore],
    queryFn: ({ pageParam }) => listRuns(organizationId, projectId, monitorId, {
      notBefore,
      pageSize: runPageSize,
      cursor: pageParam === '' ? undefined : pageParam,
    }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  })
  const allRuns = runs.data?.pages.flatMap((page) => page.items) ?? []
  const stateLabels: Record<MonitorState, string> = {
    draft: t('monitor.state.draft'),
    active: t('monitor.state.active'),
    paused: t('monitor.state.paused'),
    archived: t('monitor.state.archived'),
  }

  if (monitor.isPending) {
    return <p>{t('monitor.detail.loading')}</p>
  }
  if (monitor.isError) {
    return (
      <p className="error" role="alert">
        {monitor.error instanceof ApiError && monitor.error.status === 404
          ? t('monitor.detail.notFound')
          : t('monitor.detail.loadFailed')}
      </p>
    )
  }

  return (
    <article>
      <p className="breadcrumb">
        <Link to={`/organizations/${organizationId}`}>{t('monitor.detail.back')}</Link>
      </p>
      <div className="monitor-heading">
        <div>
          <p className="eyebrow">{t('monitor.detail.eyebrow')}</p>
          <h1>{monitor.data.name}</h1>
        </div>
        <span className="monitor-state" data-state={monitor.data.state}>
          {stateLabels[monitor.data.state]}
        </span>
      </div>
      <dl className="monitor-summary">
        <div><dt>{t('monitor.interval')}</dt><dd>{t('monitor.intervalValue', { seconds: monitor.data.intervalSeconds })}</dd></div>
        <div><dt>{t('monitor.revision')}</dt><dd>{monitor.data.latestRevisionNumber === 0 ? t('monitor.unconfigured') : `v${monitor.data.latestRevisionNumber}`}</dd></div>
        <div><dt>{t('monitor.updated')}</dt><dd>{formatDateTime(monitor.data.updatedAt)}</dd></div>
        <div><dt>{t('monitor.identifier')}</dt><dd><code>{monitor.data.id}</code></dd></div>
      </dl>

      <MonitorLifecycleControls monitor={monitor.data} />
      <RenameMonitorForm monitor={monitor.data} />
      <ChangeMonitorIntervalForm monitor={monitor.data} />
      <ChangeMonitorTargetForm monitor={monitor.data} />

      <MonitorHealthSection
        organizationId={organizationId}
        projectId={projectId}
        monitorId={monitorId}
      />

      <MonitorIncidentsSection
        organizationId={organizationId}
        projectId={projectId}
        monitorId={monitorId}
        incidentId={incidentId}
      />

      <MonitorAlertsSection
        organizationId={organizationId}
        projectId={projectId}
        monitorId={monitorId}
      />

      <section className="runs-section" aria-labelledby="runs-heading">
        <div className="section-heading">
          <div>
            <h2 id="runs-heading">{t('run.heading')}</h2>
            <p className="muted">{t('run.range', { days: runLookbackDays })}</p>
          </div>
          <ManualRunControl monitor={monitor.data} />
        </div>
        {runs.isPending && <p>{t('run.loading')}</p>}
        {runs.isError && <p className="error" role="alert">{t('run.loadFailed')}</p>}
        {runs.data && allRuns.length === 0 && <p className="muted">{t('run.empty')}</p>}
        {allRuns.length > 0 && (
          <RunsTable
            runs={allRuns}
            organizationId={organizationId}
            projectId={projectId}
            monitorId={monitorId}
            selectedRunId={runId}
          />
        )}
        {runs.hasNextPage && (
          <button
            type="button"
            className="button-secondary load-more"
            onClick={() => runs.fetchNextPage()}
            disabled={runs.isFetchingNextPage}
          >
            {runs.isFetchingNextPage ? t('run.loadingMore') : t('run.loadMore')}
          </button>
        )}
      </section>

      {runId && (
        <RunDetails
          organizationId={organizationId}
          projectId={projectId}
          monitorId={monitorId}
          runId={runId}
        />
      )}
    </article>
  )
}
