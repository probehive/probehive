import { useParams } from 'react-router'

import OrganizationOverview from './OrganizationOverview.tsx'

export default function OrganizationOverviewPage() {
  const { organizationId } = useParams<'organizationId'>()
  return <OrganizationOverview organizationId={organizationId ?? ''} />
}
