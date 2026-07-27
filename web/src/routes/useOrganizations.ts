import { useQuery } from '@tanstack/react-query'

import { listOrganizations } from '../api/organizations'

export const organizationsQueryKey = ['organizations'] as const

export function useOrganizations() {
  return useQuery({
    queryKey: organizationsQueryKey,
    queryFn: listOrganizations,
  })
}
