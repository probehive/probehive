import { Link } from 'react-router'

import CreateOrganizationForm from './CreateOrganizationForm.tsx'
import { useOrganizations } from './useOrganizations'

/**
 * The signed-in landing page. Setup provisions the installation Organization
 * (ADR 0018), so this normally lists at least one and the create form is an
 * option rather than a required step.
 */
export default function OrganizationsPage() {
  const organizations = useOrganizations()

  return (
    <section>
      <h1>Organizations</h1>
      {organizations.isPending && <p role="status">Loading Organizations…</p>}
      {organizations.isError && (
        <p className="error" role="alert">
          Organizations could not be loaded.
        </p>
      )}
      {organizations.data &&
        (organizations.data.length === 0 ? (
          <p>No Organizations yet.</p>
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
