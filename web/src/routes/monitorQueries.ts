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
