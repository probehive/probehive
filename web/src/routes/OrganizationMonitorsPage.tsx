import { useOutletContext } from 'react-router'

import MonitorInventoryPanel from './MonitorInventoryPanel.tsx'
import type { OrganizationContext } from './OrganizationLayout.tsx'

export default function OrganizationMonitorsPage() {
  const { organization } = useOutletContext<OrganizationContext>()
  return (
    <MonitorInventoryPanel
      organizationId={organization.id}
      projectId={organization.defaultProject.id}
    />
  )
}
