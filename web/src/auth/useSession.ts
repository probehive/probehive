import { useQuery } from '@tanstack/react-query'

import { getSession } from '../api/auth'

export const sessionQueryKey = ['session'] as const

export function useSession() {
  return useQuery({
    queryKey: sessionQueryKey,
    queryFn: getSession,
    staleTime: 60_000,
  })
}
