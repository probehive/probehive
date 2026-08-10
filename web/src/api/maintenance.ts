import { getJson, postJson } from './http'

export type MaintenanceWindowStatus = 'upcoming' | 'active' | 'ended' | 'cancelled'

export interface MaintenanceWindowResponse {
  id: string
  organizationId: string
  projectId: string
  monitorId: string
  startsAt: string
  endsAt: string
  status: MaintenanceWindowStatus
  createdAt: string
  cancelledAt: string | null
}

function collectionPath(
  organizationId: string,
  projectId: string,
  monitorId: string,
): string {
  return '/api/v1/organizations/' + encodeURIComponent(organizationId) +
    '/projects/' + encodeURIComponent(projectId) +
    '/monitors/' + encodeURIComponent(monitorId) + '/maintenance-windows'
}

export function listMaintenanceWindows(
  organizationId: string,
  projectId: string,
  monitorId: string,
): Promise<MaintenanceWindowResponse[]> {
  return getJson<MaintenanceWindowResponse[]>(
    collectionPath(organizationId, projectId, monitorId),
  )
}

export async function createMaintenanceWindow(
  organizationId: string,
  projectId: string,
  monitorId: string,
  startsAt: string,
  endsAt: string,
): Promise<MaintenanceWindowResponse> {
  const response = await postJson(
    collectionPath(organizationId, projectId, monitorId),
    { startsAt, endsAt },
  )
  return (await response.json()) as MaintenanceWindowResponse
}

export async function cancelMaintenanceWindow(
  organizationId: string,
  projectId: string,
  monitorId: string,
  maintenanceWindowId: string,
): Promise<MaintenanceWindowResponse> {
  const response = await postJson(
    collectionPath(organizationId, projectId, monitorId) +
      '/' + encodeURIComponent(maintenanceWindowId) + '/cancel',
  )
  return (await response.json()) as MaintenanceWindowResponse
}
