import { useQuery } from '@tanstack/react-query'
import { Link, NavLink, Outlet, useParams } from 'react-router'

import { ApiError, getOrganization, type OrganizationResponse } from '../api/organizations'
import { useTranslation } from '../i18n/context'
import type { MessageKey } from '../i18n/en'

/** Shared shape every Organization child route reads through useOutletContext. */
export interface OrganizationContext {
  organization: OrganizationResponse
}

const tabs: Array<{ suffix: '' | 'monitors' | 'incidents' | 'integrations' | 'status' | 'settings'; key: MessageKey; end: boolean }> = [
  { suffix: '', key: 'organization.tab.overview', end: true },
  { suffix: 'monitors', key: 'organization.tab.monitors', end: false },
  { suffix: 'incidents', key: 'organization.tab.incidents', end: false },
  { suffix: 'integrations', key: 'organization.tab.integrations', end: false },
  { suffix: 'status', key: 'organization.tab.status', end: false },
  { suffix: 'settings', key: 'organization.tab.settings', end: false },
]

/**
 * One shared Organization workspace frame: breadcrumb, Organization identity,
 * and tab navigation with a stable URL per section. Child routes render only
 * their own section content.
 */
export default function OrganizationLayout() {
  const { organizationId } = useParams<'organizationId'>()
  const { t } = useTranslation()
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
  const base = '/organizations/' + organization.id

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
      <nav className="section-nav organization-tabs" aria-label={t('organization.navigation')}>
        {tabs.map((tab) => (
          <NavLink key={tab.suffix} to={tab.suffix === '' ? base : base + '/' + tab.suffix} end={tab.end}>
            {t(tab.key)}
          </NavLink>
        ))}
      </nav>
      <Outlet context={{ organization } satisfies OrganizationContext} />
    </section>
  )
}
