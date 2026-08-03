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
    <section>
      <h1>{t('organizations.heading')}</h1>
      {organizations.isPending && <p role="status">{t('organizations.loading')}</p>}
      {organizations.isError && (
        <p className="error" role="alert">
          {t('organization.loadFailed')}
        </p>
      )}
      {organizations.data &&
        (organizations.data.length === 0 ? (
          <p>{t('organizations.empty')}</p>
        ) : (
          <ul>
            {organizations.data.map((organization) => (
              <li key={organization.id}>
                <Link to={`/organizations/${organization.id}`}>{organization.displayName}</Link>{' '}
                <span className="slug">{organization.slug}</span>
              </li>
            ))}
          </ul>
        ))}
      <CreateOrganizationForm />
    </section>
  )
}
