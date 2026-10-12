import { useOutletContext } from 'react-router'

import type { OrganizationContext } from './OrganizationLayout.tsx'
import StatusPageDraftSection from './StatusPageDraftSection.tsx'

export default function OrganizationStatusPage() {
  const { organization } = useOutletContext<OrganizationContext>()
  return (
    <StatusPageDraftSection
      organizationId={organization.id}
      projectId={organization.defaultProject.id}
    />
  )
}
