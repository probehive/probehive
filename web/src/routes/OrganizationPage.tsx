import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router'

import { ApiError, getOrganization } from '../api/organizations'
import { useTranslation } from '../i18n/context'
import MonitorInventoryPanel from './MonitorInventoryPanel.tsx'
import OrganizationOverview from './OrganizationOverview.tsx'
import RenameOrganizationForm from './RenameOrganizationForm.tsx'
import StatusPageDraftSection from './StatusPageDraftSection.tsx'

export default function OrganizationPage() {
  const { organizationId } = useParams<'organizationId'>()
  const { t, formatDateTime } = useTranslation()
  const query = useQuery({
    queryKey: ['organizations', organizationId],
    queryFn: () => getOrganization(organizationId ?? ''),
    enabled: organizationId !== undefined,
  })

  if (query.isPending) {
    return <p>{t('organization.loading')}</p>
  }

  if (query.isError) {
    const notFound = query.error instanceof ApiError && query.error.status === 404
    return (
      <p className="error" role="alert">
        {notFound ? t('organization.notFound') : t('organization.loadFailed')}
      </p>
    )
  }

  const organization = query.data
  return (
    <section className="organization-page">
      <p className="breadcrumb">
        <Link to="/">{t('organizations.heading')}</Link>
      </p>
      <header className="page-heading organization-heading">
        <div>
          <p className="eyebrow">{t('organization.eyebrow')}</p>
          <h1>{organization.displayName}</h1>
        </div>
      </header>
      <nav className="section-nav" aria-label={t('organization.navigation')}>
        <a href="#organization-overview">{t('overview.heading')}</a>
        <a href="#monitors">{t('monitor.heading')}</a>
        <Link to={'/organizations/' + organization.id + '/incidents'}>{t('incident.inbox.navigation')}</Link>
        <a href="#status-page">{t('statusPage.heading')}</a>
        <Link to={'/organizations/' + organization.id + '/integrations'}>{t('integration.heading')}</Link>
        <a href="#organization-settings">{t('overview.action.settings')}</a>
      </nav>
      <OrganizationOverview organizationId={organization.id} />
      <section
        className="organization-settings"
        id="organization-settings"
        aria-labelledby="organization-settings-heading"
      >
        <h2 id="organization-settings-heading">{t('organization.settings')}</h2>
        <dl className="organization-summary detail-grid">
          <dt>{t('organization.slug')}</dt>
          <dd>{organization.slug}</dd>
          <dt>{t('organization.identifier')}</dt>
          <dd>
            <code>{organization.id}</code>
          </dd>
          <dt>{t('organization.created')}</dt>
          <dd>{formatDateTime(organization.createdAt)}</dd>
        </dl>
        <RenameOrganizationForm organization={organization} />
        <section className="project-summary" aria-labelledby="default-project-heading">
          <h3 id="default-project-heading">{t('organization.defaultProject')}</h3>
          <dl className="detail-grid">
            <dt>{t('organization.name')}</dt>
            <dd>{organization.defaultProject.name}</dd>
            <dt>{t('organization.identifier')}</dt>
            <dd>
              <code>{organization.defaultProject.id}</code>
            </dd>
          </dl>
        </section>
      </section>
      <MonitorInventoryPanel
        organizationId={organization.id}
        projectId={organization.defaultProject.id}
      />
      <StatusPageDraftSection
        organizationId={organization.id}
        projectId={organization.defaultProject.id}
      />
    </section>
  )
}
