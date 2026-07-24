import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router'

import { ApiError, getOrganization } from '../api/organizations'

export default function OrganizationPage() {
  const { organizationId } = useParams<'organizationId'>()
  const query = useQuery({
    queryKey: ['organizations', organizationId],
    queryFn: () => getOrganization(organizationId ?? ''),
    enabled: organizationId !== undefined,
  })

  if (query.isPending) {
    return <p>Loading…</p>
  }

  if (query.isError) {
    const notFound = query.error instanceof ApiError && query.error.status === 404
    return (
      <p className="error" role="alert">
        {notFound ? 'This Organization does not exist.' : 'The Organization could not be loaded.'}
      </p>
    )
  }

  const organization = query.data
  return (
    <section>
      <h1>{organization.displayName}</h1>
      <dl>
        <dt>Slug</dt>
        <dd>{organization.slug}</dd>
        <dt>Identifier</dt>
        <dd>
          <code>{organization.id}</code>
        </dd>
        <dt>Created</dt>
        <dd>{new Date(organization.createdAt).toISOString()}</dd>
      </dl>
      <h2>Default Project</h2>
      <dl>
        <dt>Name</dt>
        <dd>{organization.defaultProject.name}</dd>
        <dt>Identifier</dt>
        <dd>
          <code>{organization.defaultProject.id}</code>
        </dd>
      </dl>
    </section>
  )
}
