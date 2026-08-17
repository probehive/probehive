import { getJson, postJson, putJson } from './http'

export { ApiError, type ProblemDetails } from './http'

export interface WebhookIntegrationResponse {
  id: string
  organizationId: string
  name: string
  destinationUrl: string
  enabled: boolean
  version: number
  activeSecretVersion: number
  pendingSecretVersion: number | null
  retiringSecretVersion: number | null
  createdAt: string
  updatedAt: string
}

export interface CreateWebhookIntegrationResponse {
  integration: WebhookIntegrationResponse
  signingSecret: string
}

export interface PrepareWebhookSigningSecretResponse {
  integration: WebhookIntegrationResponse
  secretVersion: number
  signingSecret: string
}

function integrationsPath(organizationId: string): string {
  return '/api/v1/organizations/' + encodeURIComponent(organizationId) + '/webhook-integrations'
}

function integrationPath(organizationId: string, integrationId: string): string {
  return integrationsPath(organizationId) + '/' + encodeURIComponent(integrationId)
}

export function webhookIntegrationsQueryKey(organizationId: string) {
  return ['webhook-integrations', organizationId] as const
}

export function listWebhookIntegrations(
  organizationId: string,
): Promise<WebhookIntegrationResponse[]> {
  return getJson<WebhookIntegrationResponse[]>(integrationsPath(organizationId))
}

export async function createWebhookIntegration(
  organizationId: string,
  name: string,
  destinationUrl: string,
): Promise<CreateWebhookIntegrationResponse> {
  const response = await postJson(integrationsPath(organizationId), { name, destinationUrl })
  return (await response.json()) as CreateWebhookIntegrationResponse
}

export async function setWebhookIntegrationEnabled(
  organizationId: string,
  integrationId: string,
  enabled: boolean,
  version: number,
): Promise<WebhookIntegrationResponse> {
  const response = await putJson(integrationPath(organizationId, integrationId) + '/state', {
    enabled,
    version,
  })
  return (await response.json()) as WebhookIntegrationResponse
}

export async function prepareWebhookSigningSecret(
  organizationId: string,
  integrationId: string,
  version: number,
): Promise<PrepareWebhookSigningSecretResponse> {
  const response = await postJson(
    integrationPath(organizationId, integrationId) + '/signing-secrets/prepare',
    { version },
  )
  return (await response.json()) as PrepareWebhookSigningSecretResponse
}

export async function activateWebhookSigningSecret(
  organizationId: string,
  integrationId: string,
  version: number,
): Promise<WebhookIntegrationResponse> {
  const response = await postJson(
    integrationPath(organizationId, integrationId) + '/signing-secrets/activate',
    { version },
  )
  return (await response.json()) as WebhookIntegrationResponse
}

export async function retireWebhookSigningSecret(
  organizationId: string,
  integrationId: string,
  version: number,
): Promise<WebhookIntegrationResponse> {
  const response = await postJson(
    integrationPath(organizationId, integrationId) + '/signing-secrets/retire',
    { version },
  )
  return (await response.json()) as WebhookIntegrationResponse
}
