export interface ProblemDetails {
  type?: string
  title?: string
  status?: number
  detail?: string
  errors?: Record<string, string[]>
}

export class ApiError extends Error {
  readonly status: number
  readonly problem: ProblemDetails

  constructor(status: number, problem: ProblemDetails) {
    super(problem.title ?? `Request failed with status ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.problem = problem
  }
}

export async function readProblem(response: Response): Promise<ProblemDetails> {
  try {
    return (await response.json()) as ProblemDetails
  } catch {
    return { status: response.status }
  }
}

interface AntiforgeryToken {
  headerName: string
  requestToken: string
}

let antiforgery: AntiforgeryToken | null = null

/**
 * Fetches a fresh antiforgery token. Tokens bind to the authenticated identity,
 * so this must run again after login, logout, and setup (ADR 0013).
 */
export async function refreshAntiforgery(): Promise<void> {
  const response = await fetch('/api/v1/auth/antiforgery')
  if (!response.ok) {
    throw new ApiError(response.status, await readProblem(response))
  }
  antiforgery = (await response.json()) as AntiforgeryToken
}

/** Test seam: pre-seeds or clears the cached antiforgery token. */
export function setAntiforgeryForTests(token: { headerName: string; requestToken: string } | null): void {
  antiforgery = token
}

export async function getJson<T>(url: string): Promise<T> {
  const response = await fetch(url)
  if (!response.ok) {
    throw new ApiError(response.status, await readProblem(response))
  }
  return (await response.json()) as T
}

/** Sends an unsafe request with the antiforgery header; throws ApiError on failure. */
export async function postJson(url: string, body?: unknown): Promise<Response> {
  if (antiforgery === null) {
    await refreshAntiforgery()
  }
  const token = antiforgery as AntiforgeryToken
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      [token.headerName]: token.requestToken,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!response.ok) {
    throw new ApiError(response.status, await readProblem(response))
  }
  return response
}
