import { getJson, postJson } from './http'

export { ApiError, type ProblemDetails } from './http'

export interface UserResponse {
  id: string
  email: string
  displayName: string
  role: string
  createdAt: string
}

export interface CreateUserRequest {
  email: string
  displayName: string
  password: string
}

/** Every local user of this instance, in creation order. */
export function listUsers(): Promise<UserResponse[]> {
  return getJson<UserResponse[]>('/api/v1/users')
}

export async function createUser(request: CreateUserRequest): Promise<UserResponse> {
  const response = await postJson('/api/v1/users', request)
  return (await response.json()) as UserResponse
}
