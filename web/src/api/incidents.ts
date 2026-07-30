import type { HealthCountsResponse, HealthState } from './health'
import { getJson, postJson } from './http'

export type IncidentState = 'open' | 'acknowledged' | 'resolved'
export type IncidentTimelineKind = 'opened' | 'acknowledged' | 'resolved'

export interface IncidentTimelineResponse {
  id: string
  incidentVersion: number
  kind: IncidentTimelineKind
  healthTransitionId: string | null
  actorUserId: string | null
  oldHealthState: HealthState | null
  newHealthState: HealthState | null
  policyVersion: string | null
  causalRunId: string | null
  causalRunScheduledFor: string | null
  counts: HealthCountsResponse | null
  occurredAt: string
}

export interface IncidentResponse {
  id: string
  organizationId: string
  projectId: string
  monitorId: string
  state: IncidentState
  version: number
  openedTransitionId: string
  acknowledgedBy: string | null
  acknowledgedAt: string | null
  resolvedTransitionId: string | null
  resolvedAt: string | null
  createdAt: string
  updatedAt: string
  timeline: IncidentTimelineResponse[]
}

export interface IncidentPageResponse {
  items: IncidentResponse[]
  nextCursor: string | null
}

interface ListIncidentsOptions {
  pageSize?: number
  cursor?: string
}

function incidentsPath(
  organizationId: string,
  projectId: string,
  monitorId: string,
): string {
  return '/api/v1/organizations/' + encodeURIComponent(organizationId) +
    '/projects/' + encodeURIComponent(projectId) +
    '/monitors/' + encodeURIComponent(monitorId) + '/incidents'
}

export function listIncidents(
  organizationId: string,
  projectId: string,
  monitorId: string,
  options: ListIncidentsOptions = {},
): Promise<IncidentPageResponse> {
  const query = new URLSearchParams({
    pageSize: String(options.pageSize ?? 25),
  })
  if (options.cursor !== undefined) {
    query.set('cursor', options.cursor)
  }
  return getJson<IncidentPageResponse>(
    incidentsPath(organizationId, projectId, monitorId) + '?' + query.toString(),
  )
}

export function getIncident(
  organizationId: string,
  projectId: string,
  monitorId: string,
  incidentId: string,
): Promise<IncidentResponse> {
  return getJson<IncidentResponse>(
    incidentsPath(organizationId, projectId, monitorId) + '/' + encodeURIComponent(incidentId),
  )
}

export async function acknowledgeIncident(
  organizationId: string,
  projectId: string,
  monitorId: string,
  incidentId: string,
): Promise<IncidentResponse> {
  const response = await postJson(
    incidentsPath(organizationId, projectId, monitorId) + '/' +
      encodeURIComponent(incidentId) + '/acknowledge',
  )
  return (await response.json()) as IncidentResponse
}
