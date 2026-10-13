import { getJson, postJson } from './http'

export type MaintenanceWindowStatus = 'upcoming' | 'active' | 'ended' | 'cancelled'

export interface MaintenanceWindowResponse {
  id: string
  organizationId: string
  monitorId: string
  startsAt: string
  endsAt: string
  status: MaintenanceWindowStatus
  createdAt: string
  cancelledAt: string | null
}

function collectionPath(
  organizationId: string,
  monitorId: string,
): string {
  return '/api/v1/organizations/' + encodeURIComponent(organizationId) +
        '/monitors/' + encodeURIComponent(monitorId) + '/maintenance-windows'
}

export function listMaintenanceWindows(
  organizationId: string,
  monitorId: string,
): Promise<MaintenanceWindowResponse[]> {
  return getJson<MaintenanceWindowResponse[]>(
    collectionPath(organizationId, monitorId),
  )
}

export async function createMaintenanceWindow(
  organizationId: string,
  monitorId: string,
  startsAt: string,
  endsAt: string,
): Promise<MaintenanceWindowResponse> {
  const response = await postJson(
    collectionPath(organizationId, monitorId),
    { startsAt, endsAt },
  )
  return (await response.json()) as MaintenanceWindowResponse
}

export async function cancelMaintenanceWindow(
  organizationId: string,
  monitorId: string,
  maintenanceWindowId: string,
): Promise<MaintenanceWindowResponse> {
  const response = await postJson(
    collectionPath(organizationId, monitorId) +
      '/' + encodeURIComponent(maintenanceWindowId) + '/cancel',
  )
  return (await response.json()) as MaintenanceWindowResponse
}
