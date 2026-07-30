import { getJson } from './http'

export type HealthState = 'unknown' | 'healthy' | 'degraded' | 'down'
export type StableHealthState = Exclude<HealthState, 'degraded'>
export type HealthTransitionDirection = 'failure' | 'recovery'
export type HealthEvidence = 'passing' | 'failing'

export interface HealthCountsResponse {
  configured: number
  eligible: number
  responding: number
  passing: number
  failing: number
  locationFault: number
  indeterminate: number
  missing: number
}

export interface HealthCandidateResponse {
  id: string
  direction: HealthTransitionDirection
  expectedEvidence: HealthEvidence
  sourceRevisionNumber: number
  triggeringRunId: string
  triggeringScheduledFor: string
  requestedAt: string
}

export interface MonitorHealthResponse {
  organizationId: string
  projectId: string
  monitorId: string
  state: HealthState
  stableState: StableHealthState
  policyVersion: string
  version: number
  sourceRevisionNumber: number | null
  lastScheduledFor: string | null
  lastDeterminateFinishedAt: string | null
  lastRunId: string | null
  lastRunScheduledFor: string | null
  candidate: HealthCandidateResponse | null
  counts: HealthCountsResponse
  transitionedAt: string
  updatedAt: string
}

export function getMonitorHealth(
  organizationId: string,
  projectId: string,
  monitorId: string,
): Promise<MonitorHealthResponse> {
  return getJson<MonitorHealthResponse>(
    `/api/v1/organizations/${encodeURIComponent(organizationId)}/projects/${encodeURIComponent(projectId)}/monitors/${encodeURIComponent(monitorId)}/health`,
  )
}
