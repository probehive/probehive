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

export type DeliveryAttemptOutcome =
  | 'inProgress'
  | 'succeeded'
  | 'failed'
  | 'cancelled'

export interface DeliveryAttemptResponse {
  sequence: number
  startedAt: string
  finishedAt: string | null
  outcome: DeliveryAttemptOutcome
  httpStatus: number | null
  failureCode: string | null
}

export type DeliverySuppressionReason = 'maintenance'

export interface AlertDeliveryResponse {
  id: string
  channel: 'webhook'
  integrationId: string
  integrationVersion: number
  secretVersion: number
  routedAt: string
  suppressionReason: DeliverySuppressionReason | null
  maintenanceWindowId: string | null
  attempts: DeliveryAttemptResponse[]
}

export interface AlertDeliveryPageResponse {
  items: AlertDeliveryResponse[]
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

export function listAlertDeliveries(
  organizationId: string,
  projectId: string,
  monitorId: string,
  alertId: string,
): Promise<AlertDeliveryPageResponse> {
  return getJson<AlertDeliveryPageResponse>(
    alertsPath(organizationId, projectId, monitorId) + '/' +
      encodeURIComponent(alertId) + '/deliveries',
  )
}
