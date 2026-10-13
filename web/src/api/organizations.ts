import { getJson, postJson, putJson } from './http'

export { ApiError, type ProblemDetails } from './http'

export interface ProjectResponse {
  id: string
  organizationId: string
  name: string
  isDefault: boolean
  createdAt: string
}

export interface OrganizationResponse {
  id: string
  displayName: string
  createdAt: string
  defaultProject: ProjectResponse
}

export interface CreateOrganizationRequest {
  displayName: string
}

/** Creates an Organization; the id is its only stable identifier. */
export async function createOrganization(
  request: CreateOrganizationRequest,
): Promise<OrganizationResponse> {
  const response = await postJson('/api/v1/organizations', request)
  return (await response.json()) as OrganizationResponse
}

/** The Organizations the caller can see, in creation order. */
export function listOrganizations(): Promise<OrganizationResponse[]> {
  return getJson<OrganizationResponse[]>('/api/v1/organizations')
}

/** Changes only the display name. */
export async function renameOrganization(
  organizationId: string,
  displayName: string,
): Promise<OrganizationResponse> {
  const response = await putJson(
    `/api/v1/organizations/${encodeURIComponent(organizationId)}/name`,
    { displayName },
  )
  return (await response.json()) as OrganizationResponse
}

export function getOrganization(organizationId: string): Promise<OrganizationResponse> {
  return getJson<OrganizationResponse>(`/api/v1/organizations/${encodeURIComponent(organizationId)}`)
}
