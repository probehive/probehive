import { getJson } from './http'

export type OverviewIncidentState = 'open' | 'acknowledged'

export interface OrganizationOverviewResponse {
  organizationId: string
  monitors: OrganizationOverviewMonitorCounts | null
  health: OrganizationOverviewHealthCounts | null
  incidents: OrganizationOverviewIncidentSummary | null
  integrations: OrganizationOverviewIntegrationCounts | null
  statusPage: OrganizationOverviewStatusPageState | null
  capabilities: OrganizationOverviewCapabilities
}

export interface OrganizationOverviewMonitorCounts {
  total: number
  draft: number
  active: number
  paused: number
  archived: number
}

export interface OrganizationOverviewHealthCounts {
  notEvaluated: number
  unknown: number
  healthy: number
  degraded: number
  down: number
}

export interface OrganizationOverviewIncidentSummary {
  active: number
  open: number
  acknowledged: number
  activePreview: OrganizationOverviewActiveIncident[]
  activePreviewTruncated: boolean
}

export interface OrganizationOverviewActiveIncident {
  id: string
  projectId: string
  monitorId: string
  monitorName: string
  state: OverviewIncidentState
  updatedAt: string
}

export interface OrganizationOverviewIntegrationCounts {
  total: number
  enabled: number
}

export interface OrganizationOverviewStatusPageState {
  configured: boolean
  published: boolean
}

export interface OrganizationOverviewCapabilities {
  manageOrganization: boolean
  manageIntegrations: boolean
  manageStatusPage: boolean
}

export function organizationOverviewQueryKey(organizationId: string) {
  return ['organization-overview', organizationId] as const
}

export function getOrganizationOverview(
  organizationId: string,
): Promise<OrganizationOverviewResponse> {
  return getJson<OrganizationOverviewResponse>(
    '/api/v1/organizations/' + encodeURIComponent(organizationId) + '/overview',
  )
}
