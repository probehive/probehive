import { getJson } from './http'

export type AlertKind = 'incident.opened' | 'incident.resolved'

export interface AlertResponse {
  id: string
  organizationId: string
  projectId: string
  monitorId: string
  incidentId: string
  incidentVersion: number
  kind: AlertKind
  occurredAt: string
  createdAt: string
}

export interface AlertPageResponse {
  items: AlertResponse[]
  nextCursor: string | null
}

interface ListAlertsOptions {
  pageSize?: number
  cursor?: string
}

function alertsPath(
  organizationId: string,
  projectId: string,
  monitorId: string,
): string {
  return '/api/v1/organizations/' + encodeURIComponent(organizationId) +
    '/projects/' + encodeURIComponent(projectId) +
    '/monitors/' + encodeURIComponent(monitorId) + '/alerts'
}

export function listAlerts(
  organizationId: string,
  projectId: string,
  monitorId: string,
  options: ListAlertsOptions = {},
): Promise<AlertPageResponse> {
  const query = new URLSearchParams({
    pageSize: String(options.pageSize ?? 25),
  })
  if (options.cursor !== undefined) {
    query.set('cursor', options.cursor)
  }
  return getJson<AlertPageResponse>(
    alertsPath(organizationId, projectId, monitorId) + '?' + query.toString(),
  )
}
