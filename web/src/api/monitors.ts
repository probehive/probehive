import { getJson, postJson, putJson } from './http'

export { ApiError } from './http'

export type MonitorState = 'draft' | 'active' | 'paused' | 'archived'
export type MonitorStateTarget = Exclude<MonitorState, 'draft'>

export interface MonitorResponse {
  id: string
  organizationId: string
  name: string
  checkType: string
  state: MonitorState
  intervalSeconds: number
  latestRevisionNumber: number
  createdAt: string
  updatedAt: string
}

export interface MonitorRevisionResponse {
  id: string
  monitorId: string
  revisionNumber: number
  checkType: string
  checkSchemaVersion: number
  checkConfiguration: unknown
  createdAt: string
}

function collectionPath(organizationId: string): string {
  return `/api/v1/organizations/${encodeURIComponent(organizationId)}/monitors`
}

function monitorPath(organizationId: string, monitorId: string): string {
  return `${collectionPath(organizationId)}/${encodeURIComponent(monitorId)}`
}

export function listMonitors(organizationId: string): Promise<MonitorResponse[]> {
  return getJson<MonitorResponse[]>(collectionPath(organizationId))
}

export function getMonitor(organizationId: string, monitorId: string): Promise<MonitorResponse> {
  return getJson<MonitorResponse>(monitorPath(organizationId, monitorId))
}

export async function renameMonitor(
  organizationId: string,
  monitorId: string,
  name: string,
): Promise<MonitorResponse> {
  const response = await putJson(
    `${monitorPath(organizationId, monitorId)}/name`,
    { name },
  )
  return (await response.json()) as MonitorResponse
}

export async function changeMonitorInterval(
  organizationId: string,
  monitorId: string,
  intervalSeconds: number,
): Promise<MonitorResponse> {
  const response = await putJson(
    `${monitorPath(organizationId, monitorId)}/interval`,
    { intervalSeconds },
  )
  return (await response.json()) as MonitorResponse
}

export async function createHTTPMonitor(
  organizationId: string,
  name: string,
  intervalSeconds: number,
): Promise<MonitorResponse> {
  const response = await postJson(collectionPath(organizationId), {
    name,
    checkType: 'http',
    intervalSeconds,
  })
  return (await response.json()) as MonitorResponse
}

export async function createHTTPRevision(
  organizationId: string,
  monitorId: string,
  url: string,
): Promise<MonitorRevisionResponse> {
  const response = await postJson(
    `${monitorPath(organizationId, monitorId)}/revisions`,
    {
      checkSchemaVersion: 1,
      checkConfiguration: { url },
    },
  )
  return (await response.json()) as MonitorRevisionResponse
}

export async function changeMonitorState(
  organizationId: string,
  monitorId: string,
  state: MonitorStateTarget,
): Promise<MonitorResponse> {
  const response = await putJson(
    `${monitorPath(organizationId, monitorId)}/state`,
    { state },
  )
  return (await response.json()) as MonitorResponse
}

export function activateMonitor(
  organizationId: string,
  monitorId: string,
): Promise<MonitorResponse> {
  return changeMonitorState(organizationId, monitorId, 'active')
}
export type MonitorHealthState = 'notEvaluated' | 'unknown' | 'healthy' | 'degraded' | 'down'
export type MonitorRunOutcome = 'notRun' | 'inProgress' | 'passed' | 'failed' | 'errored' | 'timedout' | 'cancelled' | 'skipped'
export type MonitorMaintenanceState = 'none' | 'upcoming' | 'active'
export type MonitorInventorySort = 'name' | 'createdAt' | 'updatedAt'
export type MonitorInventoryDirection = 'asc' | 'desc'

export interface MonitorInventoryItemResponse {
  monitor: MonitorResponse
  health: null | { state: Exclude<MonitorHealthState, 'notEvaluated'>; updatedAt: string }
  lastRun: null | { id: string; outcome: Exclude<MonitorRunOutcome, 'notRun'>; scheduledFor: string }
  maintenance: {
    state: MonitorMaintenanceState
    windowId: string | null
    startsAt: string | null
    endsAt: string | null
  }
}

export interface MonitorInventoryPageResponse {
  items: MonitorInventoryItemResponse[]
  page: number
  pageSize: number
  total: number
}

export interface MonitorInventoryQuery {
  search?: string
  state?: MonitorState
  health?: MonitorHealthState
  runOutcome?: MonitorRunOutcome
  maintenance?: MonitorMaintenanceState
  sort: MonitorInventorySort
  direction: MonitorInventoryDirection
  page: number
  pageSize: number
}
export function listMonitorInventory(
  organizationId: string,
  query: MonitorInventoryQuery,
): Promise<MonitorInventoryPageResponse> {
  const parameters = new URLSearchParams()
  if (query.search) parameters.set('search', query.search)
  if (query.state) parameters.set('state', query.state)
  if (query.health) parameters.set('health', query.health)
  if (query.runOutcome) parameters.set('runOutcome', query.runOutcome)
  if (query.maintenance) parameters.set('maintenance', query.maintenance)
  parameters.set('sort', query.sort)
  parameters.set('direction', query.direction)
  parameters.set('page', String(query.page))
  parameters.set('pageSize', String(query.pageSize))
  const path = `/api/v1/organizations/${encodeURIComponent(organizationId)}/monitor-inventory`
  return getJson<MonitorInventoryPageResponse>(`${path}?${parameters.toString()}`)
}
