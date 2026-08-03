import { Fragment, useState } from 'react'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'

import {
  listAlertDeliveries,
  listAlerts,
  type AlertDeliveryResponse,
  type AlertKind,
  type AlertResponse,
  type DeliveryAttemptOutcome,
} from '../api/alerts'
import { useTranslation } from '../i18n/context'

const alertPageSize = 25

function alertsQueryKey(
  organizationId: string,
  projectId: string,
  monitorId: string,
) {
  return ['alerts', organizationId, projectId, monitorId] as const
}

function incidentRoute(
  organizationId: string,
  projectId: string,
  monitorId: string,
  incidentId: string,
): string {
  return '/organizations/' + organizationId +
    '/projects/' + projectId + '/monitors/' + monitorId + '/incidents/' + incidentId
}

function kindLabel(
  kind: AlertKind,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  return t(`alert.kind.${kind}`)
}

function AlertKindBadge({ kind }: { kind: AlertKind }) {
  const { t } = useTranslation()
  return (
    <span
      className="incident-state alert-kind"
      data-kind={kind}
      data-state={kind === 'incident.opened' ? 'open' : 'resolved'}
    >
      {kindLabel(kind, t)}
    </span>
  )
}

function deliveryOutcomeLabel(
  outcome: DeliveryAttemptOutcome,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  return t(`alert.delivery.outcome.${outcome}`)
}

function DeliveryAudit({ delivery }: { delivery: AlertDeliveryResponse }) {
  const { t, formatDateTime } = useTranslation()
  return (
    <article className="delivery-audit">
      <div className="delivery-audit-heading">
        <strong>{t('alert.delivery.webhook')}</strong>
        <span>{formatDateTime(delivery.routedAt)}</span>
      </div>
      <dl className="delivery-metadata">
        <div>
          <dt>{t('alert.delivery.identifier')}</dt>
          <dd>{delivery.id}</dd>
        </div>
        <div>
          <dt>{t('alert.delivery.integration')}</dt>
          <dd>{delivery.integrationId}</dd>
        </div>
        <div>
          <dt>{t('alert.delivery.integrationVersion')}</dt>
          <dd>{delivery.integrationVersion}</dd>
        </div>
        <div>
          <dt>{t('alert.delivery.secretVersion')}</dt>
          <dd>{delivery.secretVersion}</dd>
        </div>
      </dl>
      {delivery.attempts.length === 0 ? (
        <p className="muted">{t('alert.delivery.pending')}</p>
      ) : (
        <div className="table-scroll">
          <table className="delivery-attempts-table">
            <thead>
              <tr>
                <th scope="col">{t('alert.delivery.attempt')}</th>
                <th scope="col">{t('alert.delivery.outcome')}</th>
                <th scope="col">{t('alert.delivery.started')}</th>
                <th scope="col">{t('alert.delivery.finished')}</th>
                <th scope="col">{t('alert.delivery.httpStatus')}</th>
                <th scope="col">{t('alert.delivery.failureCode')}</th>
              </tr>
            </thead>
            <tbody>
              {delivery.attempts.map((attempt) => (
                <tr key={attempt.sequence}>
                  <td>{attempt.sequence}</td>
                  <td>
                    <span className="delivery-outcome" data-outcome={attempt.outcome}>
                      {deliveryOutcomeLabel(attempt.outcome, t)}
                    </span>
                  </td>
                  <td>{formatDateTime(attempt.startedAt)}</td>
                  <td>
                    {attempt.finishedAt === null
                      ? t('alert.delivery.notRecorded')
                      : formatDateTime(attempt.finishedAt)}
                  </td>
                  <td>{attempt.httpStatus ?? t('alert.delivery.notRecorded')}</td>
                  <td>{attempt.failureCode ?? t('alert.delivery.notRecorded')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </article>
  )
}

function AlertDeliveryEvidence({
  organizationId,
  projectId,
  monitorId,
  alertId,
}: {
  organizationId: string
  projectId: string
  monitorId: string
  alertId: string
}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['alert-deliveries', organizationId, projectId, monitorId, alertId],
    queryFn: () => listAlertDeliveries(
      organizationId, projectId, monitorId, alertId,
    ),
  })

  return (
    <div className="delivery-evidence" id={`alert-deliveries-${alertId}`}>
      <h3>{t('alert.delivery.heading')}</h3>
      {query.isPending && <p>{t('alert.delivery.loading')}</p>}
      {query.isError && (
        <p className="error" role="alert">{t('alert.delivery.loadFailed')}</p>
      )}
      {query.data?.items.length === 0 && (
        <p className="muted">{t('alert.delivery.empty')}</p>
      )}
      {query.data?.items.map((delivery) => (
        <DeliveryAudit key={delivery.id} delivery={delivery} />
      ))}
    </div>
  )
}

function AlertRow({
  alert,
  organizationId,
  projectId,
  monitorId,
}: {
  alert: AlertResponse
  organizationId: string
  projectId: string
  monitorId: string
}) {
  const { t, formatDateTime } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const evidenceId = `alert-deliveries-${alert.id}`
  return (
    <Fragment>
      <tr>
        <td><AlertKindBadge kind={alert.kind} /></td>
        <td>{formatDateTime(alert.occurredAt)}</td>
        <td>{formatDateTime(alert.createdAt)}</td>
        <td>{alert.incidentVersion}</td>
        <td>
          <div className="alert-actions">
            <button
              type="button"
              className="button-secondary delivery-toggle"
              aria-expanded={expanded}
              aria-controls={evidenceId}
              aria-label={expanded ? t('alert.delivery.hide') : t('alert.delivery.show')}
              title={expanded ? t('alert.delivery.hide') : t('alert.delivery.show')}
              onClick={() => setExpanded((value) => !value)}
            >
              <span aria-hidden="true">{expanded ? '-' : '+'}</span>
            </button>
            <Link
              className="incident-detail-link"
              to={incidentRoute(
                organizationId,
                projectId,
                monitorId,
                alert.incidentId,
              )}
              aria-label={t('alert.view')}
              title={t('alert.view')}
            >
              <span className="incident-detail-text" aria-hidden="true">
                {t('alert.view')}
              </span>
              <span className="incident-detail-icon" aria-hidden="true">&rarr;</span>
            </Link>
          </div>
        </td>
      </tr>
      {expanded && (
        <tr className="delivery-evidence-row">
          <td colSpan={5}>
            <AlertDeliveryEvidence
              organizationId={organizationId}
              projectId={projectId}
              monitorId={monitorId}
              alertId={alert.id}
            />
          </td>
        </tr>
      )}
    </Fragment>
  )
}

function AlertsTable({
  alerts,
  organizationId,
  projectId,
  monitorId,
}: {
  alerts: AlertResponse[]
  organizationId: string
  projectId: string
  monitorId: string
}) {
  const { t } = useTranslation()
  return (
    <div className="table-scroll">
      <table className="incidents-table alert-intents-table">
        <thead>
          <tr>
            <th scope="col">{t('alert.kind')}</th>
            <th scope="col">{t('alert.occurred')}</th>
            <th scope="col">{t('alert.recorded')}</th>
            <th scope="col">{t('alert.incidentVersion')}</th>
            <th scope="col"><span className="visually-hidden">{t('alert.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {alerts.map((alert) => (
            <AlertRow
              key={alert.id}
              alert={alert}
              organizationId={organizationId}
              projectId={projectId}
              monitorId={monitorId}
            />
          ))}
        </tbody>
      </table>
    </div>
  )
}

export default function MonitorAlertsSection({
  organizationId,
  projectId,
  monitorId,
}: {
  organizationId: string
  projectId: string
  monitorId: string
}) {
  const { t } = useTranslation()
  const query = useInfiniteQuery({
    queryKey: alertsQueryKey(organizationId, projectId, monitorId),
    queryFn: ({ pageParam }) => listAlerts(organizationId, projectId, monitorId, {
      pageSize: alertPageSize,
      cursor: pageParam === '' ? undefined : pageParam,
    }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  })
  const alerts = query.data?.pages.flatMap((page) => page.items) ?? []

  return (
    <section className="incidents-section alert-intents-section" aria-labelledby="alerts-heading">
      <div className="section-heading">
        <div>
          <h2 id="alerts-heading">{t('alert.heading')}</h2>
          <p className="muted">{t('alert.scope')}</p>
        </div>
      </div>
      {query.isPending && <p>{t('alert.loading')}</p>}
      {query.isError && <p className="error" role="alert">{t('alert.loadFailed')}</p>}
      {query.data && alerts.length === 0 && (
        <p className="muted">{t('alert.empty')}</p>
      )}
      {alerts.length > 0 && (
        <AlertsTable
          alerts={alerts}
          organizationId={organizationId}
          projectId={projectId}
          monitorId={monitorId}
        />
      )}
      {query.hasNextPage && (
        <button
          type="button"
          className="button-secondary load-more"
          onClick={() => query.fetchNextPage()}
          disabled={query.isFetchingNextPage}
        >
          {query.isFetchingNextPage ? t('alert.loadingMore') : t('alert.loadMore')}
        </button>
      )}
    </section>
  )
}
