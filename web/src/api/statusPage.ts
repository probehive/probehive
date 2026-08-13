import { deleteJson, getJson, getOptionalJson, postJson, putJson } from './http'

export interface StatusComponentResponse {
  id: string
  monitorId: string
  label: string
  position: number
}

export interface StatusPageDraftResponse {
  id: string
  organizationId: string
  title: string
  version: number
  components: StatusComponentResponse[]
  publication: { publishedAt: string } | null
  createdAt: string
  updatedAt: string
}

export interface StatusComponentInput {
  monitorId: string
  label: string
}

export interface PublishStatusPageResponse {
  publicUrl: string
  publishedAt: string
}

export interface PublicStatusComponentResponse {
  label: string
  state: 'unknown' | 'healthy' | 'degraded' | 'down'
  updatedAt: string
  maintenance: boolean
}

export interface PublicStatusPageResponse {
  title: string
  components: PublicStatusComponentResponse[]
}

function draftPath(organizationId: string): string {
  return `/api/v1/organizations/${encodeURIComponent(organizationId)}/status-page/draft`
}

export function getStatusPageDraft(
  organizationId: string,
): Promise<StatusPageDraftResponse | null> {
  return getOptionalJson<StatusPageDraftResponse>(draftPath(organizationId))
}

export async function replaceStatusPageDraft(
  organizationId: string,
  title: string,
  version: number,
  components: StatusComponentInput[],
): Promise<StatusPageDraftResponse> {
  const response = await putJson(draftPath(organizationId), {
    title,
    version,
    components,
  })
  return (await response.json()) as StatusPageDraftResponse
}

function publicationPath(organizationId: string): string {
  return `/api/v1/organizations/${encodeURIComponent(organizationId)}/status-page/publication`
}

export async function publishStatusPage(organizationId: string): Promise<PublishStatusPageResponse> {
  const response = await postJson(publicationPath(organizationId))
  return (await response.json()) as PublishStatusPageResponse
}

export async function revokeStatusPage(organizationId: string): Promise<void> {
  await deleteJson(publicationPath(organizationId))
}

export function getPublicStatusPage(token: string): Promise<PublicStatusPageResponse> {
  return getJson<PublicStatusPageResponse>(`/api/v1/status-pages/${encodeURIComponent(token)}`)
}
