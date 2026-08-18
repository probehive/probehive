import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'

import {
  getOrganizationOverview,
  organizationOverviewQueryKey,
  type OrganizationOverviewActiveIncident,
} from '../api/overview'
import { useTranslation } from '../i18n/context'

function OverviewMetric({ label, value }: { label: string; value: number }) {
  return (
    <div className="overview-metric">
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  )
}

function incidentPath(
  organizationId: string,
  incident: OrganizationOverviewActiveIncident,
): string {
  return (
    '/organizations/' + encodeURIComponent(organizationId) +
    '/projects/' + encodeURIComponent(incident.projectId) +
    '/monitors/' + encodeURIComponent(incident.monitorId) +
    '/incidents/' + encodeURIComponent(incident.id)
  )
}

export default function OrganizationOverview({ organizationId }: { organizationId: string }) {
  const { t, formatDateTime } = useTranslation()
  const query = useQuery({
    queryKey: organizationOverviewQueryKey(organizationId),
    queryFn: () => getOrganizationOverview(organizationId),
  })
  const incidentStateLabels = {
    open: t('overview.incidents.state.open'),
    acknowledged: t('overview.incidents.state.acknowledged'),
  } as const

  return (
    <section className="organization-overview" id="organization-overview" aria-labelledby="organization-overview-heading">
      <div className="section-heading">
        <div>
          <p className="eyebrow">{t('organization.eyebrow')}</p>
          <h2 id="organization-overview-heading">{t('overview.heading')}</h2>
        </div>
      </div>
      {query.isPending && <p>{t('overview.loading')}</p>}
      {query.isError && (
        <div className="error overview-error" role="alert">
          <p>{t('overview.loadFailed')}</p>
          <button type="button" className="button-secondary button-compact" onClick={() => void query.refetch()}>
            {t('overview.retry')}
          </button>
        </div>
      )}
      {query.data && (
        <div className="overview-layout">
          <section className="overview-group" aria-labelledby="overview-monitoring-heading">
            <div className="section-heading">
              <h3 id="overview-monitoring-heading">{t('overview.monitoring.heading')}</h3>
              <a href="#monitors" className="overview-link">{t('overview.action.monitors')}</a>
            </div>
            {query.data.monitors === null ? (
              <p className="muted">{t('overview.monitoring.unavailable')}</p>
            ) : (
              <>
                <dl className="overview-metrics">
                  <OverviewMetric label={t('overview.monitors.total')} value={query.data.monitors.total} />
                  <OverviewMetric label={t('overview.monitors.active')} value={query.data.monitors.active} />
                  <OverviewMetric label={t('overview.monitors.draft')} value={query.data.monitors.draft} />
                  <OverviewMetric label={t('overview.monitors.paused')} value={query.data.monitors.paused} />
                  <OverviewMetric label={t('overview.monitors.archived')} value={query.data.monitors.archived} />
                </dl>
                {query.data.monitors.total === 0 && (
                  <p className="muted overview-note">{t('overview.monitors.empty')}</p>
                )}
                {query.data.monitors.total > 0 && query.data.monitors.active === 0 && (
                  <p className="muted overview-note">{t('overview.monitors.noActive')}</p>
                )}
                <h4>{t('overview.health.heading')}</h4>
                {query.data.health === null ? (
                  <p className="muted">{t('overview.health.unavailable')}</p>
                ) : query.data.monitors.active === 0 ? (
                  <p className="muted">{t('overview.health.noActive')}</p>
                ) : (
                  <>
                    <dl className="overview-metrics overview-health-metrics">
                      <OverviewMetric label={t('overview.health.notEvaluated')} value={query.data.health.notEvaluated} />
                      <OverviewMetric label={t('overview.health.unknown')} value={query.data.health.unknown} />
                      <OverviewMetric label={t('overview.health.healthy')} value={query.data.health.healthy} />
                      <OverviewMetric label={t('overview.health.degraded')} value={query.data.health.degraded} />
                      <OverviewMetric label={t('overview.health.down')} value={query.data.health.down} />
                    </dl>
                    {query.data.health.notEvaluated > 0 && (
                      <p className="muted overview-note">
                        {t('overview.health.waiting', { count: query.data.health.notEvaluated })}
                      </p>
                    )}
                  </>
                )}
              </>
            )}
          </section>

          <section className="overview-group" aria-labelledby="overview-incidents-heading">
            <div className="section-heading">
              <h3 id="overview-incidents-heading">{t('overview.incidents.heading')}</h3>
              <Link to={'/organizations/' + organizationId + '/incidents'} className="overview-link">
                {t('overview.action.incidents')}
              </Link>
            </div>
            {query.data.incidents === null ? (
              <p className="muted">{t('overview.incidents.unavailable')}</p>
            ) : (
              <>
                <dl className="overview-metrics overview-incident-metrics">
                  <OverviewMetric label={t('overview.incidents.active')} value={query.data.incidents.active} />
                  <OverviewMetric label={t('overview.incidents.open')} value={query.data.incidents.open} />
                  <OverviewMetric label={t('overview.incidents.acknowledged')} value={query.data.incidents.acknowledged} />
                </dl>
                {query.data.incidents.active === 0 ? (
                  <p className="muted overview-note">{t('overview.incidents.none')}</p>
                ) : (
                  <>
                    <h4>{t('overview.incidents.preview')}</h4>
                    <ol className="overview-incident-list">
                      {query.data.incidents.activePreview.map((incident) => (
                        <li key={incident.id}>
                          <Link
                            to={incidentPath(organizationId, incident)}
                            aria-label={t('overview.incidents.view', { name: incident.monitorName })}
                          >
                            {incident.monitorName}
                          </Link>
                          <div className="overview-incident-meta">
                            <span className="incident-state" data-state={incident.state}>
                              {incidentStateLabels[incident.state]}
                            </span>
                            <time dateTime={incident.updatedAt}>
                              {t('overview.incidents.updated', { time: formatDateTime(incident.updatedAt) })}
                            </time>
                          </div>
                        </li>
                      ))}
                    </ol>
                    {query.data.incidents.activePreviewTruncated && (
                      <p className="overview-note">
                        <Link to={'/organizations/' + organizationId + '/incidents'}>
                          {t('overview.incidents.more')}
                        </Link>
                      </p>
                    )}
                  </>
                )}
              </>
            )}
          </section>

          <section className="overview-group" aria-labelledby="overview-administration-heading">
            <div className="section-heading">
              <h3 id="overview-administration-heading">{t('overview.administration.heading')}</h3>
            </div>
            {query.data.integrations === null && query.data.statusPage === null ? (
              <p className="muted">{t('overview.administration.unavailable')}</p>
            ) : (
              <dl className="overview-admin-list">
                {query.data.integrations !== null && (
                  <div>
                    <dt>
                      {query.data.capabilities.manageIntegrations ? (
                        <Link to={'/organizations/' + organizationId + '/integrations'}>
                          {t('overview.integrations.label')}
                        </Link>
                      ) : (
                        t('overview.integrations.label')
                      )}
                    </dt>
                    <dd>
                      {query.data.integrations.total === 0
                        ? t('overview.integrations.none')
                        : t('overview.integrations.value', {
                            enabled: query.data.integrations.enabled,
                            total: query.data.integrations.total,
                          })}
                    </dd>
                  </div>
                )}
                {query.data.statusPage !== null && (
                  <div>
                    <dt>
                      {query.data.capabilities.manageStatusPage ? (
                        <a href="#status-page">{t('overview.statusPage.label')}</a>
                      ) : (
                        t('overview.statusPage.label')
                      )}
                    </dt>
                    <dd>
                      {!query.data.statusPage.configured
                        ? t('overview.statusPage.notConfigured')
                        : query.data.statusPage.published
                          ? t('overview.statusPage.published')
                          : t('overview.statusPage.private')}
                    </dd>
                  </div>
                )}
              </dl>
            )}
            {query.data.capabilities.manageOrganization && (
              <p className="overview-action">
                <a href="#organization-settings">{t('overview.action.settings')}</a>
              </p>
            )}
          </section>
        </div>
      )}
    </section>
  )
}
