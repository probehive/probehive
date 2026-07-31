import { useInfiniteQuery } from '@tanstack/react-query'
import { Link } from 'react-router'

import { listAlerts, type AlertKind, type AlertResponse } from '../api/alerts'
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
  const { t, formatDateTime } = useTranslation()
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
            <tr key={alert.id}>
              <td><AlertKindBadge kind={alert.kind} /></td>
              <td>{formatDateTime(alert.occurredAt)}</td>
              <td>{formatDateTime(alert.createdAt)}</td>
              <td>{alert.incidentVersion}</td>
              <td>
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
              </td>
            </tr>
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
