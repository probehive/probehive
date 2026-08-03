import { ApiError, getJson, postJson, readProblem, refreshAntiforgery } from './http'
import type { OrganizationResponse } from './organizations'

export interface SetupStatusResponse {
  setupComplete: boolean
}

export interface SessionResponse {
  userId: string
  email: string
  displayName: string
  role: string
}

export interface UserResponse {
  id: string
  email: string
  displayName: string
  role: string
  createdAt: string
}

export interface CreateFirstAdministratorRequest {
  email: string
  displayName: string
  password: string
}

/** Setup also provisions the installation Organization, so a new install is usable at once. */
export interface SetupResponse {
  user: UserResponse
  organization: OrganizationResponse
}

export function getSetupStatus(): Promise<SetupStatusResponse> {
  return getJson<SetupStatusResponse>('/api/v1/setup/status')
}

/** Resolves to null when there is no authenticated session. */
export async function getSession(): Promise<SessionResponse | null> {
  const response = await fetch('/api/v1/auth/session')
  if (response.status === 401) {
    return null
  }
  if (!response.ok) {
    throw new ApiError(response.status, await readProblem(response))
  }
  return (await response.json()) as SessionResponse
}

export async function login(email: string, password: string): Promise<SessionResponse> {
  const response = await postJson('/api/v1/auth/login', { email, password })
  const session = (await response.json()) as SessionResponse
  // The previous token belonged to the anonymous identity.
  await refreshAntiforgery()
  return session
}

export async function logout(): Promise<void> {
  await postJson('/api/v1/auth/logout')
  await refreshAntiforgery()
}

export async function createFirstAdministrator(request: CreateFirstAdministratorRequest): Promise<SetupResponse> {
  const response = await postJson('/api/v1/setup/admin', request)
  const result = (await response.json()) as SetupResponse
  // Setup signs the new administrator in, which invalidates the anonymous token.
  await refreshAntiforgery()
  return result
}
