import { useOutletContext } from 'react-router'

import { useTranslation } from '../i18n/context'
import type { OrganizationContext } from './OrganizationLayout.tsx'
import RenameOrganizationForm from './RenameOrganizationForm.tsx'

export default function OrganizationSettingsPage() {
  const { organization } = useOutletContext<OrganizationContext>()
  const { t, formatDateTime } = useTranslation()

  return (
    <section
      className="organization-settings"
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
  )
}
