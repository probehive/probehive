import { getJson, postJson, putJson } from './http'

export { ApiError } from './http'

export type MonitorState = 'draft' | 'active' | 'paused' | 'archived'

export interface MonitorResponse {
  id: string
  organizationId: string
  projectId: string
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

function collectionPath(organizationId: string, projectId: string): string {
  return `/api/v1/organizations/${encodeURIComponent(organizationId)}/projects/${encodeURIComponent(projectId)}/monitors`
}

function monitorPath(organizationId: string, projectId: string, monitorId: string): string {
  return `${collectionPath(organizationId, projectId)}/${encodeURIComponent(monitorId)}`
}

export function listMonitors(organizationId: string, projectId: string): Promise<MonitorResponse[]> {
  return getJson<MonitorResponse[]>(collectionPath(organizationId, projectId))
}

export function getMonitor(organizationId: string, projectId: string, monitorId: string): Promise<MonitorResponse> {
  return getJson<MonitorResponse>(monitorPath(organizationId, projectId, monitorId))
}

export async function createHTTPMonitor(
  organizationId: string,
  projectId: string,
  name: string,
  intervalSeconds: number,
): Promise<MonitorResponse> {
  const response = await postJson(collectionPath(organizationId, projectId), {
    name,
    checkType: 'http',
    intervalSeconds,
  })
  return (await response.json()) as MonitorResponse
}

export async function createHTTPRevision(
  organizationId: string,
  projectId: string,
  monitorId: string,
  url: string,
): Promise<MonitorRevisionResponse> {
  const response = await postJson(
    `${monitorPath(organizationId, projectId, monitorId)}/revisions`,
    {
      checkSchemaVersion: 1,
      checkConfiguration: { url },
    },
  )
  return (await response.json()) as MonitorRevisionResponse
}

export async function activateMonitor(
  organizationId: string,
  projectId: string,
  monitorId: string,
): Promise<MonitorResponse> {
  const response = await putJson(
    `${monitorPath(organizationId, projectId, monitorId)}/state`,
    { state: 'active' },
  )
  return (await response.json()) as MonitorResponse
}
