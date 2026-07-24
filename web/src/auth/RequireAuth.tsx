import { Navigate, Outlet } from 'react-router'

import { useSession } from './useSession'

/** Layout route that gates its children behind an authenticated session (ADR 0013). */
export default function RequireAuth() {
  const session = useSession()

  if (session.isPending) {
    return <p role="status">Checking session…</p>
  }
  if (!session.data) {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
}
