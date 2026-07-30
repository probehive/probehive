import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { Link } from 'react-router'

import {
  getMonitorHealth,
  type HealthEvidence,
  type HealthState,
  type HealthTransitionDirection,
} from '../api/health'
import { ApiError } from '../api/http'
import { useTranslation } from '../i18n/context'

function stateLabel(
  state: HealthState,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  return t(`health.state.${state}`)
}

function directionLabel(
  direction: HealthTransitionDirection,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  return t(`health.direction.${direction}`)
}

function evidenceLabel(
  evidence: HealthEvidence,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  return t(`health.evidence.${evidence}`)
}

function runRoute(
  organizationId: string,
  projectId: string,
  monitorId: string,
  runId: string,
): string {
  return `/organizations/${organizationId}/projects/${projectId}/monitors/${monitorId}/runs/${runId}`
}

function DetailValue({ children }: { children: ReactNode }) {
  return <dd>{children ?? '--'}</dd>
}

export default function MonitorHealthSection({
  organizationId,
  projectId,
  monitorId,
}: {
  organizationId: string
  projectId: string
  monitorId: string
}) {
  const { t, locale, formatDateTime } = useTranslation()
  const number = new Intl.NumberFormat(locale)
  const query = useQuery({
    queryKey: ['monitorHealth', organizationId, projectId, monitorId],
    queryFn: () => getMonitorHealth(organizationId, projectId, monitorId),
  })
  const notAvailable = query.error instanceof ApiError && query.error.status === 404

  return (
    <section className="health-section" aria-labelledby="health-heading">
      <h2 id="health-heading">{t('health.heading')}</h2>
      {query.isPending && <p>{t('health.loading')}</p>}
      {query.isError && (
        <p className={notAvailable ? 'muted' : 'error'} role={notAvailable ? undefined : 'alert'}>
          {notAvailable ? t('health.notAvailable') : t('health.loadFailed')}
        </p>
      )}
      {query.data && (
        <>
          <dl className="health-summary detail-grid">
            <div>
              <dt>{t('health.currentState')}</dt>
              <DetailValue>
                <span className="health-state" data-state={query.data.state}>
                  {stateLabel(query.data.state, t)}
                </span>
              </DetailValue>
            </div>
            <div>
              <dt>{t('health.stableState')}</dt>
              <DetailValue>{stateLabel(query.data.stableState, t)}</DetailValue>
            </div>
            <div>
              <dt>{t('health.policyVersion')}</dt>
              <DetailValue><code>{query.data.policyVersion}</code></DetailValue>
            </div>
            <div>
              <dt>{t('health.evaluationVersion')}</dt>
              <DetailValue>{number.format(query.data.version)}</DetailValue>
            </div>
            <div>
              <dt>{t('health.sourceRevision')}</dt>
              <DetailValue>
                {query.data.sourceRevisionNumber === null
                  ? '--'
                  : `v${query.data.sourceRevisionNumber}`}
              </DetailValue>
            </div>
            <div>
              <dt>{t('health.lastCohort')}</dt>
              <DetailValue>
                {query.data.lastScheduledFor === null
                  ? '--'
                  : formatDateTime(query.data.lastScheduledFor)}
              </DetailValue>
            </div>
            <div>
              <dt>{t('health.lastDeterminate')}</dt>
              <DetailValue>
                {query.data.lastDeterminateFinishedAt === null
                  ? '--'
                  : formatDateTime(query.data.lastDeterminateFinishedAt)}
              </DetailValue>
            </div>
            <div>
              <dt>{t('health.transitionedAt')}</dt>
              <DetailValue>{formatDateTime(query.data.transitionedAt)}</DetailValue>
            </div>
            <div>
              <dt>{t('health.updatedAt')}</dt>
              <DetailValue>{formatDateTime(query.data.updatedAt)}</DetailValue>
            </div>
          </dl>

          <h3>{t('health.counts.heading')}</h3>
          <dl className="health-counts">
            <div><dt>{t('health.counts.configured')}</dt><dd>{number.format(query.data.counts.configured)}</dd></div>
            <div><dt>{t('health.counts.eligible')}</dt><dd>{number.format(query.data.counts.eligible)}</dd></div>
            <div><dt>{t('health.counts.responding')}</dt><dd>{number.format(query.data.counts.responding)}</dd></div>
            <div><dt>{t('health.counts.passing')}</dt><dd>{number.format(query.data.counts.passing)}</dd></div>
            <div><dt>{t('health.counts.failing')}</dt><dd>{number.format(query.data.counts.failing)}</dd></div>
            <div><dt>{t('health.counts.locationFault')}</dt><dd>{number.format(query.data.counts.locationFault)}</dd></div>
            <div><dt>{t('health.counts.indeterminate')}</dt><dd>{number.format(query.data.counts.indeterminate)}</dd></div>
            <div><dt>{t('health.counts.missing')}</dt><dd>{number.format(query.data.counts.missing)}</dd></div>
          </dl>

          <div className="health-evidence-section">
            <h3>{t('health.causalRun.heading')}</h3>
            {query.data.lastRunId === null ? (
              <p className="muted">{t('health.causalRun.empty')}</p>
            ) : (
              <dl className="detail-grid">
                <div>
                  <dt>{t('health.causalRun.identifier')}</dt>
                  <DetailValue>
                    <Link
                      className="run-detail-link"
                      to={runRoute(organizationId, projectId, monitorId, query.data.lastRunId)}
                    >
                      <code>{query.data.lastRunId}</code>
                    </Link>
                  </DetailValue>
                </div>
                <div>
                  <dt>{t('health.causalRun.scheduledFor')}</dt>
                  <DetailValue>
                    {query.data.lastRunScheduledFor === null
                      ? '--'
                      : formatDateTime(query.data.lastRunScheduledFor)}
                  </DetailValue>
                </div>
              </dl>
            )}
          </div>

          <div className="health-evidence-section">
            <h3>{t('health.candidate.heading')}</h3>
            {query.data.candidate === null ? (
              <p className="muted">{t('health.candidate.empty')}</p>
            ) : (
              <dl className="detail-grid">
                <div>
                  <dt>{t('health.candidate.direction')}</dt>
                  <DetailValue>{directionLabel(query.data.candidate.direction, t)}</DetailValue>
                </div>
                <div>
                  <dt>{t('health.candidate.expected')}</dt>
                  <DetailValue>{evidenceLabel(query.data.candidate.expectedEvidence, t)}</DetailValue>
                </div>
                <div>
                  <dt>{t('health.candidate.revision')}</dt>
                  <DetailValue>v{query.data.candidate.sourceRevisionNumber}</DetailValue>
                </div>
                <div>
                  <dt>{t('health.candidate.triggeringRun')}</dt>
                  <DetailValue>
                    <Link
                      className="run-detail-link"
                      to={runRoute(
                        organizationId,
                        projectId,
                        monitorId,
                        query.data.candidate.triggeringRunId,
                      )}
                    >
                      <code>{query.data.candidate.triggeringRunId}</code>
                    </Link>
                  </DetailValue>
                </div>
                <div>
                  <dt>{t('health.candidate.triggeringScheduledFor')}</dt>
                  <DetailValue>{formatDateTime(query.data.candidate.triggeringScheduledFor)}</DetailValue>
                </div>
                <div>
                  <dt>{t('health.candidate.requestedAt')}</dt>
                  <DetailValue>{formatDateTime(query.data.candidate.requestedAt)}</DetailValue>
                </div>
                <div>
                  <dt>{t('health.candidate.identifier')}</dt>
                  <DetailValue><code>{query.data.candidate.id}</code></DetailValue>
                </div>
              </dl>
            )}
          </div>
        </>
      )}
    </section>
  )
}
