import { Link } from 'react-router'

import { useTranslation } from '../i18n/context'
import CreateOrganizationForm from './CreateOrganizationForm.tsx'
import { useOrganizations } from './useOrganizations'

/**
 * The signed-in landing page. Setup provisions the installation Organization
 * so this normally lists at least one and the create form is an
 * option rather than a required step.
 */
export default function OrganizationsPage() {
  const organizations = useOrganizations()
  const { t } = useTranslation()

  return (
    <section className="organizations-page">
      <header className="page-heading organizations-heading">
        <div>
          <p className="eyebrow">{t('app.title')}</p>
          <h1>{t('organizations.heading')}</h1>
        </div>
      </header>
      <div className="organizations-layout">
        <section className="organization-list-section" aria-labelledby="organizations-list-heading">
          <div className="section-heading">
            <div>
              <p className="eyebrow">{t('organizations.heading')}</p>
              <h2 id="organizations-list-heading">{t('organizations.available')}</h2>
            </div>
            <span className="section-count" aria-label={t('organizations.available')}>
              {organizations.data ? organizations.data.length : '-'}
            </span>
          </div>
          {organizations.isPending && <p role="status">{t('organizations.loading')}</p>}
          {organizations.isError && (
            <p className="error" role="alert">
              {t('organization.loadFailed')}
            </p>
          )}
          {organizations.data && organizations.data.length === 0 && (
            <p className="muted">{t('organizations.empty')}</p>
          )}
          {organizations.data && organizations.data.length > 0 && (
            <ul className="organization-list">
              {organizations.data.map((organization) => (
                <li key={organization.id} className="organization-card">
                  <Link
                    className="organization-card-link"
                    to={`/organizations/${organization.id}`}
                    aria-label={organization.displayName}
                  >
                    <span className="organization-card-name">{organization.displayName}</span>
                    <span className="organization-card-arrow" aria-hidden="true">&rarr;</span>
                  </Link>
                  <div className="organization-card-meta">
                    <span className="slug">{organization.slug}</span>
                    <span>{organization.defaultProject.name}</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
        <aside className="organization-create-panel">
          <CreateOrganizationForm />
        </aside>
      </div>
    </section>
  )
}
