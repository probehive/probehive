export function monitorsQueryKey(organizationId: string, projectId: string) {
  return ['monitors', organizationId, projectId] as const
}

export function monitorQueryKey(
  organizationId: string,
  projectId: string,
  monitorId: string,
) {
  return [...monitorsQueryKey(organizationId, projectId), monitorId] as const
}
export function monitorInventoryQueryKey(
  organizationId: string,
  projectId: string,
  query: object,
) {
  return [...monitorsQueryKey(organizationId, projectId), 'inventory', query] as const
}
