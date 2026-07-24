import { getJson, postJson } from './http'

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
  slug: string
  displayName: string
  createdAt: string
  defaultProject: ProjectResponse
}

export interface CreateOrganizationRequest {
  slug: string
  displayName: string
}

export interface CreateOrganizationOutcome {
  organization: OrganizationResponse
  /** True when this call created the Organization; false for an idempotent replay. */
  created: boolean
}

export async function createOrganization(request: CreateOrganizationRequest): Promise<CreateOrganizationOutcome> {
  const response = await postJson('/api/v1/organizations', request)
  return {
    organization: (await response.json()) as OrganizationResponse,
    created: response.status === 201,
  }
}

export function getOrganization(organizationId: string): Promise<OrganizationResponse> {
  return getJson<OrganizationResponse>(`/api/v1/organizations/${encodeURIComponent(organizationId)}`)
}
