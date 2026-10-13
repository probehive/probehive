export function monitorsQueryKey(organizationId: string) {
  return ['monitors', organizationId] as const
}

export function monitorQueryKey(organizationId: string, monitorId: string) {
  return [...monitorsQueryKey(organizationId), monitorId] as const
}
export function monitorInventoryQueryKey(
  organizationId: string,
  query: object,
) {
  return [...monitorsQueryKey(organizationId), 'inventory', query] as const
}
