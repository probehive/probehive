import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router'

import { ApiError, getOrganization } from '../api/organizations'
import { useTranslation } from '../i18n/context'
import MonitorsPanel from './MonitorsPanel.tsx'
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
      <header className="page-heading organization-heading">
        <div>
          <p className="eyebrow">{t('organization.eyebrow')}</p>
          <h1>{organization.displayName}</h1>
        </div>
      </header>
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
        <h2 id="default-project-heading">{t('organization.defaultProject')}</h2>
        <dl className="detail-grid">
        <dt>{t('organization.name')}</dt>
        <dd>{organization.defaultProject.name}</dd>
        <dt>{t('organization.identifier')}</dt>
        <dd>
          <code>{organization.defaultProject.id}</code>
        </dd>
      </dl>
      </section>
      <MonitorsPanel
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
