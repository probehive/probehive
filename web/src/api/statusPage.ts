import { getOptionalJson, putJson } from './http'

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
  createdAt: string
  updatedAt: string
}

export interface StatusComponentInput {
  monitorId: string
  label: string
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
