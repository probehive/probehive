import { getJson } from './http'

export type RunOutcome = 'passed' | 'failed' | 'errored' | 'timedout' | 'cancelled' | 'skipped'
export type RunKind = 'scheduled' | 'confirmation' | 'manual'

export interface ConfirmationCauseResponse {
  candidateId: string
  triggeringRunId: string
  triggeringScheduledFor: string
  causationEventId: string
  policyVersion: string
}

export interface RunResponse {
  id: string
  organizationId: string
  projectId: string
  monitorId: string
  revisionNumber: number
  location: string
  scheduledFor: string
  kind: RunKind
  outcome: RunOutcome | null
  startedAt: string | null
  finishedAt: string | null
  leaseExpiresAt: string | null
  confirmation: ConfirmationCauseResponse | null
}

export interface RunPageResponse {
  items: RunResponse[]
  nextCursor: string | null
}

export interface ObservationPhasesResponse {
  connectMicroseconds: number
  tlsMicroseconds: number
  firstByteMicroseconds: number
}

export interface TLSObservationResponse {
  version: string
  cipherSuite: string
  certificateExpiresAt: string | null
}

export interface HTTPObservationResponse {
  statusCode: number
  protocol: string
  redirectCount: number
  bodyBytes: number
  bodyTruncated: boolean
  tls: TLSObservationResponse | null
}

export interface ObservationResponse {
  runId: string
  organizationId: string
  scheduledFor: string
  failureCode: string
  failureClass: string
  durationMicroseconds: number
  phases: ObservationPhasesResponse
  http: HTTPObservationResponse | null
}

interface ListRunsOptions {
  notBefore: string
  pageSize?: number
  cursor?: string
}

function runsPath(organizationId: string, projectId: string, monitorId: string): string {
  return `/api/v1/organizations/${encodeURIComponent(organizationId)}/projects/${encodeURIComponent(projectId)}/monitors/${encodeURIComponent(monitorId)}/runs`
}

export function listRuns(
  organizationId: string,
  projectId: string,
  monitorId: string,
  options: ListRunsOptions,
): Promise<RunPageResponse> {
  const query = new URLSearchParams({
    notBefore: options.notBefore,
    pageSize: String(options.pageSize ?? 25),
  })
  if (options.cursor !== undefined) {
    query.set('cursor', options.cursor)
  }
  return getJson<RunPageResponse>(
    `${runsPath(organizationId, projectId, monitorId)}?${query.toString()}`,
  )
}

export function getRun(
  organizationId: string,
  projectId: string,
  monitorId: string,
  runId: string,
): Promise<RunResponse> {
  return getJson<RunResponse>(
    `${runsPath(organizationId, projectId, monitorId)}/${encodeURIComponent(runId)}`,
  )
}

export function getObservation(
  organizationId: string,
  projectId: string,
  monitorId: string,
  runId: string,
): Promise<ObservationResponse> {
  return getJson<ObservationResponse>(
    `${runsPath(organizationId, projectId, monitorId)}/${encodeURIComponent(runId)}/observation`,
  )
}
