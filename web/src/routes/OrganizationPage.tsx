import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router'

import { ApiError, getOrganization } from '../api/organizations'
import { useTranslation } from '../i18n/context'
import RenameOrganizationForm from './RenameOrganizationForm.tsx'

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
    <section>
      <h1>{organization.displayName}</h1>
      <dl>
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
      <h2>{t('organization.defaultProject')}</h2>
      <dl>
        <dt>{t('organization.name')}</dt>
        <dd>{organization.defaultProject.name}</dd>
        <dt>{t('organization.identifier')}</dt>
        <dd>
          <code>{organization.defaultProject.id}</code>
        </dd>
      </dl>
    </section>
  )
}
